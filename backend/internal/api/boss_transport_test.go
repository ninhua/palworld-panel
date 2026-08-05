package api

import (
	"math"
	"testing"

	"palpanel/internal/boss"
)

func TestSpreadBossTransportDestination(t *testing.T) {
	center := boss.Location{X: 100, Y: 200, Z: 300}
	first := spreadBossTransportDestination(center, 0, 4, 50)
	if math.Abs(first.X-150) > 0.0001 || math.Abs(first.Y-200) > 0.0001 || first.Z != center.Z {
		t.Fatalf("first=%+v", first)
	}
	third := spreadBossTransportDestination(center, 2, 4, 50)
	if math.Abs(third.X-50) > 0.0001 || math.Abs(third.Y-200) > 0.0001 {
		t.Fatalf("third=%+v", third)
	}
	if got := spreadBossTransportDestination(center, 0, 1, 50); got != center {
		t.Fatalf("single destination=%+v", got)
	}
}

func TestSelectBossTransportParticipants(t *testing.T) {
	participants := []boss.Participant{
		{PlayerUID: "0000000000000000000000000000000B", Eligible: true, Status: boss.ParticipantStatusCheckedIn, RegisteredAt: "2"},
		{PlayerUID: "0000000000000000000000000000000A", Eligible: true, Status: boss.ParticipantStatusCheckedIn, RegisteredAt: "1"},
		{PlayerUID: "0000000000000000000000000000000C", Eligible: false, Status: boss.ParticipantStatusRegistered, RegisteredAt: "0"},
		{PlayerUID: "0000000000000000000000000000000D", Eligible: true, Status: boss.ParticipantStatusCancelled, RegisteredAt: "0"},
	}
	selected := selectBossTransportParticipants(participants, nil)
	if len(selected) != 2 || selected[0].PlayerUID != "0000000000000000000000000000000A" || selected[1].PlayerUID != "0000000000000000000000000000000B" {
		t.Fatalf("selected=%+v", selected)
	}
	filtered := selectBossTransportParticipants(participants, []string{"0000000000000000000000000000000b"})
	if len(filtered) != 1 || filtered[0].PlayerUID != "0000000000000000000000000000000B" {
		t.Fatalf("filtered=%+v", filtered)
	}
}
