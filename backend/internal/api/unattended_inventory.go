package api

import (
	"time"

	"github.com/gin-gonic/gin"

	"palpanel/internal/pallocalize"
	"palpanel/internal/playerpresence"
	"palpanel/internal/unattendedinventory"
)

func init() {
	patchFeatures = append(patchFeatures, "unattended-inventory-delta")
}

type unattendedInventoryItem struct {
	ItemID   string `json:"item_id"`
	ItemName string `json:"item_name"`
	ItemIcon string `json:"item_icon"`
	Category string `json:"category"`
	Quantity int64  `json:"quantity"`
}

func (s Server) unattendedInventoryView(c *gin.Context, sourceID string) gin.H {
	if sourceID != "server" {
		return gin.H{
			"available":   false,
			"status":      "source_mismatch",
			"additions":   []unattendedInventoryItem{},
			"total_added": 0,
		}
	}
	scope, err := playerpresence.ResolveServerScope(s.cfg.ServerDirectory())
	if err != nil {
		return gin.H{
			"available":   false,
			"status":      "world_unavailable",
			"additions":   []unattendedInventoryItem{},
			"total_added": 0,
		}
	}
	state, err := unattendedinventory.Load(c.Request.Context(), s.store, scope)
	if err != nil {
		return gin.H{
			"available":   false,
			"status":      "state_unavailable",
			"world_id":    scope.WorldID,
			"additions":   []unattendedInventoryItem{},
			"total_added": 0,
		}
	}
	view := unattendedinventory.Public(state, time.Now())
	items := make([]unattendedInventoryItem, 0, len(view.Additions))
	for _, item := range view.Additions {
		items = append(items, unattendedInventoryItem{
			ItemID:   item.ItemID,
			ItemName: pallocalize.ItemName(item.ItemID),
			ItemIcon: pallocalize.ItemIcon(item.ItemID),
			Category: inventoryItemCategory(item.ItemID),
			Quantity: item.Quantity,
		})
	}
	return gin.H{
		"available":                  view.Available,
		"status":                     view.Status,
		"world_id":                   view.WorldID,
		"started_at":                 view.StartedAt,
		"baseline_at":                view.BaselineAt,
		"eligible_at":                view.EligibleAt,
		"ended_at":                   view.EndedAt,
		"last_observed_at":           view.LastObservedAt,
		"duration_seconds":           view.DurationSeconds,
		"eligible_remaining_seconds": view.EligibleRemainingSeconds,
		"qualified":                  view.Qualified,
		"current_online":             view.CurrentOnline,
		"presence_stale":             view.PresenceStale,
		"snapshot_stale":             view.SnapshotStale,
		"additions":                  items,
		"total_added":                view.TotalAdded,
	}
}
