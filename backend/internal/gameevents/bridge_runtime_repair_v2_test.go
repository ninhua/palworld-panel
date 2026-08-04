package gameevents

import (
	"context"
	"path/filepath"
	"testing"
)

func TestBridgeRuntimeRepairV2NoiseAndLoginRecovery(t *testing.T) {
	service, err := Open(filepath.Join(t.TempDir(), "panel.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	if _, err := service.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS economy_accounts (
		player_uid TEXT PRIMARY KEY,nickname TEXT NOT NULL DEFAULT '',steam_id TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'active',balance INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.ExecContext(ctx, `INSERT INTO economy_accounts(player_uid,nickname,steam_id,status,balance,created_at,updated_at)
		VALUES('F23D556C000000000000000000000000','tiantian','steam_76561199032061430','active',146,'now','now')`); err != nil {
		t.Fatal(err)
	}
	if err := service.EnsureBridgeRuntimeRepairV2(ctx); err != nil {
		t.Fatal(err)
	}

	login, err := service.AddBridgeDeadLetter(ctx, BridgeDeadLetter{
		EventID: "login-1", EventType: "PLAYER_LOGIN", PlayerHint: "steam_76561199032061430",
		Nickname: "39.166.34.148", RawLine: `steam_76561199032061430 ('39.166.34.148') connected to the server.`,
	})
	if err != nil {
		t.Fatal(err)
	}
	login, err = service.GetBridgeDeadLetter(ctx, login.ID)
	if err != nil {
		t.Fatal(err)
	}
	if login.PlayerUID != "F23D556C000000000000000000000000" || login.Nickname != "" || login.Status != "pending" {
		t.Fatalf("unexpected login repair: %+v", login)
	}

	systemReply, err := service.AddBridgeDeadLetter(ctx, BridgeDeadLetter{
		EventID: "reply-1", EventType: "PLAYER_CHAT",
		RawLine: `[Player::SendMsg::Say][1 receiver(s)] -> [SYSTEM] 用法：兑换 <商品>。`,
	})
	if err != nil {
		t.Fatal(err)
	}
	systemReply, err = service.GetBridgeDeadLetter(ctx, systemReply.ID)
	if err != nil {
		t.Fatal(err)
	}
	if systemReply.Status != "dismissed" {
		t.Fatalf("system reply status=%q", systemReply.Status)
	}

	playerDeath, err := service.AddBridgeDeadLetter(ctx, BridgeDeadLetter{
		EventID: "death-1", EventType: "PARSE_FAILED",
		RawLine: `'tiantian' (UserId=steam_76561199032061430, IP=1.2.3.4, UID=F23D556C-00000000-00000000-00000000) was attacked by a wild 'CaptainPenguin' (ID: CaptainPenguin) and died.`,
	})
	if err != nil {
		t.Fatal(err)
	}
	playerDeath, err = service.GetBridgeDeadLetter(ctx, playerDeath.ID)
	if err != nil {
		t.Fatal(err)
	}
	if playerDeath.Status != "dismissed" {
		t.Fatalf("player death status=%q", playerDeath.Status)
	}
}
