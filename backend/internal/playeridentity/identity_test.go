package playeridentity

import "testing"

func TestNormalizePalworldUIDVariants(t *testing.T) {
	variants := []string{
		"F23D556C000000000000000000000000",
		"F23D556C-00000000-00000000-00000000",
		"f23d556c-0000-0000-0000-000000000000",
		"{F23D556C-0000-0000-0000-000000000000}",
	}
	want := "F23D556C000000000000000000000000"
	for _, value := range variants {
		if got := Normalize(value); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestNormalizeSteamID(t *testing.T) {
	for input, want := range map[string]string{
		"76561199032061430":       "steam_76561199032061430",
		"STEAM_76561199032061430": "steam_76561199032061430",
		"gdk_example":             "gdk_example",
	} {
		if got := NormalizeSteamID(input); got != want {
			t.Fatalf("NormalizeSteamID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizePlatformID(t *testing.T) {
	if got := Normalize(" Steam_76561199032061430 "); got != "steam_76561199032061430" {
		t.Fatalf("unexpected platform id: %q", got)
	}
}

func TestUnknownIdentityIsPreserved(t *testing.T) {
	if got := Normalize("Player-Alpha"); got != "Player-Alpha" {
		t.Fatalf("unknown identity changed: %q", got)
	}
}
