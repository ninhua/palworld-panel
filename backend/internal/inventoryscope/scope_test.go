package inventoryscope

import (
	"testing"

	"palpanel/internal/saveindex"
)

func TestClassifierTrustsOnlyIndexedOwnership(t *testing.T) {
	index := saveindex.Index{
		Players: []saveindex.Player{{PlayerUID: "player-1", SteamID: "steam-1"}},
		Guilds:  []saveindex.Guild{{ID: "guild-1"}},
		Bases:   []saveindex.Base{{ID: "base-1", Containers: []string{"base-box"}}},
	}
	classifier := New(index)
	tests := []struct {
		name      string
		container saveindex.Container
		ownerType string
		trusted   bool
		reason    string
	}{
		{"player", saveindex.Container{OwnerType: "player", OwnerID: "PLAYER-1"}, OwnerPlayer, true, "indexed_player"},
		{"steam alias", saveindex.Container{OwnerType: "player", OwnerID: "steam-1"}, OwnerPlayer, true, "indexed_player"},
		{"linked base", saveindex.Container{ContainerID: "BASE-BOX", OwnerType: "map_object"}, OwnerBase, true, "linked_base_container"},
		{"base", saveindex.Container{OwnerType: "base", OwnerID: "base-1"}, OwnerBase, true, "indexed_base"},
		{"guild", saveindex.Container{OwnerType: "guild", OwnerID: "guild-1"}, OwnerGuild, true, "indexed_guild"},
		{"spoofed player", saveindex.Container{OwnerType: "player", OwnerID: "missing"}, OwnerUnknown, false, "unresolved_player"},
		{"world", saveindex.Container{OwnerType: "map_object", OwnerID: "wild-box"}, OwnerUnknown, false, "world_container"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifier.Resolve(test.container)
			if got.Type != test.ownerType || got.Trusted != test.trusted || got.Reason != test.reason {
				t.Fatalf("Resolve(%#v) = %#v", test.container, got)
			}
		})
	}
}
