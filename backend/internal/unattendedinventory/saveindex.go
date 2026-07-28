package unattendedinventory

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"palpanel/internal/appconfig"
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

func SnapshotFromIndex(index saveindex.Index, status saveindex.Status) Snapshot {
	totals := make(map[string]int64)
	seenContainers := make(map[string]struct{})
	for _, container := range index.Containers {
		containerID := normalizeID(container.ContainerID)
		if containerID == "" {
			continue
		}
		if _, found := seenContainers[containerID]; found {
			continue
		}
		seenContainers[containerID] = struct{}{}
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
