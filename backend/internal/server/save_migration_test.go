package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"palpanel/internal/docker"
)

func TestSaveMigrationRollsBackWorldWhenVerificationFails(t *testing.T) {
	m, closeManager := newRunningWorldTestManager(t, true)
	defer closeManager()

	source := filepath.Join(m.cfg.DataDir, "save-sources", "old-world")
	writeFile(t, filepath.Join(source, "Level.sav"), "migrated-level")
	writeFile(t, filepath.Join(source, "Players", "25527209000000000000000000000000.sav"), "migrated-player")
	m.migrationRemap = func(_ context.Context, input, output string, mappings []UIDMapping) error {
		if input != source || len(mappings) != 1 {
			t.Fatalf("remap input=%q mappings=%#v", input, mappings)
		}
		return copyMigrationFixture(input, output)
	}
	m.migrationVerify = func(string, []UIDMapping) error {
		return errors.New("synthetic semantic verification failure")
	}
	started := false
	m.migrationStatus = func(context.Context) (Status, error) {
		return Status{Container: docker.ContainerStatus{Status: "running"}}, nil
	}
	m.migrationStop = func(context.Context) error { return nil }
	m.migrationStart = func(context.Context) error { started = true; return nil }

	job, err := m.MigrateWorld(t.Context(), SaveMigrationRequest{
		SourcePath: source,
		Mappings: []UIDMapping{{
			SourceUID: "25527209-0000-0000-0000-000000000000",
			TargetUID: "f8f86740-0000-0000-0000-000000000000",
		}},
		Confirmation: "MIGRATE PLAYERS",
	}, WorldMigrationHooks{})
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, m.store, job.ID)
	if done.Status != "failed" || !strings.Contains(done.Error, "rolled back") {
		t.Fatalf("migration result = %#v", done)
	}
	body, err := os.ReadFile(filepath.Join(m.worldPath("running-world"), "Level.sav"))
	if err != nil || string(body) != "old-world" {
		t.Fatalf("original world was not restored: %q, %v", body, err)
	}
	if !started {
		t.Fatal("server was not restarted after rollback")
	}
	backups, err := m.ListBackups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("verified migration backup missing: %#v, %v", backups, err)
	}
}

func TestSaveMigrationDeploysVerifiedWorldAndRestartsServer(t *testing.T) {
	m, closeManager := newRunningWorldTestManager(t, true)
	defer closeManager()

	source := filepath.Join(m.cfg.DataDir, "save-sources", "old-world")
	writeFile(t, filepath.Join(source, "Level.sav"), "migrated-level")
	writeFile(t, filepath.Join(source, "Players", "25527209000000000000000000000000.sav"), "migrated-player")
	m.migrationRemap = func(_ context.Context, input, output string, _ []UIDMapping) error {
		return copyMigrationFixture(input, output)
	}
	m.migrationVerify = func(worldPath string, _ []UIDMapping) error {
		body, err := os.ReadFile(filepath.Join(worldPath, "Level.sav"))
		if err != nil || string(body) != "migrated-level" {
			return errors.New("migrated world is unavailable")
		}
		return nil
	}
	started := false
	m.migrationStatus = func(context.Context) (Status, error) {
		return Status{Container: docker.ContainerStatus{Status: "running"}}, nil
	}
	m.migrationStop = func(context.Context) error { return nil }
	m.migrationStart = func(context.Context) error { started = true; return nil }

	job, err := m.MigrateWorld(t.Context(), SaveMigrationRequest{
		SourcePath:   source,
		Mappings:     []UIDMapping{{SourceUID: "25527209-0000-0000-0000-000000000000", TargetUID: "f8f86740-0000-0000-0000-000000000000"}},
		Confirmation: "MIGRATE PLAYERS",
	}, WorldMigrationHooks{})
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, m.store, job.ID)
	if done.Status != "completed" {
		t.Fatalf("migration result = %#v", done)
	}
	body, err := os.ReadFile(filepath.Join(m.worldPath("running-world"), "Level.sav"))
	if err != nil || string(body) != "migrated-level" {
		t.Fatalf("migrated world was not deployed: %q, %v", body, err)
	}
	if !started {
		t.Fatal("server was not restarted")
	}
}

func TestSaveMigrationStopsPartialStartBeforeRollback(t *testing.T) {
	m, closeManager := newRunningWorldTestManager(t, true)
	defer closeManager()

	source := filepath.Join(m.cfg.DataDir, "save-sources", "old-world")
	writeFile(t, filepath.Join(source, "Level.sav"), "migrated-level")
	writeFile(t, filepath.Join(source, "Players", "25527209000000000000000000000000.sav"), "migrated-player")
	m.migrationRemap = func(_ context.Context, input, output string, _ []UIDMapping) error {
		return copyMigrationFixture(input, output)
	}
	m.migrationVerify = func(string, []UIDMapping) error { return nil }
	m.migrationStatus = func(context.Context) (Status, error) {
		return Status{Container: docker.ContainerStatus{Status: "running"}}, nil
	}
	stopCalls := 0
	m.migrationStop = func(context.Context) error { stopCalls++; return nil }
	startCalls := 0
	m.migrationStart = func(context.Context) error {
		startCalls++
		if startCalls == 1 {
			return errors.New("synthetic partial start failure")
		}
		return nil
	}

	job, err := m.MigrateWorld(t.Context(), SaveMigrationRequest{
		SourcePath:   source,
		Mappings:     []UIDMapping{{SourceUID: "25527209-0000-0000-0000-000000000000", TargetUID: "f8f86740-0000-0000-0000-000000000000"}},
		Confirmation: "MIGRATE PLAYERS",
	}, WorldMigrationHooks{})
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, m.store, job.ID)
	if done.Status != "failed" || stopCalls != 2 || startCalls != 2 {
		t.Fatalf("migration result=%#v stopCalls=%d startCalls=%d", done, stopCalls, startCalls)
	}
	body, err := os.ReadFile(filepath.Join(m.worldPath("running-world"), "Level.sav"))
	if err != nil || string(body) != "old-world" {
		t.Fatalf("original world was not restored after partial start: %q, %v", body, err)
	}
}

func copyMigrationFixture(source, target string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if info.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, payload, 0o600)
	})
}
