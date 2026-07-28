package api

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"palpanel/internal/playerpresence"
)

func (s Server) attachPlayerPresence(ctx context.Context, views []gin.H, enabled bool) {
	if !enabled {
		markPresenceUnavailable(views, false)
		return
	}
	scope, err := playerpresence.ResolveServerScope(s.cfg.ServerDirectory())
	if err != nil {
		markPresenceUnavailable(views, true)
		return
	}
	state, err := playerpresence.LoadScoped(ctx, s.store, scope)
	if err != nil {
		markPresenceUnavailable(views, true)
		return
	}
	stale := playerpresence.IsStale(state, time.Now())
	available := state.Available && !stale
	for _, view := range views {
		view["presence_available"] = false
		view["presence_stale"] = stale
		view["presence_observed_at"] = state.ObservedAt
		view["presence_scope_id"] = scope.ID
		view["presence_world_id"] = scope.WorldID
		record, found := playerpresence.Find(state, firstNonEmpty(stringFromPresenceView(view, "player_uid"), stringFromPresenceView(view, "steam_id"), stringFromPresenceView(view, "id")))
		if !found {
			continue
		}
		view["presence_available"] = available
		view["presence_online"] = record.Online
		view["session_seconds"] = record.SessionSeconds
		view["total_seconds"] = record.TotalSeconds
		view["session_started_at"] = record.SessionStartedAt
		view["last_seen_at"] = record.LastSeenAt
		view["last_online_at"] = record.LastOnlineAt
		view["last_offline_at"] = record.LastOfflineAt
		view["presence_sessions"] = record.Sessions
	}
}

func markPresenceUnavailable(views []gin.H, stale bool) {
	for _, view := range views {
		view["presence_available"] = false
		view["presence_stale"] = stale
	}
}

func stringFromPresenceView(view gin.H, key string) string {
	value, _ := view[key].(string)
	return value
}
