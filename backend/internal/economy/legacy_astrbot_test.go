package economy

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyAstrBotImportIsIncremental(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "astrbot.sqlite3")
	source, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE accounts(qq_id TEXT PRIMARY KEY,balance INTEGER NOT NULL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL)`,
		`CREATE TABLE bindings(qq_id TEXT PRIMARY KEY,player_uid TEXT NOT NULL UNIQUE,nickname TEXT NOT NULL,source_fingerprint TEXT NOT NULL DEFAULT '',status TEXT NOT NULL DEFAULT 'active',verified_at INTEGER NOT NULL,updated_at INTEGER NOT NULL)`,
		`CREATE TABLE checkins(qq_id TEXT NOT NULL,local_date TEXT NOT NULL,points INTEGER NOT NULL,created_at INTEGER NOT NULL,PRIMARY KEY(qq_id,local_date))`,
		`INSERT INTO accounts VALUES('10001',15,1,1),('10002',9,1,1),('10003',0,1,1)`,
		`INSERT INTO bindings VALUES('10001','player-1','Alice','','active',1,1),('10003','player-3','Zero','','active',1,1)`,
		`INSERT INTO checkins VALUES('10001','2026-07-31',10,1)`,
	} {
		if _, err := source.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}

	service, err := Open(filepath.Join(directory, "panel.sqlite3"), "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	preview, err := service.InspectLegacyAstrBot(ctx, sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ImportableAccounts != 1 || preview.ImportablePoints != 15 || preview.UnboundAccounts != 1 || preview.ZeroBalance != 1 {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	first, err := service.ImportLegacyAstrBot(ctx, sourcePath, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if first.ImportedAccounts != 1 || first.ImportedPoints != 15 || first.ImportedCheckins != 1 {
		t.Fatalf("unexpected first import: %+v", first)
	}
	account, err := service.GetAccount(ctx, "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if account.Balance != 15 {
		t.Fatalf("balance=%d want 15", account.Balance)
	}
	second, err := service.ImportLegacyAstrBot(ctx, sourcePath, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if second.ImportedPoints != 0 || second.AlreadyCurrent != 1 || second.ImportedCheckins != 0 {
		t.Fatalf("unexpected repeated import: %+v", second)
	}

	source, err = sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec(`UPDATE accounts SET balance=20 WHERE qq_id='10001'`); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := service.ImportLegacyAstrBot(ctx, sourcePath, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if third.ImportedPoints != 5 || third.ImportedAccounts != 1 {
		t.Fatalf("unexpected incremental import: %+v", third)
	}
	account, err = service.GetAccount(ctx, "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if account.Balance != 20 {
		t.Fatalf("balance=%d want 20", account.Balance)
	}
}

func TestLegacyAstrBotRejectsNonSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not.sqlite3")
	if err := os.WriteFile(path, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := Open(filepath.Join(t.TempDir(), "panel.sqlite3"), "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.InspectLegacyAstrBot(context.Background(), path); err == nil {
		t.Fatal("expected invalid database error")
	}
}
