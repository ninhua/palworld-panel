package indexer

import (
	"regexp"
	"strings"
	"unicode"
)

var escapedBytePattern = regexp.MustCompile(`\\[xX][0-9A-Fa-f]{2}`)

const IndexVersion = 2

type Coordinates struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type Player struct {
	PlayerUID        string         `json:"player_uid"`
	SteamID          string         `json:"steam_id"`
	Nickname         string         `json:"nickname"`
	Level            int            `json:"level"`
	GuildID          string         `json:"guild_id"`
	GuildName        string         `json:"guild_name"`
	IsOnline         bool           `json:"is_online"`
	LastOnlineTime   string         `json:"last_online_time"`
	Location         Coordinates    `json:"location"`
	InventorySummary map[string]any `json:"inventory_summary,omitempty"`
	Raw              any            `json:"-"`
}

type GuildMember struct {
	PlayerUID      string `json:"player_uid"`
	Nickname       string `json:"nickname"`
	LastOnlineTime string `json:"last_online_time,omitempty"`
}

type Guild struct {
	ID                string        `json:"id"`
	Name              string        `json:"name"`
	OwnerPlayerUID    string        `json:"owner_player_uid"`
	Members           []GuildMember `json:"members"`
	BaseIDs           []string      `json:"base_ids"`
	OnlineMemberCount int           `json:"online_member_count"`
	Raw               any           `json:"-"`
}

type Worker struct {
	InstanceID  string `json:"instance_id"`
	CharacterID string `json:"character_id"`
	Nickname    string `json:"nickname,omitempty"`
	Level       int    `json:"level,omitempty"`
}

type Base struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	GuildID         string      `json:"guild_id"`
	GuildName       string      `json:"guild_name"`
	Location        Coordinates `json:"location"`
	StructuresCount int         `json:"structures_count"`
	Workers         []Worker    `json:"workers"`
	Containers      []string    `json:"containers"`
	Status          string      `json:"status"`
	Raw             any         `json:"-"`
}

type WorkSuitability struct {
	Type  string `json:"type"`
	Level int    `json:"level"`
}

type Pal struct {
	InstanceID      string            `json:"instance_id"`
	CharacterID     string            `json:"character_id"`
	Nickname        string            `json:"nickname"`
	Level           int               `json:"level"`
	OwnerPlayerUID  string            `json:"owner_player_uid"`
	OldOwnerUIDs    []string          `json:"old_owner_uids"`
	GuildID         string            `json:"guild_id"`
	ContainerID     string            `json:"container_id"`
	SlotIndex       int               `json:"slot_index"`
	LocationType    string            `json:"location_type"`
	Location        Coordinates       `json:"location"`
	Gender          string            `json:"gender"`
	Rank            int               `json:"rank"`
	IVHP            int               `json:"iv_hp"`
	IVAttack        int               `json:"iv_attack"`
	IVDefense       int               `json:"iv_defense"`
	Skills          []string          `json:"skills"`
	EquippedSkills  []string          `json:"equipped_skills"`
	Passives        []string          `json:"passives"`
	WorkSuitability []WorkSuitability `json:"work_suitability"`
	Health          *int64            `json:"health,omitempty"`
	Sanity          *float64          `json:"sanity,omitempty"`
	FullStomach     *float64          `json:"full_stomach,omitempty"`
	IsSick          bool              `json:"is_sick"`
	OnExpedition    bool              `json:"on_expedition"`
	Status          string            `json:"status"`
	Raw             any               `json:"-"`
}

type Slot struct {
	Slot       int      `json:"slot"`
	ItemID     string   `json:"item_id"`
	Count      int      `json:"count"`
	Durability *float64 `json:"durability,omitempty"`
}

type Container struct {
	ContainerID   string `json:"container_id"`
	ContainerType string `json:"container_type,omitempty"`
	OwnerType     string `json:"owner_type"`
	OwnerID       string `json:"owner_id"`
	Slots         []Slot `json:"slots"`
}

type MapEntity struct {
	Type     string      `json:"type"`
	ID       string      `json:"id"`
	Label    string      `json:"label"`
	Location Coordinates `json:"location"`
}

type Counts struct {
	Players     int `json:"players"`
	Guilds      int `json:"guilds"`
	Bases       int `json:"bases"`
	Pals        int `json:"pals"`
	Containers  int `json:"containers"`
	MapEntities int `json:"map_entities"`
}

type SnapshotFile struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"`
}

type Snapshot struct {
	Fingerprint string         `json:"fingerprint"`
	Files       []SnapshotFile `json:"files,omitempty"`
}

type Index struct {
	Version     int         `json:"version"`
	SourcePath  string      `json:"source_path"`
	GeneratedAt string      `json:"generated_at"`
	DurationMS  int         `json:"duration_ms"`
	Parser      string      `json:"parser"`
	Warnings    []string    `json:"warnings"`
	Players     []Player    `json:"players"`
	Guilds      []Guild     `json:"guilds"`
	Bases       []Base      `json:"bases"`
	Pals        []Pal       `json:"pals"`
	Containers  []Container `json:"containers"`
	MapEntities []MapEntity `json:"map_entities"`
	Snapshot    Snapshot    `json:"snapshot"`
	Counts      Counts      `json:"counts"`
}

func EmptyIndex(sourcePath, parser string) Index {
	return Index{
		Version:     IndexVersion,
		SourcePath:  sourcePath,
		GeneratedAt: UTCNow(),
		Parser:      parser,
		Warnings:    []string{},
		Players:     []Player{},
		Guilds:      []Guild{},
		Bases:       []Base{},
		Pals:        []Pal{},
		Containers:  []Container{},
		MapEntities: []MapEntity{},
	}
}

func (i *Index) Finalize() {
	if i.Warnings == nil {
		i.Warnings = []string{}
	}
	for index, warning := range i.Warnings {
		i.Warnings[index] = safeDiagnostic(warning)
	}
	if i.Players == nil {
		i.Players = []Player{}
	}
	if i.Guilds == nil {
		i.Guilds = []Guild{}
	}
	if i.Bases == nil {
		i.Bases = []Base{}
	}
	if i.Pals == nil {
		i.Pals = []Pal{}
	}
	if i.Containers == nil {
		i.Containers = []Container{}
	}
	if i.MapEntities == nil {
		i.MapEntities = []MapEntity{}
	}
	i.Counts = Counts{
		Players:     len(i.Players),
		Guilds:      len(i.Guilds),
		Bases:       len(i.Bases),
		Pals:        len(i.Pals),
		Containers:  len(i.Containers),
		MapEntities: len(i.MapEntities),
	}
}

func safeDiagnostic(value string) string {
	value = strings.ToValidUTF8(value, "")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return -1
		}
		return r
	}, value)
	value = escapedBytePattern.ReplaceAllString(value, "[byte]")
	runes := []rune(value)
	if len(runes) > 512 {
		value = string(runes[:512]) + "…"
	}
	return value
}
