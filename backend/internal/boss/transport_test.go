package boss

import (
	"context"
	"errors"
	"testing"
)

func TestParticipantTransportLifecycleAndLease(t *testing.T) {
	service, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	template, err := service.CreateTemplate(ctx, TemplateInput{
		Name: "transport", PalID: "SheepBall", Level: 1, Count: 1,
		HPMultiplier: 1, AttackMultiplier: 1, DefenseMultiplier: 1,
		Location: Location{X: 100, Y: 200, Z: 300}, Enabled: true, Metadata: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	summonResult, err := service.CreateSummon(ctx, CreateSummonRequest{TemplateID: template.ID, RequestKey: "transport-test", Metadata: map[string]any{}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	radius := 0.0
	if _, err := service.ConfigureRegistration(ctx, summonResult.Summon.ID, RegistrationPolicyUpdate{Enabled: &enabled, Radius: &radius}, "test"); err != nil {
		t.Fatal(err)
	}
	participant, err := service.RegisterParticipant(ctx, summonResult.Summon.ID, ParticipantInput{PlayerUID: "00112233-4455-6677-8899-AABBCCDDEEFF", Nickname: "Tester"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !participant.Participant.Eligible {
		t.Fatalf("participant should be eligible: %+v", participant.Participant)
	}

	holder, err := service.AcquireTransportOperation(ctx, summonResult.Summon.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcquireTransportOperation(ctx, summonResult.Summon.ID); !errors.Is(err, ErrTransportBusy) {
		t.Fatalf("expected busy lease, got %v", err)
	}
	if err := service.ReleaseTransportOperation(ctx, summonResult.Summon.ID, holder); err != nil {
		t.Fatal(err)
	}

	origin := Location{X: 1, Y: 2, Z: 3}
	destination := Location{X: 100, Y: 200, Z: 300}
	prepared, duplicate, err := service.PrepareParticipantTransport(ctx, summonResult.Summon.ID, participant.Participant.PlayerUID, participant.Participant.PlayerUID, origin, destination, "test", map[string]any{"source": "test"})
	if err != nil || duplicate || prepared.State != TransportStatePrepared {
		t.Fatalf("prepared=%+v duplicate=%v err=%v", prepared, duplicate, err)
	}
	teleported, err := service.CompleteParticipantTeleport(ctx, summonResult.Summon.ID, participant.Participant.PlayerUID, "test", nil)
	if err != nil || teleported.State != TransportStateTeleported || teleported.TeleportAttempts != 1 {
		t.Fatalf("teleported=%+v err=%v", teleported, err)
	}
	duplicateItem, duplicate, err := service.PrepareParticipantTransport(ctx, summonResult.Summon.ID, participant.Participant.PlayerUID, participant.Participant.PlayerUID, Location{X: 9}, destination, "test", nil)
	if err != nil || !duplicate || duplicateItem.Origin.X != origin.X {
		t.Fatalf("duplicate item=%+v duplicate=%v err=%v", duplicateItem, duplicate, err)
	}
	returned, err := service.CompleteParticipantReturn(ctx, summonResult.Summon.ID, participant.Participant.PlayerUID, "test", nil)
	if err != nil || returned.State != TransportStateReturned || returned.ReturnAttempts != 1 {
		t.Fatalf("returned=%+v err=%v", returned, err)
	}
	snapshot, err := service.TransportSnapshot(ctx, summonResult.Summon.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Count != 1 || snapshot.Returned != 1 || snapshot.Teleported != 0 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestPreparedCheckpointCanBeSafelyReturnedAfterInterruption(t *testing.T) {
	service, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	template, err := service.CreateTemplate(ctx, TemplateInput{
		Name: "transport-interrupted", PalID: "SheepBall", Level: 1, Count: 1,
		HPMultiplier: 1, AttackMultiplier: 1, DefenseMultiplier: 1,
		Location: Location{X: 100, Y: 200, Z: 300}, Enabled: true, Metadata: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	summonResult, err := service.CreateSummon(ctx, CreateSummonRequest{TemplateID: template.ID, RequestKey: "transport-interrupted-test", Metadata: map[string]any{}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	radius := 0.0
	if _, err := service.ConfigureRegistration(ctx, summonResult.Summon.ID, RegistrationPolicyUpdate{Enabled: &enabled, Radius: &radius}, "test"); err != nil {
		t.Fatal(err)
	}
	participant, err := service.RegisterParticipant(ctx, summonResult.Summon.ID, ParticipantInput{PlayerUID: "ABCDEF00112233445566778899AABBCC", Nickname: "Interrupted"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	origin := Location{X: 7, Y: 8, Z: 9}
	destination := Location{X: 100, Y: 200, Z: 300}
	prepared, duplicate, err := service.PrepareParticipantTransport(ctx, summonResult.Summon.ID, participant.Participant.PlayerUID, participant.Participant.PlayerUID, origin, destination, "test", nil)
	if err != nil || duplicate || prepared.State != TransportStatePrepared {
		t.Fatalf("prepared=%+v duplicate=%v err=%v", prepared, duplicate, err)
	}
	blocked, duplicate, err := service.PrepareParticipantTransport(ctx, summonResult.Summon.ID, participant.Participant.PlayerUID, participant.Participant.PlayerUID, Location{X: 999}, destination, "test", nil)
	if err != nil || !duplicate || blocked.State != TransportStatePrepared || blocked.Origin.X != origin.X {
		t.Fatalf("blocked=%+v duplicate=%v err=%v", blocked, duplicate, err)
	}
	returned, err := service.CompleteParticipantReturn(ctx, summonResult.Summon.ID, participant.Participant.PlayerUID, "test", nil)
	if err != nil || returned.State != TransportStateReturned || returned.ReturnAttempts != 1 {
		t.Fatalf("returned=%+v err=%v", returned, err)
	}
}

func TestParticipantTransportRejectsIneligibleParticipant(t *testing.T) {
	service, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	template, err := service.CreateTemplate(ctx, TemplateInput{
		Name: "transport-outside", PalID: "SheepBall", Level: 1, Count: 1,
		HPMultiplier: 1, AttackMultiplier: 1, DefenseMultiplier: 1,
		Location: Location{}, Enabled: true, Metadata: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	summonResult, err := service.CreateSummon(ctx, CreateSummonRequest{TemplateID: template.ID, RequestKey: "transport-outside-test", Metadata: map[string]any{}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	radius := 10.0
	if _, err := service.ConfigureRegistration(ctx, summonResult.Summon.ID, RegistrationPolicyUpdate{Enabled: &enabled, Radius: &radius}, "test"); err != nil {
		t.Fatal(err)
	}
	participant, err := service.RegisterParticipant(ctx, summonResult.Summon.ID, ParticipantInput{PlayerUID: "11112222333344445555666677778888", Location: &Location{X: 100}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if participant.Participant.Eligible {
		t.Fatal("participant should be outside")
	}
	_, _, err = service.PrepareParticipantTransport(ctx, summonResult.Summon.ID, participant.Participant.PlayerUID, participant.Participant.PlayerUID, Location{}, Location{}, "test", nil)
	if !errors.Is(err, ErrTransportStateConflict) {
		t.Fatalf("expected state conflict, got %v", err)
	}
}
