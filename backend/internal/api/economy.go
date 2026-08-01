package api

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"palpanel/internal/economy"
)

func init() {
	patchFeatures = append(patchFeatures,
		"economy-ledger-v1",
		"economy-checkin-api",
		"economy-reservations",
		"economy-game-command-api",
		"astrbot-economy-api",
		"configurable-game-command-prefix",
		"configurable-checkin-streaks",
		"configurable-command-aliases",
		"astrbot-economy-sqlite-import",
	)
}

func (s Server) registerEconomyRoutes(api *gin.RouterGroup) {
	group := api.Group("/economy")
	group.GET("/config", Require(PermRead), s.economyConfig)
	group.PUT("/config", Require(PermConfigWrite), s.putEconomyConfig)
	group.GET("/summary", Require(PermRead), s.economySummary)
	group.GET("/accounts", Require(PermRead), s.economyAccounts)
	group.GET("/accounts/:player_uid", Require(PermRead), s.economyAccount)
	group.GET("/accounts/:player_uid/ledger", Require(PermRead), s.economyLedger)
	group.GET("/accounts/:player_uid/checkins", Require(PermRead), s.economyCheckinHistory)
	group.POST("/accounts/:player_uid/adjust", Require(PermPlayersWrite), s.economyAdjust)
	group.POST("/accounts/:player_uid/checkin", Require(PermPlayersWrite), s.economyCheckin)
	group.POST("/reservations", Require(PermPlayersWrite), s.economyReserve)
	group.POST("/reservations/:id/commit", Require(PermPlayersWrite), s.economyCommitReservation)
	group.POST("/reservations/:id/release", Require(PermPlayersWrite), s.economyReleaseReservation)
	group.POST("/commands/execute", Require(PermPlayersWrite), s.economyExecuteCommand)
	group.POST("/imports/astrbot/inspect", RequireInteractiveAdmin(), s.economyInspectAstrBot)
	group.POST("/imports/astrbot", RequireInteractiveAdmin(), s.economyImportAstrBot)
	group.POST("/maintenance/release-expired", Require(PermPlayersWrite), s.economyReleaseExpired)
}

func (s Server) economyService() (*economy.Service, error) {
	timezone := strings.TrimSpace(os.Getenv("PALPANEL_OPERATIONS_TIMEZONE"))
	return economy.ForPath(s.cfg.DBPath, timezone, economy.Defaults{
		CommandPrefix:      defaultGameCommandPrefix(),
		DailyCheckinPoints: defaultDailyCheckinPoints(),
	})
}

const maximumAstrBotEconomyDatabaseBytes int64 = 64 << 20

func (s Server) economyInspectAstrBot(c *gin.Context) {
	path, cleanup, err := receiveAstrBotEconomyDatabase(c)
	if err != nil {
		fail(c, http.StatusBadRequest, "astrbot_database_upload_failed", err.Error())
		return
	}
	defer cleanup()
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	preview, err := service.InspectLegacyAstrBot(c.Request.Context(), path)
	if err != nil {
		economyFailure(c, err)
		return
	}
	ok(c, preview)
}

func (s Server) economyImportAstrBot(c *gin.Context) {
	path, cleanup, err := receiveAstrBotEconomyDatabase(c)
	if err != nil {
		fail(c, http.StatusBadRequest, "astrbot_database_upload_failed", err.Error())
		return
	}
	defer cleanup()
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	result, err := service.ImportLegacyAstrBot(c.Request.Context(), path, CurrentPrincipal(c).Name)
	if err != nil {
		economyFailure(c, err)
		return
	}
	created(c, result)
}

func receiveAstrBotEconomyDatabase(c *gin.Context) (string, func(), error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maximumAstrBotEconomyDatabaseBytes+(1<<20))
	upload, header, err := c.Request.FormFile("database")
	if err != nil {
		return "", func() {}, errors.New("multipart field database is required")
	}
	defer upload.Close()
	if header.Size <= 0 {
		return "", func() {}, errors.New("uploaded database is empty")
	}
	if header.Size > maximumAstrBotEconomyDatabaseBytes {
		return "", func() {}, fmt.Errorf("uploaded database exceeds %d MiB", maximumAstrBotEconomyDatabaseBytes>>20)
	}
	temporary, err := os.CreateTemp("", "palpanel-astrbot-economy-*.sqlite3")
	if err != nil {
		return "", func() {}, err
	}
	path := temporary.Name()
	cleanup := func() { _ = os.Remove(path) }
	_ = temporary.Chmod(0o600)
	written, copyErr := io.CopyN(temporary, upload, maximumAstrBotEconomyDatabaseBytes+1)
	closeErr := temporary.Close()
	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		cleanup()
		return "", func() {}, copyErr
	}
	if closeErr != nil {
		cleanup()
		return "", func() {}, closeErr
	}
	if written > maximumAstrBotEconomyDatabaseBytes {
		cleanup()
		return "", func() {}, fmt.Errorf("uploaded database exceeds %d MiB", maximumAstrBotEconomyDatabaseBytes>>20)
	}
	return path, cleanup, nil
}

func (s Server) economyConfig(c *gin.Context) {
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	config, err := service.DetailedConfig(c.Request.Context())
	if err != nil {
		economyFailure(c, err)
		return
	}
	ok(c, config)
}

func (s Server) putEconomyConfig(c *gin.Context) {
	var request struct {
		CommandPrefix            *string   `json:"command_prefix"`
		AllowBareCommands        *bool     `json:"allow_bare_commands"`
		DailyCheckinPoints       *int64    `json:"daily_checkin_points"`
		CheckinStreakEnabled     *bool     `json:"checkin_streak_enabled"`
		CheckinStreakBonusPerDay *int64    `json:"checkin_streak_bonus_per_day"`
		CheckinStreakMaxDays     *int      `json:"checkin_streak_max_days"`
		CheckinCycleDays         *int      `json:"checkin_cycle_days"`
		CheckinCycleBonus        *int64    `json:"checkin_cycle_bonus"`
		CheckinAliases           *[]string `json:"checkin_aliases"`
		PointsAliases            *[]string `json:"points_aliases"`
		HelpAliases              *[]string `json:"help_aliases"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	current, err := service.DetailedConfig(c.Request.Context())
	if err != nil {
		economyFailure(c, err)
		return
	}
	if request.CommandPrefix != nil {
		current.CommandPrefix = *request.CommandPrefix
	}
	if request.AllowBareCommands != nil {
		current.AllowBareCommands = *request.AllowBareCommands
	}
	if request.DailyCheckinPoints != nil {
		current.DailyCheckinPoints = *request.DailyCheckinPoints
	}
	if request.CheckinStreakEnabled != nil {
		current.CheckinStreakEnabled = *request.CheckinStreakEnabled
	}
	if request.CheckinStreakBonusPerDay != nil {
		current.CheckinStreakBonusPerDay = *request.CheckinStreakBonusPerDay
	}
	if request.CheckinStreakMaxDays != nil {
		current.CheckinStreakMaxDays = *request.CheckinStreakMaxDays
	}
	if request.CheckinCycleDays != nil {
		current.CheckinCycleDays = *request.CheckinCycleDays
	}
	if request.CheckinCycleBonus != nil {
		current.CheckinCycleBonus = *request.CheckinCycleBonus
	}
	if request.CheckinAliases != nil {
		current.CheckinAliases = *request.CheckinAliases
	}
	if request.PointsAliases != nil {
		current.PointsAliases = *request.PointsAliases
	}
	if request.HelpAliases != nil {
		current.HelpAliases = *request.HelpAliases
	}
	updated, err := service.UpdateDetailedConfig(c.Request.Context(), current)
	if err != nil {
		economyFailure(c, err)
		return
	}
	ok(c, updated)
}

func (s Server) economySummary(c *gin.Context) {
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	summary, err := service.Summary(c.Request.Context())
	if err != nil {
		economyFailure(c, err)
		return
	}
	ok(c, summary)
}

func (s Server) economyAccounts(c *gin.Context) {
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	accounts, err := service.ListAccounts(
		c.Request.Context(),
		c.Query("query"),
		economyQueryInt(c, "limit", 50),
		economyQueryInt(c, "offset", 0),
	)
	if err != nil {
		economyFailure(c, err)
		return
	}
	ok(c, gin.H{"items": accounts, "count": len(accounts)})
}

func (s Server) economyAccount(c *gin.Context) {
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	account, err := service.GetAccount(c.Request.Context(), c.Param("player_uid"))
	if errors.Is(err, sql.ErrNoRows) {
		fail(c, http.StatusNotFound, "economy_account_not_found", "point account not found")
		return
	}
	if err != nil {
		economyFailure(c, err)
		return
	}
	ok(c, account)
}

func (s Server) economyLedger(c *gin.Context) {
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	entries, err := service.ListLedger(
		c.Request.Context(),
		c.Param("player_uid"),
		economyQueryInt(c, "limit", 50),
		economyQueryInt(c, "offset", 0),
	)
	if err != nil {
		economyFailure(c, err)
		return
	}
	ok(c, gin.H{"items": entries, "count": len(entries)})
}

func (s Server) economyAdjust(c *gin.Context) {
	var request struct {
		Nickname      string         `json:"nickname"`
		SteamID       string         `json:"steam_id"`
		Delta         int64          `json:"delta"`
		Reason        string         `json:"reason"`
		ReferenceType string         `json:"reference_type"`
		ReferenceID   string         `json:"reference_id"`
		Metadata      map[string]any `json:"metadata"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	result, err := service.Adjust(c.Request.Context(), economy.Adjustment{
		PlayerUID:     c.Param("player_uid"),
		Nickname:      request.Nickname,
		SteamID:       request.SteamID,
		Delta:         request.Delta,
		Reason:        request.Reason,
		ReferenceType: request.ReferenceType,
		ReferenceID:   request.ReferenceID,
		Actor:         CurrentPrincipal(c).Name,
		Metadata:      request.Metadata,
	})
	if err != nil {
		economyFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) economyCheckin(c *gin.Context) {
	var request struct {
		Nickname  string `json:"nickname"`
		SteamID   string `json:"steam_id"`
		LocalDate string `json:"local_date"`
		Points    *int64 `json:"points"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	result, err := service.CheckinWithPolicy(
		c.Request.Context(), c.Param("player_uid"), request.Nickname, request.SteamID,
		request.LocalDate, request.Points, CurrentPrincipal(c).Name,
	)
	if err != nil {
		economyFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) economyCheckinHistory(c *gin.Context) {
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	items, err := service.CheckinHistory(c.Request.Context(), c.Param("player_uid"), economyQueryInt(c, "limit", 30))
	if err != nil {
		economyFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func (s Server) economyReserve(c *gin.Context) {
	var request struct {
		PlayerUID   string `json:"player_uid"`
		Nickname    string `json:"nickname"`
		SteamID     string `json:"steam_id"`
		ReferenceID string `json:"reference_id"`
		Amount      int64  `json:"amount"`
		TTLSeconds  int64  `json:"ttl_seconds"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	ttl := time.Duration(request.TTLSeconds) * time.Second
	result, err := service.Reserve(
		c.Request.Context(), request.PlayerUID, request.Nickname, request.SteamID,
		request.ReferenceID, request.Amount, ttl, CurrentPrincipal(c).Name,
	)
	if err != nil {
		economyFailure(c, err)
		return
	}
	created(c, result)
}

func (s Server) economyCommitReservation(c *gin.Context) {
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	result, err := service.CommitReservation(c.Request.Context(), c.Param("id"), CurrentPrincipal(c).Name)
	if err != nil {
		economyFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) economyReleaseReservation(c *gin.Context) {
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	result, err := service.ReleaseReservation(c.Request.Context(), c.Param("id"), CurrentPrincipal(c).Name)
	if err != nil {
		economyFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) economyExecuteCommand(c *gin.Context) {
	var request economy.CommandRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	result, err := service.ExecuteCommandDetailed(c.Request.Context(), request)
	if err != nil {
		economyFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) economyReleaseExpired(c *gin.Context) {
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	count, err := service.ReleaseExpired(c.Request.Context())
	if err != nil {
		economyFailure(c, err)
		return
	}
	ok(c, gin.H{"released": count})
}

func (s Server) astrBotEconomyCheckin(c *gin.Context) {
	var request struct {
		PlayerUID string `json:"player_uid"`
		Nickname  string `json:"nickname"`
		SteamID   string `json:"steam_id"`
		LocalDate string `json:"local_date"`
		Points    *int64 `json:"points"`
		QQID      string `json:"qq_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	result, err := service.CheckinWithPolicy(
		c.Request.Context(), request.PlayerUID, request.Nickname, request.SteamID,
		request.LocalDate, request.Points, "astrbot:"+strings.TrimSpace(request.QQID),
	)
	if err != nil {
		economyFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) astrBotEconomyBalance(c *gin.Context) {
	var request struct {
		PlayerUID string `json:"player_uid"`
		Nickname  string `json:"nickname"`
		SteamID   string `json:"steam_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	account, err := service.EnsureAccount(c.Request.Context(), request.PlayerUID, request.Nickname, request.SteamID)
	if err != nil {
		economyFailure(c, err)
		return
	}
	ok(c, account)
}

func defaultGameCommandPrefix() string {
	if value, found := os.LookupEnv("PALPANEL_GAME_COMMAND_PREFIX"); found {
		return value
	}
	return "!"
}

func defaultDailyCheckinPoints() int64 {
	value := strings.TrimSpace(os.Getenv("PALPANEL_DAILY_CHECKIN_POINTS"))
	if value == "" {
		return 10
	}
	points, err := strconv.ParseInt(value, 10, 64)
	if err != nil || points < 0 || points > 1000000 {
		return 10
	}
	return points
}

func economyQueryInt(c *gin.Context, name string, fallback int) int {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func economyFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, economy.ErrInvalidPlayerUID),
		errors.Is(err, economy.ErrInvalidLegacyAstrBotDatabase),
		errors.Is(err, economy.ErrInvalidCommandPrefix),
		errors.Is(err, economy.ErrInvalidCheckinPoints),
		errors.Is(err, economy.ErrInvalidAmount),
		errors.Is(err, economy.ErrInvalidDelta),
		errors.Is(err, economy.ErrInvalidReason),
		errors.Is(err, economy.ErrInvalidReference):
		fail(c, http.StatusBadRequest, "economy_invalid_request", err.Error())
	case errors.Is(err, economy.ErrInsufficientBalance):
		fail(c, http.StatusConflict, "economy_insufficient_balance", err.Error())
	case errors.Is(err, economy.ErrReservationNotFound), errors.Is(err, sql.ErrNoRows):
		fail(c, http.StatusNotFound, "economy_not_found", err.Error())
	case errors.Is(err, economy.ErrReservationSettled):
		fail(c, http.StatusConflict, "economy_reservation_settled", err.Error())
	default:
		fail(c, http.StatusInternalServerError, "economy_operation_failed", err.Error())
	}
}
