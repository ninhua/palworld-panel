package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDetailedAuditTargetPreservesSingleParameterCompatibility(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Params = gin.Params{{Key: "id", Value: "steam_1"}}

	if got := detailedAuditTarget(context); got != "steam_1" {
		t.Fatalf("single-parameter audit target = %q, want steam_1", got)
	}
}

func TestDetailedAuditTargetLabelsMultipleParameters(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Params = gin.Params{
		{Key: "guild_id", Value: "guild-1"},
		{Key: "player_id", Value: "player-1"},
	}

	if got := detailedAuditTarget(context); got != "guild_id=guild-1, player_id=player-1" {
		t.Fatalf("multi-parameter audit target = %q", got)
	}
}

func TestDetailedAuditTargetWithoutParameters(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	if got := detailedAuditTarget(context); got != "" {
		t.Fatalf("empty audit target = %q", got)
	}
}
