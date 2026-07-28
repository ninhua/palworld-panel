package api

import "testing"

func TestPlayerSummaryFeatureRegistered(t *testing.T) {
	for _, feature := range patchFeatures {
		if feature == "player-summary" {
			return
		}
	}
	t.Fatalf("player-summary not registered in %#v", patchFeatures)
}
