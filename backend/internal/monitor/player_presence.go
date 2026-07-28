package monitor

import (
	"context"
	"net/http"
	"time"

	"palpanel/internal/paldefender"
	"palpanel/internal/palrest"
	"palpanel/internal/playerpresence"
	"palpanel/internal/startergift"
	"palpanel/internal/unattendedinventory"
)

func (m Manager) observePlayerPresence(ctx context.Context, client palrest.Client) error {
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

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
	snapshot, snapshotErr := unattendedinventory.CurrentSnapshot(requestCtx, m.cfg)
	if observeErr := unattendedinventory.Observe(requestCtx, m.store, scope, now, len(players), snapshot, snapshotErr); observeErr != nil {
		m.debugf("unattended inventory observation failed: %q", observeErr.Error())
	}
	dispatcher := startergift.NewPalDefenderDispatcher(paldefender.NewManager(m.cfg, m.store))
	return startergift.Observe(ctx, m.store, scope, now, players, dispatcher)
}
