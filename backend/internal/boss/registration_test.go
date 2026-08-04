package boss

import (
	"context"
	"testing"
)

func TestParticipantAreaUsesHorizontalDistanceByDefault(t *testing.T) {
	policy := RegistrationPolicy{Radius: 100, Center: Location{X: 0, Y: 0, Z: 500}, UseZ: false}
	status, distance, eligible := participantArea(policy, &Location{X: 60, Y: 80, Z: 5000})
	if status != ParticipantAreaEligible || !eligible || distance != 100 {
		t.Fatalf("status=%q distance=%v eligible=%v", status, distance, eligible)
	}
}

func TestParticipantAreaCanIncludeZ(t *testing.T) {
	policy := RegistrationPolicy{Radius: 100, Center: Location{}, UseZ: true}
	status, _, eligible := participantArea(policy, &Location{X: 60, Y: 80, Z: 1})
	if status != ParticipantAreaOutside || eligible {
		t.Fatalf("status=%q eligible=%v", status, eligible)
	}
}

func TestRegistrationActionAliases(t *testing.T) {
	for _, action := range []string{"registration_configure", "registration-configure", "participant_register", "participant-check-area", "participant_cancel", "registration_snapshot"} {
		if !IsRegistrationControlAction(action) {
			t.Fatalf("action %q not recognized", action)
		}
	}
	if IsRegistrationControlAction("active") {
		t.Fatal("normal summon transition was recognized as registration control")
	}
}

func TestRegistrationSchemaAndCap(t *testing.T) {
	service, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	template, err := service.CreateTemplate(ctx, TemplateInput{
		Name: "registration", PalID: "SheepBall", Level: 1, Count: 1,
		HPMultiplier: 1, AttackMultiplier: 1, DefenseMultiplier: 1,
		Location: Location{}, Enabled: true, Metadata: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	summonResult, err := service.CreateSummon(ctx, CreateSummonRequest{TemplateID: template.ID, RequestKey: "registration-test", Metadata: map[string]any{}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	maxPlayers := 1
	radius := 100.0
	if _, err := service.ConfigureRegistration(ctx, summonResult.Summon.ID, RegistrationPolicyUpdate{Enabled: &enabled, MaxPlayers: &maxPlayers, Radius: &radius}, "test"); err != nil {
		t.Fatal(err)
	}
	first, err := service.RegisterParticipant(ctx, summonResult.Summon.ID, ParticipantInput{PlayerUID: "00112233-4455-6677-8899-AABBCCDDEEFF", Location: &Location{X: 10}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !first.Participant.Eligible || first.Participant.PlayerUID != "00112233445566778899AABBCCDDEEFF" {
		t.Fatalf("participant=%+v", first.Participant)
	}
	if _, err := service.RegisterParticipant(ctx, summonResult.Summon.ID, ParticipantInput{PlayerUID: "11112222333344445555666677778888"}, "test"); err != ErrRegistrationFull {
		t.Fatalf("expected full, got %v", err)
	}
	if _, err := service.CancelParticipant(ctx, summonResult.Summon.ID, ParticipantCancelInput{PlayerUID: first.Participant.PlayerUID}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterParticipant(ctx, summonResult.Summon.ID, ParticipantInput{PlayerUID: "11112222333344445555666677778888"}, "test"); err != nil {
		t.Fatalf("slot was not released: %v", err)
	}
}
