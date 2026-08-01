package playeruid

import "testing"

func TestCalculateKnownSteamAndNoSteamUIDs(t *testing.T) {
	pair, err := Calculate("76561198452436974")
	if err != nil {
		t.Fatal(err)
	}
	if pair.Steam != "25527209-0000-0000-0000-000000000000" {
		t.Fatalf("Steam UID = %q", pair.Steam)
	}
	if pair.NoSteam != "f8f86740-0000-0000-0000-000000000000" {
		t.Fatalf("NoSteam UID = %q", pair.NoSteam)
	}
}

func TestCalculateRejectsInvalidSteamID(t *testing.T) {
	for _, value := range []string{"", "steam_76561198452436974", "123", "7656119845243697x"} {
		if _, err := Calculate(value); err == nil {
			t.Fatalf("Calculate(%q) succeeded", value)
		}
	}
}

func TestDetectModeRequiresOneConsistentMode(t *testing.T) {
	steam, err := DetectMode([]Identity{{SteamID: "76561198452436974", PlayerUID: "25527209-0000-0000-0000-000000000000"}})
	if err != nil || steam.Mode != ModeSteam || steam.Matched != 1 || steam.Total != 1 {
		t.Fatalf("Steam detection = %#v, %v", steam, err)
	}
	nosteam, err := DetectMode([]Identity{{SteamID: "76561198452436974", PlayerUID: "f8f86740-0000-0000-0000-000000000000"}})
	if err != nil || nosteam.Mode != ModeNoSteam {
		t.Fatalf("NoSteam detection = %#v, %v", nosteam, err)
	}
	unknown, err := DetectMode([]Identity{{SteamID: "76561198452436974", PlayerUID: "aaaaaaaa-0000-0000-0000-000000000000"}})
	if err != nil || unknown.Mode != ModeUnknown || unknown.Matched != 0 {
		t.Fatalf("unknown detection = %#v, %v", unknown, err)
	}
}
