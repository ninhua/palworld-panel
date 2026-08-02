package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"palpanel/internal/economy"
)

const maximumOnlineSampleGap = 90 * time.Second

type OnlinePlayer struct {
	PlayerUID string `json:"player_uid"`
	Nickname  string `json:"nickname,omitempty"`
	SteamID   string `json:"steam_id,omitempty"`
}

type OnlineTrackingRecord struct {
	PlayerUID           string `json:"player_uid"`
	Nickname            string `json:"nickname,omitempty"`
	SteamID             string `json:"steam_id,omitempty"`
	Active              bool   `json:"active"`
	OnlineSince         string `json:"online_since,omitempty"`
	LastSeenAt          string `json:"last_seen_at,omitempty"`
	PendingSeconds      int64  `json:"pending_seconds"`
	TotalEmittedMinutes int64  `json:"total_emitted_minutes"`
	UpdatedAt           string `json:"updated_at"`
}

type OnlineSampleResult struct {
	SampledAt            string           `json:"sampled_at"`
	OnlinePlayers        int              `json:"online_players"`
	TrackedPlayers       int              `json:"tracked_players"`
	StartedSessions      int              `json:"started_sessions"`
	EndedSessions        int              `json:"ended_sessions"`
	EmittedMinutes       int64            `json:"emitted_minutes"`
	TotalEmittedMinutess int64            `json:"total_emitted_minutes"`
	Updates              []ProgressUpdate `json:"updates"`
}

func (s *Service) EnsureOnlineTrackingSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS operations_task_online_tracking (
			player_uid TEXT PRIMARY KEY,
			nickname TEXT NOT NULL DEFAULT '',
			steam_id TEXT NOT NULL DEFAULT '',
			active INTEGER NOT NULL DEFAULT 0 CHECK(active IN (0,1)),
			online_since TEXT NOT NULL DEFAULT '',
			last_seen_at TEXT NOT NULL DEFAULT '',
			pending_seconds INTEGER NOT NULL DEFAULT 0 CHECK(pending_seconds >= 0),
			total_emitted_minutes INTEGER NOT NULL DEFAULT 0 CHECK(total_emitted_minutes >= 0),
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_operations_task_online_tracking_active ON operations_task_online_tracking(active,updated_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create online task tracking schema: %w", err)
		}
	}
	return nil
}

func (s *Service) SampleOnlinePlayers(ctx context.Context, players []OnlinePlayer, firstSample bool, adjuster interface {
	Adjust(context.Context, economy.Adjustment) (economy.AdjustmentResult, error)
}) (OnlineSampleResult, error) {
	if err := s.EnsureOnlineTrackingSchema(ctx); err != nil {
		return OnlineSampleResult{}, err
	}
	now := s.now().UTC()
	result := OnlineSampleResult{SampledAt: now.Format(time.RFC3339Nano), Updates: make([]ProgressUpdate, 0)}
	current := normalizeOnlinePlayers(players)
	result.OnlinePlayers = len(current)
	existing, err := s.onlineTrackingRecords(ctx)
	if err != nil {
		return result, err
	}

	for playerUID, player := range current {
		record, found := existing[playerUID]
		if !found {
			record = OnlineTrackingRecord{PlayerUID: playerUID, Nickname: player.Nickname, SteamID: player.SteamID}
			if err := s.saveOnlineTrackingRecord(ctx, record, true, now, now, 0, 0); err != nil {
				return result, err
			}
			result.StartedSessions++
			continue
		}
		delete(existing, playerUID)
		record.Nickname = firstNonEmptyString(player.Nickname, record.Nickname)
		record.SteamID = firstNonEmptyString(player.SteamID, record.SteamID)
		lastSeen := parseOnlineTimestamp(record.LastSeenAt, now)
		delta := onlineSampleSeconds(lastSeen, now, firstSample || !record.Active)
		pending := record.PendingSeconds + delta
		emitted := pending / 60
		if emitted > 0 {
			updates, processErr := s.emitOnlineMinutes(ctx, record, player, emitted, now, adjuster)
			if processErr != nil {
				return result, processErr
			}
			result.EmittedMinutes += emitted
			result.Updates = append(result.Updates, updates...)
			pending %= 60
			record.TotalEmittedMinutes += emitted
		}
		onlineSince := parseOnlineTimestamp(record.OnlineSince, now)
		if !record.Active || strings.TrimSpace(record.OnlineSince) == "" {
			onlineSince = now
			result.StartedSessions++
		}
		if err := s.saveOnlineTrackingRecord(ctx, record, true, onlineSince, now, pending, record.TotalEmittedMinutes); err != nil {
			return result, err
		}
	}

	for _, record := range existing {
		if !record.Active {
			continue
		}
		lastSeen := parseOnlineTimestamp(record.LastSeenAt, now)
		pending := record.PendingSeconds + onlineSampleSeconds(lastSeen, now, firstSample)
		emitted := pending / 60
		player := OnlinePlayer{PlayerUID: record.PlayerUID, Nickname: record.Nickname, SteamID: record.SteamID}
		if emitted > 0 {
			updates, processErr := s.emitOnlineMinutes(ctx, record, player, emitted, now, adjuster)
			if processErr != nil {
				return result, processErr
			}
			result.EmittedMinutes += emitted
			result.Updates = append(result.Updates, updates...)
			pending %= 60
			record.TotalEmittedMinutes += emitted
		}
		if err := s.saveOnlineTrackingRecord(ctx, record, false, time.Time{}, now, pending, record.TotalEmittedMinutes); err != nil {
			return result, err
		}
		result.EndedSessions++
	}

	var tracked int
	var totalEmitted int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(total_emitted_minutes),0) FROM operations_task_online_tracking`).Scan(&tracked, &totalEmitted); err != nil {
		return result, err
	}
	result.TrackedPlayers = tracked
	result.TotalEmittedMinutess = totalEmitted
	return result, nil
}

func (s *Service) OnlineTrackingRecords(ctx context.Context, limit int) ([]OnlineTrackingRecord, error) {
	if err := s.EnsureOnlineTrackingSchema(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT player_uid,nickname,steam_id,active,online_since,last_seen_at,pending_seconds,total_emitted_minutes,updated_at FROM operations_task_online_tracking ORDER BY active DESC,updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]OnlineTrackingRecord, 0)
	for rows.Next() {
		item, scanErr := scanOnlineTrackingRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) onlineTrackingRecords(ctx context.Context) (map[string]OnlineTrackingRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT player_uid,nickname,steam_id,active,online_since,last_seen_at,pending_seconds,total_emitted_minutes,updated_at FROM operations_task_online_tracking`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := map[string]OnlineTrackingRecord{}
	for rows.Next() {
		item, scanErr := scanOnlineTrackingRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items[item.PlayerUID] = item
	}
	return items, rows.Err()
}

func scanOnlineTrackingRecord(scanner interface{ Scan(...any) error }) (OnlineTrackingRecord, error) {
	var item OnlineTrackingRecord
	var active int
	if err := scanner.Scan(&item.PlayerUID, &item.Nickname, &item.SteamID, &active, &item.OnlineSince, &item.LastSeenAt, &item.PendingSeconds, &item.TotalEmittedMinutes, &item.UpdatedAt); err != nil {
		return OnlineTrackingRecord{}, err
	}
	item.Active = active != 0
	return item, nil
}

func (s *Service) saveOnlineTrackingRecord(ctx context.Context, record OnlineTrackingRecord, active bool, onlineSince, lastSeen time.Time, pendingSeconds, totalMinutes int64) error {
	if pendingSeconds < 0 {
		pendingSeconds = 0
	}
	if totalMinutes < 0 {
		totalMinutes = 0
	}
	onlineSinceText := ""
	if active && !onlineSince.IsZero() {
		onlineSinceText = onlineSince.UTC().Format(time.RFC3339Nano)
	}
	lastSeenText := ""
	if !lastSeen.IsZero() {
		lastSeenText = lastSeen.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO operations_task_online_tracking(player_uid,nickname,steam_id,active,online_since,last_seen_at,pending_seconds,total_emitted_minutes,updated_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(player_uid) DO UPDATE SET nickname=CASE WHEN excluded.nickname<>'' THEN excluded.nickname ELSE nickname END,steam_id=CASE WHEN excluded.steam_id<>'' THEN excluded.steam_id ELSE steam_id END,active=excluded.active,online_since=excluded.online_since,last_seen_at=excluded.last_seen_at,pending_seconds=excluded.pending_seconds,total_emitted_minutes=excluded.total_emitted_minutes,updated_at=excluded.updated_at`, record.PlayerUID, firstNonEmptyString(record.Nickname), firstNonEmptyString(record.SteamID), boolInt(active), onlineSinceText, lastSeenText, pendingSeconds, totalMinutes, s.timestamp())
	return err
}

func (s *Service) emitOnlineMinutes(ctx context.Context, record OnlineTrackingRecord, player OnlinePlayer, minutes int64, now time.Time, adjuster interface {
	Adjust(context.Context, economy.Adjustment) (economy.AdjustmentResult, error)
}) ([]ProgressUpdate, error) {
	if minutes <= 0 {
		return nil, nil
	}
	start := record.TotalEmittedMinutes + 1
	end := record.TotalEmittedMinutes + minutes
	event := Event{
		EventID:    onlineEventID(player.PlayerUID, start, end),
		Type:       "PLAYER_ONLINE",
		PlayerUID:  player.PlayerUID,
		Nickname:   firstNonEmptyString(player.Nickname, record.Nickname),
		SteamID:    firstNonEmptyString(player.SteamID, record.SteamID),
		OccurredAt: now.UTC().Format(time.RFC3339Nano),
		Payload: map[string]any{
			"minutes":     minutes,
			"seconds":     minutes * 60,
			"from_minute": start,
			"to_minute":   end,
		},
	}
	return s.ProcessEvent(ctx, event, adjuster)
}

func normalizeOnlinePlayers(players []OnlinePlayer) map[string]OnlinePlayer {
	result := map[string]OnlinePlayer{}
	for _, player := range players {
		player.PlayerUID = strings.TrimSpace(player.PlayerUID)
		player.Nickname = strings.TrimSpace(player.Nickname)
		player.SteamID = strings.TrimSpace(player.SteamID)
		if player.PlayerUID == "" {
			continue
		}
		if previous, found := result[player.PlayerUID]; found {
			player.Nickname = firstNonEmptyString(player.Nickname, previous.Nickname)
			player.SteamID = firstNonEmptyString(player.SteamID, previous.SteamID)
		}
		result[player.PlayerUID] = player
	}
	return result
}

func onlineSampleSeconds(lastSeen, now time.Time, reset bool) int64 {
	if reset || lastSeen.IsZero() || !now.After(lastSeen) {
		return 0
	}
	delta := now.Sub(lastSeen)
	if delta > maximumOnlineSampleGap {
		delta = maximumOnlineSampleGap
	}
	return int64(delta / time.Second)
}

func onlineEventID(playerUID string, start, end int64) string {
	value := fmt.Sprintf("%s:%d:%d", strings.TrimSpace(playerUID), start, end)
	hash := sha256.Sum256([]byte(value))
	return "online_" + hex.EncodeToString(hash[:16])
}

func parseOnlineTimestamp(value string, fallback time.Time) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
