package saveindex

import "os"

// AllHistoryEvents returns the complete semantic event set for two persisted
// snapshots without applying the public diff pagination. It is used by the API
// to remove world noise before pagination, so a large number of wild entities
// cannot push player-owned changes out of the first page.
func (m *Manager) AllHistoryEvents(fromID, toID string) ([]HistoryEvent, error) {
	worldDir, err := m.FindWorldDir()
	if err != nil {
		return nil, err
	}

	m.historyMu.Lock()
	defer m.historyMu.Unlock()

	worldKey := historyWorldKey(worldDir)
	manifest, err := m.loadHistoryManifest(worldKey)
	if err != nil {
		return nil, err
	}
	fromMeta, found := historyMetadata(manifest.Items, fromID)
	if !found {
		return nil, os.ErrNotExist
	}
	toMeta, found := historyMetadata(manifest.Items, toID)
	if !found {
		return nil, os.ErrNotExist
	}
	from, err := m.readHistorySnapshot(worldKey, fromMeta)
	if err != nil {
		return nil, err
	}
	to, err := m.readHistorySnapshot(worldKey, toMeta)
	if err != nil {
		return nil, err
	}
	return buildHistoryEvents(from.Index, to.Index), nil
}
