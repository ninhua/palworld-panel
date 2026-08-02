package monitor

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"palpanel/internal/economy"
	"palpanel/internal/paldefender"
	"palpanel/internal/palrest"
	"palpanel/internal/playerpresence"
	"palpanel/internal/startergift"
	"palpanel/internal/tasks"
	"palpanel/internal/unattendedinventory"
)

type PlayerPresenceSnapshot struct {
	Players                []playerpresence.OnlinePlayer `json:"players"`
	ObservedAt             string                        `json:"observed_at,omitempty"`
	Source                 string                        `json:"source"`
	SampleDurationMS       int64                         `json:"sample_duration_ms"`
	OnlineTaskPlayers      int                           `json:"online_task_players"`
	OnlineTaskTracked      int                           `json:"online_task_tracked"`
	OnlineTaskTotalMinutes int64                         `json:"online_task_total_minutes"`
	OnlineTaskEmitted      int64                         `json:"online_task_emitted"`
	OnlineTaskError        string                        `json:"online_task_error,omitempty"`
}

type playerPresenceRuntime struct {
	mu                sync.RWMutex
	sampleMu          sync.Mutex
	observedAt        time.Time
	snapshot          PlayerPresenceSnapshot
	onlineInitialized bool
	lastOnlineTaskAt  time.Time
}

const onlineTaskSampleInterval = 30 * time.Second

var playerPresenceRuntimes sync.Map

func (m Manager) playerPresenceRuntime() *playerPresenceRuntime {
	key := strings.TrimSpace(m.cfg.DBPath)
	if key == "" {
		key = strings.TrimSpace(m.cfg.ServerDirectory())
	}
	actual, _ := playerPresenceRuntimes.LoadOrStore(key, &playerPresenceRuntime{})
	return actual.(*playerPresenceRuntime)
}

func (m Manager) PlayerPresenceSnapshot(maxAge time.Duration) (PlayerPresenceSnapshot, bool) {
	runtime := m.playerPresenceRuntime()
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	result := runtime.snapshot
	result.Players = append([]playerpresence.OnlinePlayer(nil), runtime.snapshot.Players...)
	if runtime.observedAt.IsZero() {
		return result, false
	}
	if maxAge > 0 && m.currentTime().Sub(runtime.observedAt) > maxAge {
		return result, false
	}
	return result, true
}

func (m Manager) observePlayerPresence(ctx context.Context, client palrest.Client) error {
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	runtime := m.playerPresenceRuntime()
	runtime.sampleMu.Lock()
	defer runtime.sampleMu.Unlock()
	startedAt := time.Now()

	scope, err := playerpresence.ResolveServerScope(m.cfg.ServerDirectory())
	if err != nil {
		return err
	}
	response, err := client.Do(requestCtx, http.MethodGet, "players", nil)
	if err != nil {
		return err
	}
	players := playerpresence.ParseRESTPlayers(response.Body)
	now := m.currentTime()
	if _, err = playerpresence.ObserveScoped(requestCtx, m.store, scope, now, players); err != nil {
		return err
	}

	runtime.mu.Lock()
	runtime.observedAt = now
	runtime.snapshot.Players = append([]playerpresence.OnlinePlayer(nil), players...)
	runtime.snapshot.ObservedAt = now.UTC().Format(time.RFC3339Nano)
	runtime.snapshot.Source = "monitor_palworld_rest"
	runtime.mu.Unlock()

	onlineSnapshot := m.sampleOnlineTasks(requestCtx, runtime, players)
	runtime.mu.Lock()
	runtime.snapshot.SampleDurationMS = time.Since(startedAt).Milliseconds()
	runtime.snapshot.OnlineTaskPlayers = onlineSnapshot.OnlinePlayers
	runtime.snapshot.OnlineTaskTracked = onlineSnapshot.TrackedPlayers
	runtime.snapshot.OnlineTaskTotalMinutes = onlineSnapshot.TotalMinutes
	runtime.snapshot.OnlineTaskEmitted = onlineSnapshot.EmittedMinutes
	runtime.snapshot.OnlineTaskError = onlineSnapshot.Error
	runtime.mu.Unlock()

	snapshot, snapshotErr := unattendedinventory.CurrentSnapshot(requestCtx, m.cfg)
	if observeErr := unattendedinventory.Observe(requestCtx, m.store, scope, now, len(players), snapshot, snapshotErr); observeErr != nil {
		m.debugf("unattended inventory observation failed: %q", observeErr.Error())
	}
	dispatcher := startergift.NewPalDefenderDispatcher(paldefender.NewManager(m.cfg, m.store))
	return startergift.Observe(ctx, m.store, scope, now, players, dispatcher)
}

type onlineTaskSnapshot struct {
	OnlinePlayers  int
	TrackedPlayers int
	TotalMinutes   int64
	EmittedMinutes int64
	Error          string
}

func (m Manager) sampleOnlineTasks(ctx context.Context, runtime *playerPresenceRuntime, players []playerpresence.OnlinePlayer) onlineTaskSnapshot {
	now := m.currentTime()
	runtime.mu.Lock()
	if !runtime.lastOnlineTaskAt.IsZero() && now.Sub(runtime.lastOnlineTaskAt) < onlineTaskSampleInterval {
		previous := onlineTaskSnapshot{
			OnlinePlayers:  len(players),
			TrackedPlayers: runtime.snapshot.OnlineTaskTracked,
			TotalMinutes:   runtime.snapshot.OnlineTaskTotalMinutes,
			Error:          runtime.snapshot.OnlineTaskError,
		}
		runtime.mu.Unlock()
		return previous
	}
	runtime.lastOnlineTaskAt = now
	firstSample := !runtime.onlineInitialized
	runtime.mu.Unlock()

	timezone := strings.TrimSpace(os.Getenv("PALPANEL_OPERATIONS_TIMEZONE"))
	taskService, err := tasks.ForPath(m.cfg.DBPath, timezone)
	if err != nil {
		return onlineTaskSnapshot{OnlinePlayers: len(players), Error: err.Error()}
	}
	pointService, err := economy.ForPath(m.cfg.DBPath, timezone)
	if err != nil {
		return onlineTaskSnapshot{OnlinePlayers: len(players), Error: err.Error()}
	}

	onlinePlayers := make([]tasks.OnlinePlayer, 0, len(players))
	for _, player := range players {
		playerUID := strings.TrimSpace(player.PlayerUID)
		if playerUID == "" {
			playerUID = strings.TrimSpace(player.SteamID)
		}
		if playerUID == "" {
			continue
		}
		onlinePlayers = append(onlinePlayers, tasks.OnlinePlayer{
			PlayerUID: playerUID,
			Nickname:  strings.TrimSpace(player.Nickname),
			SteamID:   strings.TrimSpace(player.SteamID),
		})
	}
	result, err := taskService.SampleOnlinePlayers(ctx, onlinePlayers, firstSample, pointService)
	if err != nil {
		return onlineTaskSnapshot{OnlinePlayers: len(onlinePlayers), Error: err.Error()}
	}
	runtime.mu.Lock()
	runtime.onlineInitialized = true
	runtime.mu.Unlock()
	return onlineTaskSnapshot{
		OnlinePlayers:  result.OnlinePlayers,
		TrackedPlayers: result.TrackedPlayers,
		TotalMinutes:   result.TotalEmittedMinutess,
		EmittedMinutes: result.EmittedMinutes,
	}
}
