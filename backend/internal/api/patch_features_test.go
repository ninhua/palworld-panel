package api

import "testing"

func TestNormalizePatchFeaturesStableAndUnique(t *testing.T) {
	got := normalizePatchFeatures([]string{"alpha", " beta ", "alpha", "", "beta", "gamma", "gamma"})
	want := []string{"alpha", "beta", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("normalizePatchFeatures() = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("normalizePatchFeatures() = %#v, want %#v", got, want)
		}
	}
}

func TestPatchFeaturesAdvertiseRetainedFixes(t *testing.T) {
	features := normalizePatchFeatures(patchFeatures)
	seen := make(map[string]bool, len(features))
	for _, feature := range features {
		if seen[feature] {
			t.Fatalf("normalized patch feature list contains duplicate %q", feature)
		}
		seen[feature] = true
	}
	for _, required := range []string{
		"save-migration-wizard-entry",
		"upstream-save-migration-primary-ui",
		"palpanel-bridge-runtime-diagnostics",
		"china-timezone-default",
		"patch-feature-deduplication",
	} {
		if !seen[required] {
			t.Fatalf("normalized patch feature list is missing %q", required)
		}
	}
}
