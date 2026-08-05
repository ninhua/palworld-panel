package inventoryscope

import (
	"strings"

	"palpanel/internal/saveindex"
)

const (
	OwnerPlayer  = "player"
	OwnerBase    = "base"
	OwnerGuild   = "guild"
	OwnerUnknown = "unknown"
)

// Classification is the authoritative inventory ownership decision shared by
// the global inventory browser and unattended-inventory snapshots.
type Classification struct {
	Type    string
	ID      string
	Trusted bool
	Reason  string
}

// Classifier resolves only ownership that can be proven from the current save
// index. Raw owner labels without a matching player/base/guild are not trusted.
type Classifier struct {
	players        map[string]string
	bases          map[string]string
	guilds         map[string]string
	baseContainers map[string]string
}

func New(index saveindex.Index) Classifier {
	classifier := Classifier{
		players:        make(map[string]string, len(index.Players)*2),
		bases:          make(map[string]string, len(index.Bases)),
		guilds:         make(map[string]string, len(index.Guilds)),
		baseContainers: make(map[string]string),
	}
	for _, player := range index.Players {
		preferred := firstNonEmpty(player.PlayerUID, player.SteamID)
		for _, value := range []string{player.PlayerUID, player.SteamID} {
			if key := CanonicalID(value); key != "" {
				classifier.players[key] = preferred
			}
		}
	}
	for _, base := range index.Bases {
		if key := CanonicalID(base.ID); key != "" {
			classifier.bases[key] = strings.TrimSpace(base.ID)
		}
		for _, containerID := range base.Containers {
			if key := CanonicalID(containerID); key != "" {
				classifier.baseContainers[key] = strings.TrimSpace(base.ID)
			}
		}
	}
	for _, guild := range index.Guilds {
		if key := CanonicalID(guild.ID); key != "" {
			classifier.guilds[key] = strings.TrimSpace(guild.ID)
		}
	}
	return classifier
}

func (classifier Classifier) Resolve(container saveindex.Container) Classification {
	if baseID, found := classifier.baseContainers[CanonicalID(container.ContainerID)]; found && baseID != "" {
		return Classification{Type: OwnerBase, ID: baseID, Trusted: true, Reason: "linked_base_container"}
	}

	ownerType := strings.ToLower(strings.TrimSpace(container.OwnerType))
	ownerKey := CanonicalID(container.OwnerID)
	switch ownerType {
	case OwnerPlayer:
		if ownerID, found := classifier.players[ownerKey]; found && ownerID != "" {
			return Classification{Type: OwnerPlayer, ID: ownerID, Trusted: true, Reason: "indexed_player"}
		}
		return Classification{Type: OwnerUnknown, ID: strings.TrimSpace(container.OwnerID), Reason: "unresolved_player"}
	case OwnerBase:
		if ownerID, found := classifier.bases[ownerKey]; found && ownerID != "" {
			return Classification{Type: OwnerBase, ID: ownerID, Trusted: true, Reason: "indexed_base"}
		}
		return Classification{Type: OwnerUnknown, ID: strings.TrimSpace(container.OwnerID), Reason: "unresolved_base"}
	case OwnerGuild:
		if ownerID, found := classifier.guilds[ownerKey]; found && ownerID != "" {
			return Classification{Type: OwnerGuild, ID: ownerID, Trusted: true, Reason: "indexed_guild"}
		}
		return Classification{Type: OwnerUnknown, ID: strings.TrimSpace(container.OwnerID), Reason: "unresolved_guild"}
	case "map_object", "mapobject", "world", "field", "drop", "dropped", "pickup", "loot", "spawn", "spawner":
		return Classification{Type: OwnerUnknown, ID: strings.TrimSpace(container.OwnerID), Reason: "world_container"}
	case "":
		return Classification{Type: OwnerUnknown, ID: strings.TrimSpace(container.OwnerID), Reason: "missing_owner_type"}
	default:
		return Classification{Type: OwnerUnknown, ID: strings.TrimSpace(container.OwnerID), Reason: "unsupported_owner_type"}
	}
}

func CanonicalID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "")
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
