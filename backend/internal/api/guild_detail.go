package api

import (
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/saveindex"
)

func (s Server) guildDetailViews(c *gin.Context, index saveindex.Index, guild saveindex.Guild, online onlinePlayersResult) ([]gin.H, []gin.H, string, error) {
	source, err := s.store.ActiveSaveSource(c.Request.Context())
	if err != nil {
		return nil, nil, "", err
	}
	annotations, err := s.loadPlayerAnnotations(c, source.ID)
	if err != nil {
		return nil, nil, "", err
	}
	customNames, err := s.loadBaseCustomNames(c, source.ID)
	if err != nil {
		return nil, nil, "", err
	}
	players := mergeSaveAndOnline(index.Players, online.Players)
	return guildMemberDetailViews(guild, players, online, annotations), guildBaseDetailViews(guild, index.Bases, customNames), source.ID, nil
}

func guildMemberDetailViews(guild saveindex.Guild, players []saveindex.Player, online onlinePlayersResult, annotations map[string]playerAnnotation) []gin.H {
	lookup := make(map[string]saveindex.Player, len(players)*2)
	for _, player := range players {
		for _, identifier := range []string{player.PlayerUID, player.SteamID} {
			if key := identityKey(identifier); key != "" {
				lookup[key] = player
			}
		}
	}

	out := make([]gin.H, 0, len(guild.Members)+1)
	seen := make(map[string]struct{}, len(guild.Members)+1)
	appendMember := func(member saveindex.GuildMember) {
		key := identityKey(member.PlayerUID)
		if key == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}

		player, found := lookup[key]
		var view gin.H
		if found {
			view = flattenPlayerWithAnnotation(player, online, annotationForPlayer(annotations, player))
			view["last_online_time"] = firstNonEmpty(member.LastOnlineTime, player.LastOnlineTime)
		} else {
			view = gin.H{
				"id":                    member.PlayerUID,
				"player_uid":            member.PlayerUID,
				"steam_id":              "",
				"nickname":              firstNonEmpty(member.Nickname, member.PlayerUID),
				"level":                 0,
				"is_online":             false,
				"last_online_time":      member.LastOnlineTime,
				"note":                  "",
				"tags":                  []string{},
				"has_annotation":        false,
				"annotation_updated_at": "",
			}
		}
		view["is_owner"] = strings.EqualFold(strings.TrimSpace(guild.OwnerPlayerUID), strings.TrimSpace(member.PlayerUID))
		out = append(out, view)
	}

	for _, member := range guild.Members {
		appendMember(member)
	}
	ownerKey := identityKey(guild.OwnerPlayerUID)
	if ownerKey != "" {
		if _, exists := seen[ownerKey]; !exists {
			owner := saveindex.GuildMember{PlayerUID: guild.OwnerPlayerUID}
			if player, found := lookup[ownerKey]; found {
				owner.Nickname = player.Nickname
				owner.LastOnlineTime = player.LastOnlineTime
			}
			appendMember(owner)
		}
	}
	return out
}

func guildBaseDetailViews(guild saveindex.Guild, bases []saveindex.Base, customNames map[string]string) []gin.H {
	lookup := make(map[string]saveindex.Base, len(bases))
	for _, base := range bases {
		lookup[identityKey(base.ID)] = base
	}
	out := make([]gin.H, 0, len(guild.BaseIDs))
	seen := make(map[string]struct{}, len(guild.BaseIDs))
	appendBase := func(base saveindex.Base) {
		key := identityKey(base.ID)
		if key == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		view := flattenBaseWithCustomName(base, customNames[base.ID])
		view["workers_count"] = len(base.Workers)
		out = append(out, view)
	}
	for _, baseID := range guild.BaseIDs {
		if base, found := lookup[identityKey(baseID)]; found {
			appendBase(base)
		}
	}
	for _, base := range bases {
		if strings.EqualFold(strings.TrimSpace(base.GuildID), strings.TrimSpace(guild.ID)) {
			appendBase(base)
		}
	}
	return out
}
