package gameevents

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestPlayerIdentityMigrationCanonicalizesEventsAndIgnoresSendMsg(t *testing.T) {
	service, err := Open(filepath.Join(t.TempDir(), "events.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if err := service.EnsureBridgeSchema(context.Background()); err != nil {
		t.Fatal(err)
	}

	const hyphens = "F23D556C-00000000-00000000-00000000"
	if _, err := service.db.Exec(`INSERT INTO game_events(event_id,type,player_uid,nickname,steam_id,occurred_at,payload_json,status,result_json,error,attempts,created_at,updated_at)
		VALUES('evt_test','PLAYER_CHAT',?,'tiantian','steam_76561199032061430','','{}','completed','{}','',1,'now','now')`, hyphens); err != nil {
		t.Fatal(err)
	}
	line := `[18:08:38][info] [Player::SendMsg::Say][1 receiver(s)] -> [SYSTEM] 【任务】第1/1页`
	if _, err := service.AddBridgeDeadLetter(context.Background(), BridgeDeadLetter{
		EventID: "sendmsg_old", EventType: "PARSE_FAILED", RawLine: line, Sample: line, Reason: "parse failed",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.EnsurePlayerIdentityConsistency(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, err := service.Get(context.Background(), "evt_test")
	if err != nil {
		t.Fatal(err)
	}
	if record.PlayerUID != "F23D556C000000000000000000000000" {
		t.Fatalf("player uid = %q", record.PlayerUID)
	}
	old, err := service.GetBridgeDeadLetterByEventID(context.Background(), "sendmsg_old")
	if err != nil {
		t.Fatal(err)
	}
	if old.Status != "dismissed" {
		t.Fatalf("old system reply status = %q", old.Status)
	}

	created, err := service.AddBridgeDeadLetter(context.Background(), BridgeDeadLetter{
		EventID: "sendmsg_new", EventType: "PARSE_FAILED", RawLine: line, Sample: line, Reason: "parse failed",
	})
	if err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	if err == nil && created.Status != "dismissed" {
		t.Fatalf("new system reply status = %q", created.Status)
	}
	pending, err := service.CountBridgeDeadLetters(context.Background(), "pending")
	if err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("pending dead letters = %d", pending)
	}
}
