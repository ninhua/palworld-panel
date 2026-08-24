package server

import (
	"context"
	"errors"
)

// WithStoppedOperation serializes a filesystem mutation with all server
// lifecycle operations. It deliberately never stops or starts the server.
func (m Manager) WithStoppedOperation(ctx context.Context, operation func() error) error {
	if operation == nil {
		return errors.New("stopped operation is required")
	}
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	status, err := m.Status(ctx)
	if err != nil {
		return err
	}
	if serverStatusRunning(status) {
		return errors.New("server must be stopped")
	}
	return operation()
}
