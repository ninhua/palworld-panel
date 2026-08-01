package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/db"
	"palpanel/internal/playeruid"
	"palpanel/internal/saveindex"
	"palpanel/internal/server"
)

type saveMigrationMappingInput struct {
	SourceUID string `json:"source_uid"`
	SteamID   string `json:"steam_id"`
}

type saveMigrationInput struct {
	SourceID               string                      `json:"source_id"`
	TargetMode             string                      `json:"target_mode"`
	Mappings               []saveMigrationMappingInput `json:"mappings"`
	ExpectedFingerprint    string                      `json:"expected_fingerprint"`
	ManualModeConfirmation string                      `json:"manual_mode_confirmation"`
	Confirmation           string                      `json:"confirmation"`
}

type saveMigrationMappingPreview struct {
	SourceUID  string `json:"source_uid"`
	Nickname   string `json:"nickname"`
	Level      int    `json:"level"`
	SteamID    string `json:"steam_id"`
	SteamUID   string `json:"steam_uid"`
	NoSteamUID string `json:"nosteam_uid"`
	TargetUID  string `json:"target_uid,omitempty"`
}

type saveMigrationPreview struct {
	Source                     db.SaveSource                 `json:"source"`
	SourceFingerprint          string                        `json:"source_fingerprint"`
	TargetMode                 playeruid.Mode                `json:"target_mode"`
	ModeSource                 string                        `json:"mode_source"`
	ModeMatched                int                           `json:"mode_matched"`
	ModeTotal                  int                           `json:"mode_total"`
	RequiresManualConfirmation bool                          `json:"requires_manual_confirmation"`
	Mappings                   []saveMigrationMappingPreview `json:"mappings"`
	Conflicts                  []string                      `json:"conflicts"`
	Ready                      bool                          `json:"ready"`
	SourcePath                 string                        `json:"-"`
}

var migrationUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (s Server) saveMigrationPlayers(c *gin.Context) {
	source, index, _, err := s.loadMigrationSource(c.Request.Context(), c.Param("id"))
	if err != nil {
		failSaveMigration(c, err)
		return
	}
	players := make([]gin.H, 0, len(index.Players))
	for _, player := range index.Players {
		players = append(players, gin.H{
			"player_uid": player.PlayerUID,
			"steam_id":   player.SteamID,
			"nickname":   player.Nickname,
			"level":      player.Level,
			"guild_name": player.GuildName,
		})
	}
	ok(c, gin.H{"source": source, "source_fingerprint": index.Snapshot.Fingerprint, "players": players})
}

func (s Server) previewSaveMigration(c *gin.Context) {
	var input saveMigrationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "migration_request_invalid", err.Error())
		return
	}
	preview, err := s.buildSaveMigrationPreview(c.Request.Context(), input)
	if err != nil {
		failSaveMigration(c, err)
		return
	}
	ok(c, preview)
}

func (s Server) startSaveMigration(c *gin.Context) {
	var input saveMigrationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "migration_request_invalid", err.Error())
		return
	}
	if input.Confirmation != "MIGRATE PLAYERS" {
		fail(c, http.StatusBadRequest, "migration_confirmation_invalid", "confirmation must be exactly MIGRATE PLAYERS")
		return
	}
	preview, err := s.buildSaveMigrationPreview(c.Request.Context(), input)
	if err != nil {
		failSaveMigration(c, err)
		return
	}
	if input.ExpectedFingerprint != "" && input.ExpectedFingerprint != preview.SourceFingerprint {
		fail(c, http.StatusConflict, "migration_source_changed", "source save changed after preview; run the preview again")
		return
	}
	if preview.RequiresManualConfirmation {
		expected := "USE STEAM UID"
		if preview.TargetMode == playeruid.ModeNoSteam {
			expected = "USE NOSTEAM UID"
		}
		if input.ManualModeConfirmation != expected {
			fail(c, http.StatusBadRequest, "manual_mode_confirmation_required", "unproven target mode requires confirmation "+expected)
			return
		}
	}
	if !preview.Ready {
		fail(c, http.StatusConflict, "migration_preflight_failed", strings.Join(preview.Conflicts, "; "))
		return
	}
	mappings := make([]server.UIDMapping, 0, len(preview.Mappings))
	for _, mapping := range preview.Mappings {
		mappings = append(mappings, server.UIDMapping{SourceUID: mapping.SourceUID, TargetUID: mapping.TargetUID})
	}
	job, err := s.server.MigrateWorld(c.Request.Context(), server.SaveMigrationRequest{
		SourcePath: preview.SourcePath, Mappings: mappings, Confirmation: input.Confirmation,
	}, server.WorldMigrationHooks{
		Prepare: func(ctx context.Context) error {
			client := s.palworldREST()
			if _, err := client.Do(ctx, http.MethodPost, "announce", gin.H{"message": "Player save migration starting. Saving progress and stopping the server."}); err != nil {
				return err
			}
			_, err := client.Do(ctx, http.MethodPost, "save", nil)
			return err
		},
		Invalidate: func() {
			s.invalidateServerCaches()
			s.invalidateSaveCaches()
			s.invalidateSaveIndexes()
		},
	})
	if err != nil {
		fail(c, http.StatusBadRequest, "migration_start_rejected", err.Error())
		return
	}
	s.invalidateServerCaches()
	s.invalidateSaveCaches()
	accepted(c, job)
}

func (s Server) buildSaveMigrationPreview(ctx context.Context, input saveMigrationInput) (saveMigrationPreview, error) {
	source, sourceIndex, sourcePath, err := s.loadMigrationSource(ctx, strings.TrimSpace(input.SourceID))
	if err != nil {
		return saveMigrationPreview{}, err
	}
	if len(input.Mappings) == 0 {
		return saveMigrationPreview{}, migrationError{"migration_mapping_required", "select at least one old player"}
	}
	sourcePlayers := map[string]saveindex.Player{}
	for _, player := range sourceIndex.Players {
		sourcePlayers[strings.ToLower(player.PlayerUID)] = player
	}

	detection, modeSource, serverPlayers := s.detectServerUIDMode(ctx)
	selectedMode := detection.Mode
	requestedMode := strings.ToLower(strings.TrimSpace(input.TargetMode))
	if requestedMode == "" || requestedMode == "auto" {
		requestedMode = string(selectedMode)
	}
	if requestedMode == string(playeruid.ModeSteam) || requestedMode == string(playeruid.ModeNoSteam) {
		selectedMode = playeruid.Mode(requestedMode)
	} else if requestedMode != string(playeruid.ModeUnknown) {
		return saveMigrationPreview{}, migrationError{"migration_mode_invalid", "target_mode must be auto, steam, or nosteam"}
	}
	if detection.Mode != playeruid.ModeUnknown && selectedMode != detection.Mode {
		return saveMigrationPreview{}, migrationError{"migration_mode_conflict", "selected target mode conflicts with the mode proven by the current server save"}
	}

	preview := saveMigrationPreview{
		Source: source, SourceFingerprint: sourceIndex.Snapshot.Fingerprint, SourcePath: sourcePath,
		TargetMode: selectedMode, ModeSource: modeSource, ModeMatched: detection.Matched, ModeTotal: detection.Total,
		RequiresManualConfirmation: detection.Mode == playeruid.ModeUnknown,
		Mappings:                   []saveMigrationMappingPreview{}, Conflicts: []string{},
	}
	seenSources := map[string]bool{}
	seenTargets := map[string]bool{}
	for _, mapping := range input.Mappings {
		sourceUID := strings.ToLower(strings.TrimSpace(mapping.SourceUID))
		if !migrationUIDPattern.MatchString(sourceUID) {
			return saveMigrationPreview{}, migrationError{"migration_source_uid_invalid", "source_uid must be a canonical PlayerUID"}
		}
		player, exists := sourcePlayers[sourceUID]
		if !exists {
			return saveMigrationPreview{}, migrationError{"migration_player_not_found", "selected old player was not found in the source save: " + sourceUID}
		}
		if seenSources[sourceUID] {
			return saveMigrationPreview{}, migrationError{"migration_duplicate_source", "the same old player was selected more than once"}
		}
		seenSources[sourceUID] = true
		pair, err := playeruid.Calculate(mapping.SteamID)
		if err != nil {
			return saveMigrationPreview{}, migrationError{"migration_steam_id_invalid", err.Error()}
		}
		targetUID := ""
		if selectedMode != playeruid.ModeUnknown {
			targetUID, _ = playeruid.UIDForMode(pair, selectedMode)
			if sourceUID == targetUID {
				preview.Conflicts = append(preview.Conflicts, player.Nickname+": target UID is identical to the old UID")
			}
			if seenTargets[targetUID] {
				preview.Conflicts = append(preview.Conflicts, player.Nickname+": target UID is duplicated by another mapping")
			}
			if existing, exists := serverPlayers[targetUID]; exists {
				preview.Conflicts = append(preview.Conflicts, player.Nickname+": target UID already exists in the current server save ("+firstNonEmpty(existing.Nickname, existing.PlayerUID)+")")
			}
			seenTargets[targetUID] = true
		}
		preview.Mappings = append(preview.Mappings, saveMigrationMappingPreview{
			SourceUID: sourceUID, Nickname: player.Nickname, Level: player.Level, SteamID: strings.TrimSpace(mapping.SteamID),
			SteamUID: pair.Steam, NoSteamUID: pair.NoSteam, TargetUID: targetUID,
		})
	}
	preview.Ready = selectedMode != playeruid.ModeUnknown && len(preview.Conflicts) == 0
	return preview, nil
}

func (s Server) loadMigrationSource(ctx context.Context, sourceID string) (db.SaveSource, saveindex.Index, string, error) {
	if sourceID == "" {
		return db.SaveSource{}, saveindex.EmptyIndex(), "", migrationError{"migration_source_required", "source_id is required"}
	}
	source, err := s.store.GetSaveSource(ctx, sourceID)
	if err != nil {
		return db.SaveSource{}, saveindex.EmptyIndex(), "", migrationError{"migration_source_not_found", "save source was not found"}
	}
	cfg := s.cfg
	digest := sha256.Sum256([]byte(source.ID))
	cfg.SaveIndexCacheDir = filepath.Join(s.cfg.SaveIndexCacheDir, "migration-"+hex.EncodeToString(digest[:8]))
	manager := saveindex.NewManager(cfg)
	if source.Kind == "import" {
		if !pathWithin(s.cfg.SaveSourcesDir, source.Path) {
			return db.SaveSource{}, saveindex.EmptyIndex(), "", migrationError{"migration_source_unsafe", "imported save source path is outside the managed save directory"}
		}
		manager.SetSourcePath(source.Path)
	}
	index, _, err := manager.Rebuild(ctx)
	if err != nil {
		return db.SaveSource{}, saveindex.EmptyIndex(), "", migrationError{"migration_source_index_failed", err.Error()}
	}
	sourcePath := source.Path
	if source.Kind == "server" {
		sourcePath = index.SourcePath
	}
	return source, index, sourcePath, nil
}

func (s Server) detectServerUIDMode(ctx context.Context) (playeruid.Detection, string, map[string]saveindex.Player) {
	index, _, err := s.serverSaveIndex.Current(ctx)
	if err != nil || len(index.Players) == 0 {
		index, _, err = s.serverSaveIndex.Rebuild(ctx)
	}
	if err != nil {
		return playeruid.Detection{Mode: playeruid.ModeUnknown}, "unproven", map[string]saveindex.Player{}
	}
	identities := make([]playeruid.Identity, 0, len(index.Players))
	players := make(map[string]saveindex.Player, len(index.Players))
	for _, player := range index.Players {
		playerUID := strings.ToLower(strings.TrimSpace(player.PlayerUID))
		if migrationUIDPattern.MatchString(playerUID) {
			players[playerUID] = player
		}
		if strings.TrimSpace(player.SteamID) == "" || !migrationUIDPattern.MatchString(strings.ToLower(player.PlayerUID)) {
			continue
		}
		identities = append(identities, playeruid.Identity{SteamID: player.SteamID, PlayerUID: player.PlayerUID})
	}
	detection, err := playeruid.DetectMode(identities)
	if err != nil || detection.Mode == playeruid.ModeUnknown {
		return playeruid.Detection{Mode: playeruid.ModeUnknown, Matched: detection.Matched, Total: detection.Total}, "unproven", players
	}
	return detection, "server_index", players
}

type migrationError struct {
	code    string
	message string
}

func (e migrationError) Error() string { return e.message }

func failSaveMigration(c *gin.Context, err error) {
	var migrationErr migrationError
	if errors.As(err, &migrationErr) {
		status := http.StatusBadRequest
		if migrationErr.code == "migration_source_not_found" {
			status = http.StatusNotFound
		}
		fail(c, status, migrationErr.code, migrationErr.message)
		return
	}
	fail(c, http.StatusInternalServerError, "migration_failed", fmt.Sprint(err))
}
