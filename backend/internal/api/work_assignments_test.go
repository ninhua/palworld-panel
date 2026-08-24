package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkAssignmentTokenExpiresAndCannotBeReplayed(t *testing.T) {
	store := newWorkAssignmentTokenStore()
	store.put("token", workAssignmentToken{ExpiresAt: time.Now().Add(time.Minute)})
	if _, ok := store.take("token", time.Now()); !ok {
		t.Fatal("expected first token use to succeed")
	}
	if _, ok := store.take("token", time.Now()); ok {
		t.Fatal("expected token replay to fail")
	}
	store.put("expired", workAssignmentToken{ExpiresAt: time.Now().Add(-time.Second)})
	if _, ok := store.take("expired", time.Now()); ok {
		t.Fatal("expected expired token to fail")
	}
}

func TestValidateWorkAssignmentRequestSeparatesGUIDsFromDefineID(t *testing.T) {
	input := workAssignmentRequest{
		WorkerInstanceID:              "31112233-4455-6677-8899-aabbccddeeff",
		WorkBaseID:                    "11112233-4455-6677-8899-aabbccddeeff",
		OwnerMapObjectConcreteModelID: "21112233-4455-6677-8899-aabbccddeeff",
		AssignDefineDataID:            "AncientBlastFurnace_0",
		LocationIndex:                 2,
	}
	if err := validateWorkAssignmentRequest("00112233-4455-6677-8899-aabbccddeeff", input); err != nil {
		t.Fatal(err)
	}
	input.WorkBaseID = "not-a-guid"
	if err := validateWorkAssignmentRequest("00112233-4455-6677-8899-aabbccddeeff", input); err == nil {
		t.Fatal("expected invalid work GUID rejection")
	}
}

func TestDecodeWorkAssignmentHelperResponses(t *testing.T) {
	plan, err := decodeWorkAssignmentPlan([]byte(`{"fixed":0}`))
	if err != nil || plan.Fixed != 0 {
		t.Fatalf("decode plan: %#v, %v", plan, err)
	}
	result, err := decodeWorkAssignmentFixResult([]byte(`{"plan":{"fixed":1},"changed":true}`))
	if err != nil || result.Plan.Fixed != 1 || !result.Changed {
		t.Fatalf("decode fix result: %#v, %v", result, err)
	}
	list, err := decodeWorkAssignmentList([]byte(`{"level_sha256":"abc","base_camp_id":"00112233-4455-6677-8899-aabbccddeeff","work_bases":[],"assignments":[],"unscoped_assignments":[]}`))
	if err != nil || list.LevelSHA256 != "abc" || list.BaseCampID == "" || list.WorkBases == nil || list.Assignments == nil || list.UnscopedAssignments == nil {
		t.Fatalf("decode list result: %#v, %v", list, err)
	}
}

func TestPublishAndRollbackLevelFile(t *testing.T) {
	root := t.TempDir()
	world := filepath.Join(root, "world")
	backup := filepath.Join(root, "backup")
	stage := filepath.Join(root, "stage")
	for _, dir := range []string{world, backup, stage} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	active := filepath.Join(world, "Level.sav")
	backupLevel := filepath.Join(backup, "Level.sav")
	stagedLevel := filepath.Join(stage, "Level.sav")
	if err := os.WriteFile(active, []byte("before"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupLevel, []byte("before"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedLevel, []byte("after"), 0o640); err != nil {
		t.Fatal(err)
	}
	publishedHash, err := publishLevelFile(stagedLevel, active)
	if err != nil {
		t.Fatal(err)
	}
	if err := rollbackLevelFile(active, backupLevel, publishedHash); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(active)
	if err != nil || string(body) != "before" {
		t.Fatalf("rollback body = %q, err = %v", body, err)
	}
}
