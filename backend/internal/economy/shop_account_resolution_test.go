package economy

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
	_ "palpanel/internal/playeridentity"
)

func TestResolveShopAccountUsesUniqueSteamAccountForZeroBalanceAlias(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	for _, statement := range []string{
		`CREATE TABLE economy_accounts(player_uid TEXT PRIMARY KEY,nickname TEXT NOT NULL DEFAULT '',steam_id TEXT NOT NULL DEFAULT '',status TEXT NOT NULL DEFAULT 'active',balance INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL)`,
		`CREATE TABLE economy_reservations(id TEXT PRIMARY KEY,player_uid TEXT NOT NULL,amount INTEGER NOT NULL,reference_id TEXT NOT NULL,status TEXT NOT NULL,expires_at TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL)`,
		`INSERT INTO economy_accounts VALUES('AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA','new','steam_76561198000000001','active',0,'1','2')`,
		`INSERT INTO economy_accounts VALUES('BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB','old','steam_76561198000000001','active',100,'1','2')`,
		`INSERT INTO economy_reservations VALUES('r1','BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB',20,'order-1','reserved','9','1','2')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{db: database}
	resolution, err := service.ResolveShopAccount(context.Background(), "AAAAAAAA-AAAAAAAA-AAAAAAAA-AAAAAAAA", "new", "76561198000000001")
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Account.PlayerUID != "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB" || resolution.Account.Balance != 100 {
		t.Fatalf("unexpected account: %#v", resolution.Account)
	}
	if resolution.MatchStrategy != "steam_id_fallback" || resolution.ReservedPoints != 20 {
		t.Fatalf("unexpected resolution: %#v", resolution)
	}
}
