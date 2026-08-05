package unattendedinventory

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"palpanel/internal/appconfig"
	"palpanel/internal/inventoryscope"
	"palpanel/internal/saveindex"
)

var indexManagers sync.Map

func CurrentSnapshot(ctx context.Context, cfg appconfig.Config) (Snapshot, error) {
	key := strings.Join([]string{
		cfg.ServerDirectory(), cfg.SaveIndexCacheDir, cfg.SaveIndexerURL,
	}, "\x00")
	value, _ := indexManagers.LoadOrStore(key, saveindex.NewManager(cfg))
	manager := value.(*saveindex.Manager)
	manager.EnsureFresh(ctx)
	index, status, err := manager.Current(ctx)
	if err != nil && !status.Stale {
		return Snapshot{}, err
	}
	switch status.State {
	case "ready", "stale":
	default:
		return Snapshot{}, fmt.Errorf("%w: save index state is %s", ErrSnapshotUnavailable, status.State)
	}
	return SnapshotFromIndex(index, status), nil
}

// SnapshotFromIndex deliberately totals only containers whose owner can be
// proven to be an indexed player, base or guild. World/map-object/drop and
// unresolved containers are excluded so spawn refreshes cannot become false
// unattended-production deltas.
func SnapshotFromIndex(index saveindex.Index, status saveindex.Status) Snapshot {
	totals := make(map[string]int64)
	seenContainers := make(map[string]struct{})
	classifier := inventoryscope.New(index)
	for _, container := range index.Containers {
		containerID := normalizeID(container.ContainerID)
		if containerID == "" {
			continue
		}
		if _, found := seenContainers[containerID]; found {
			continue
		}
		seenContainers[containerID] = struct{}{}
		if ownership := classifier.Resolve(container); !ownership.Trusted {
			continue
		}
		for _, slot := range container.Slots {
			itemID := strings.TrimSpace(slot.ItemID)
			if itemID == "" || slot.Count <= 0 {
				continue
			}
			totals[itemID] += int64(slot.Count)
		}
	}
	return Snapshot{
		Fingerprint: strings.TrimSpace(index.Snapshot.Fingerprint),
		GeneratedAt: strings.TrimSpace(index.GeneratedAt),
		Stale:       status.Stale,
		Totals:      totals,
	}
}

func normalizeID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
