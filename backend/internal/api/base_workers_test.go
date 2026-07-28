package api

import (
	"testing"

	"github.com/gin-gonic/gin"

	"palpanel/internal/saveindex"
)

func TestBaseWorkerViewsMergeIndexedPalDetails(t *testing.T) {
	base := saveindex.Base{Workers: []saveindex.Worker{
		{InstanceID: "worker-1", CharacterID: "Anubis", Nickname: "矿工", Level: 0},
		{InstanceID: "worker-2", CharacterID: "PinkCat", Level: 12},
	}}
	pals := []saveindex.Pal{
		{InstanceID: "worker-1", CharacterID: "Anubis", Level: 50, Gender: "male", Rank: 4, Passives: []string{"Workaholic"}, Status: "Working"},
		{InstanceID: "other", CharacterID: "Ganesha", Level: 30},
	}

	views := baseWorkerViews(base, pals)
	if len(views) != 2 {
		t.Fatalf("workers = %d, want 2", len(views))
	}
	if views[0]["level"] != 50 || views[0]["gender"] != "male" || views[0]["rank"] != 4 {
		t.Fatalf("indexed details were not merged: %#v", views[0])
	}
	if views[0]["name"] != "矿工" || views[0]["status"] != "Working" {
		t.Fatalf("unexpected localized worker view: %#v", views[0])
	}
	if views[1]["level"] != 12 || views[1]["status"] != "Unknown" {
		t.Fatalf("worker fallback changed: %#v", views[1])
	}
}

func TestBaseWorkerSummary(t *testing.T) {
	summary := baseWorkerSummary([]gin.H{
		{"level": 50, "nickname": "矿工", "character_id": "Anubis"},
		{"level": 10, "nickname": "", "character_id": "Anubis"},
		{"level": 30, "nickname": "伐木", "character_id": "PinkCat"},
	})
	if summary["total"] != 3 || summary["max_level"] != 50 || summary["named_count"] != 2 || summary["species_count"] != 2 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if summary["average_level"] != 30.0 {
		t.Fatalf("average_level = %#v, want 30", summary["average_level"])
	}
}
