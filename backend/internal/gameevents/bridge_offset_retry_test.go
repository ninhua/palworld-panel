package gameevents

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSaveBridgeOffsetStopsAtLatestPendingError(t *testing.T) {
	service, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer service.Close()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "PalDefender.log")
	if err := service.SaveBridgeOffset(ctx, path, 10, 100); err != nil {
		t.Fatalf("initial SaveBridgeOffset() error = %v", err)
	}
	for _, item := range []BridgeObservation{
		{SourcePath: path, Offset: 40, Status: "error", Reason: "temporary task database failure"},
		{SourcePath: path, Offset: 70, Status: "error", Reason: "temporary economy failure"},
	} {
		if err := service.AddBridgeObservation(ctx, item); err != nil {
			t.Fatalf("AddBridgeObservation(%d) error = %v", item.Offset, err)
		}
	}

	if err := service.SaveBridgeOffset(ctx, path, 90, 100); err != nil {
		t.Fatalf("SaveBridgeOffset() error = %v", err)
	}
	assertBridgeOffset(t, service, path, 40)

	if err := service.AddBridgeObservation(ctx, BridgeObservation{SourcePath: path, Offset: 40, Status: "processed"}); err != nil {
		t.Fatalf("mark first event processed: %v", err)
	}
	if err := service.SaveBridgeOffset(ctx, path, 90, 100); err != nil {
		t.Fatalf("SaveBridgeOffset() after first recovery error = %v", err)
	}
	assertBridgeOffset(t, service, path, 70)

	if err := service.AddBridgeObservation(ctx, BridgeObservation{SourcePath: path, Offset: 70, Status: "processed"}); err != nil {
		t.Fatalf("mark second event processed: %v", err)
	}
	if err := service.SaveBridgeOffset(ctx, path, 90, 100); err != nil {
		t.Fatalf("SaveBridgeOffset() after all recovery error = %v", err)
	}
	assertBridgeOffset(t, service, path, 90)
}

func TestSaveBridgeOffsetIgnoresOldErrorsAfterTruncation(t *testing.T) {
	service, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer service.Close()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "PalDefender.log")
	if err := service.SaveBridgeOffset(ctx, path, 90, 100); err != nil {
		t.Fatalf("initial SaveBridgeOffset() error = %v", err)
	}
	if err := service.AddBridgeObservation(ctx, BridgeObservation{SourcePath: path, Offset: 10, Status: "error", Reason: "old file generation"}); err != nil {
		t.Fatalf("AddBridgeObservation() error = %v", err)
	}

	if err := service.SaveBridgeOffset(ctx, path, 20, 20); err != nil {
		t.Fatalf("SaveBridgeOffset() after truncation error = %v", err)
	}
	assertBridgeOffset(t, service, path, 20)
}

func assertBridgeOffset(t *testing.T, service *Service, path string, want int64) {
	t.Helper()
	item, found, err := service.BridgeOffset(context.Background(), path)
	if err != nil {
		t.Fatalf("BridgeOffset() error = %v", err)
	}
	if !found || item.Offset != want {
		t.Fatalf("BridgeOffset() = %#v, found=%t, want offset %d", item, found, want)
	}
}
