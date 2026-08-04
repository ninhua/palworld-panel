package economy

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPlayerIdentityMigrationMergesEconomyAccounts(t *testing.T) {
	service, err := Open(filepath.Join(t.TempDir(), "economy.sqlite"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	const (
		compact = "F23D556C000000000000000000000000"
		hyphens = "F23D556C-00000000-00000000-00000000"
	)
	for _, account := range []struct {
		uid     string
		balance int64
	}{
		{compact, 52},
		{hyphens, 50},
	} {
		if _, err := service.db.Exec(`INSERT INTO economy_accounts(player_uid,nickname,steam_id,status,balance,created_at,updated_at)
			VALUES(?, 'tiantian', 'steam_76561199032061430', 'active', ?, '2026-08-01T00:00:00Z', '2026-08-03T00:00:00Z')`, account.uid, account.balance); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.db.Exec(`INSERT INTO economy_ledger(id,player_uid,delta,balance_after,reason,reference_type,reference_id,actor,metadata_json,created_at)
		VALUES
		('ledger_a', ?, 52, 52, 'test', 'task', 'same-reference', 'test', '{}', '2026-08-01T00:00:00Z'),
		('ledger_b', ?, 50, 50, 'test', 'task', 'same-reference', 'test', '{}', '2026-08-02T00:00:00Z')`, compact, hyphens); err != nil {
		t.Fatal(err)
	}

	if err := service.EnsurePlayerIdentityConsistency(context.Background()); err != nil {
		t.Fatal(err)
	}
	accounts, err := service.ListAccounts(context.Background(), "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(accounts))
	}
	if accounts[0].PlayerUID != compact || accounts[0].Balance != 102 {
		t.Fatalf("merged account = %+v", accounts[0])
	}
	entries, err := service.ListLedger(context.Background(), hyphens, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].ReferenceID == entries[1].ReferenceID {
		t.Fatalf("ledger entries were not preserved safely: %+v", entries)
	}
	if _, err := service.EnsureAccount(context.Background(), hyphens, "tiantian", "steam_76561199032061430"); err != nil {
		t.Fatal(err)
	}
	accounts, err = service.ListAccounts(context.Background(), "", 10, 0)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("format variant created a second account: count=%d err=%v", len(accounts), err)
	}
}
