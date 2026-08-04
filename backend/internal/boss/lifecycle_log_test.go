package boss

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBossLifecycleLogCursorIgnoresHistoryAndDeduplicates(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "PalDefender.log")
	if err := os.WriteFile(path, []byte("[00:00:00][info] old has killed Pal 'ChickenPal' (ChickenPal)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cursors, err := captureBossLifecycleLogCursors(directory)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("[00:00:01][info] player has killed Pal 'ChickenPal' (ChickenPal)\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := file.WriteString("[00:00:02][info] player has killed Pal 'Sheepball' (Sheepball)\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	events, updated, _, err := scanBossLifecycleLogs(directory, cursors, []string{"ChickenPal"}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events=%d, want 1: %#v", len(events), events)
	}
	if events[0].TargetID != "ChickenPal" {
		t.Fatalf("target=%q, want ChickenPal", events[0].TargetID)
	}
	events, _, _, err = scanBossLifecycleLogs(directory, updated, []string{"ChickenPal"}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("duplicate events=%d", len(events))
	}
}

func TestBossLifecycleLogRotationStartsNewGeneration(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "PalDefender.log")
	if err := os.WriteFile(path, []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cursors, err := captureBossLifecycleLogCursors(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[00:00:03][info] player has killed Pal 'ChickenPal' (ChickenPal)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	events, updated, _, err := scanBossLifecycleLogs(directory, cursors, []string{"ChickenPal"}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events=%d, want 1", len(events))
	}
	if updated[path].Generation != 1 {
		t.Fatalf("generation=%d, want 1", updated[path].Generation)
	}
}

func TestBossLifecyclePartialLineWaitsForCompletion(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "PalDefender.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cursors, err := captureBossLifecycleLogCursors(directory)
	if err != nil {
		t.Fatal(err)
	}
	partial := "[00:00:04][info] player has killed Pal 'ChickenPal' (ChickenPal)"
	if err := os.WriteFile(path, []byte(partial), 0o644); err != nil {
		t.Fatal(err)
	}
	events, cursors, _, err := scanBossLifecycleLogs(directory, cursors, []string{"ChickenPal"}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("partial line produced %d events", len(events))
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	events, _, _, err = scanBossLifecycleLogs(directory, cursors, []string{"ChickenPal"}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("completed line produced %d events, want 1", len(events))
	}
}
