package economy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidCheckinPolicy = errors.New("invalid checkin policy")

const (
	defaultStreakMaxDays      = 7
	maximumStreakDays         = 365
	maximumAliasCount         = 20
	maximumAliasLength        = 32
	maximumCheckinHistoryRows = 365
)

type DetailedConfig struct {
	CommandPrefix            string   `json:"command_prefix"`
	AllowBareCommands        bool     `json:"allow_bare_commands"`
	DailyCheckinPoints       int64    `json:"daily_checkin_points"`
	CheckinStreakEnabled     bool     `json:"checkin_streak_enabled"`
	CheckinStreakBonusPerDay int64    `json:"checkin_streak_bonus_per_day"`
	CheckinStreakMaxDays     int      `json:"checkin_streak_max_days"`
	CheckinCycleDays         int      `json:"checkin_cycle_days"`
	CheckinCycleBonus        int64    `json:"checkin_cycle_bonus"`
	CheckinAliases           []string `json:"checkin_aliases"`
	PointsAliases            []string `json:"points_aliases"`
	HelpAliases              []string `json:"help_aliases"`
	UpdatedAt                string   `json:"updated_at"`
}

type DetailedCheckinResult struct {
	Account       Account      `json:"account"`
	Awarded       bool         `json:"awarded"`
	LocalDate     string       `json:"local_date"`
	Points        int64        `json:"points"`
	BasePoints    int64        `json:"base_points"`
	StreakBonus   int64        `json:"streak_bonus"`
	CycleBonus    int64        `json:"cycle_bonus"`
	StreakDay     int          `json:"streak_day"`
	NextDayPoints int64        `json:"next_day_points"`
	Ledger        *LedgerEntry `json:"ledger,omitempty"`
}

type CheckinHistoryEntry struct {
	LocalDate   string `json:"local_date"`
	Points      int64  `json:"points"`
	BasePoints  int64  `json:"base_points"`
	StreakBonus int64  `json:"streak_bonus"`
	CycleBonus  int64  `json:"cycle_bonus"`
	StreakDay   int    `json:"streak_day"`
	CreatedAt   string `json:"created_at"`
}

func defaultDetailedConfig(base Config) DetailedConfig {
	return DetailedConfig{
		CommandPrefix:            base.CommandPrefix,
		AllowBareCommands:        true,
		DailyCheckinPoints:       base.DailyCheckinPoints,
		CheckinStreakEnabled:     true,
		CheckinStreakBonusPerDay: 2,
		CheckinStreakMaxDays:     defaultStreakMaxDays,
		CheckinCycleDays:         7,
		CheckinCycleBonus:        10,
		CheckinAliases:           []string{"签到", "qd", "checkin"},
		PointsAliases:            []string{"积分", "jf", "points", "point"},
		HelpAliases:              []string{"帮助", "菜单", "help", "menu"},
		UpdatedAt:                base.UpdatedAt,
	}
}

func (s *Service) ensureCheckinPolicySchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS economy_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create economy settings schema: %w", err)
	}
	columns, err := tableColumns(ctx, s.db, "economy_checkins")
	if err != nil {
		return err
	}
	migrations := []struct {
		name string
		sql  string
	}{
		{name: "base_points", sql: `ALTER TABLE economy_checkins ADD COLUMN base_points INTEGER NOT NULL DEFAULT 0`},
		{name: "streak_bonus", sql: `ALTER TABLE economy_checkins ADD COLUMN streak_bonus INTEGER NOT NULL DEFAULT 0`},
		{name: "cycle_bonus", sql: `ALTER TABLE economy_checkins ADD COLUMN cycle_bonus INTEGER NOT NULL DEFAULT 0`},
		{name: "streak_day", sql: `ALTER TABLE economy_checkins ADD COLUMN streak_day INTEGER NOT NULL DEFAULT 1`},
	}
	for _, migration := range migrations {
		if columns[migration.name] {
			continue
		}
		if _, err := s.db.ExecContext(ctx, migration.sql); err != nil {
			return fmt.Errorf("migrate economy_checkins.%s: %w", migration.name, err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE economy_checkins SET base_points=points WHERE base_points=0 AND points<>0`); err != nil {
		return fmt.Errorf("backfill economy checkin base points: %w", err)
	}
	return nil
}

func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		result[strings.ToLower(strings.TrimSpace(name))] = true
	}
	return result, rows.Err()
}

func (s *Service) DetailedConfig(ctx context.Context) (DetailedConfig, error) {
	if err := s.ensureCheckinPolicySchema(ctx); err != nil {
		return DetailedConfig{}, err
	}
	base, err := s.Config(ctx)
	if err != nil {
		return DetailedConfig{}, err
	}
	config := defaultDetailedConfig(base)
	rows, err := s.db.QueryContext(ctx, `SELECT key,value,updated_at FROM economy_settings WHERE key IN (
		'checkin_streak_enabled','checkin_streak_bonus_per_day','checkin_streak_max_days',
		'allow_bare_commands','checkin_cycle_days','checkin_cycle_bonus','checkin_aliases','points_aliases','help_aliases'
	)`)
	if err != nil {
		return DetailedConfig{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value, updatedAt string
		if err := rows.Scan(&key, &value, &updatedAt); err != nil {
			return DetailedConfig{}, err
		}
		if updatedAt > config.UpdatedAt {
			config.UpdatedAt = updatedAt
		}
		switch key {
		case "allow_bare_commands":
			config.AllowBareCommands = value == "1" || strings.EqualFold(value, "true")
		case "checkin_streak_enabled":
			config.CheckinStreakEnabled = value == "1" || strings.EqualFold(value, "true")
		case "checkin_streak_bonus_per_day":
			config.CheckinStreakBonusPerDay = parseInt64(value, config.CheckinStreakBonusPerDay)
		case "checkin_streak_max_days":
			config.CheckinStreakMaxDays = int(parseInt64(value, int64(config.CheckinStreakMaxDays)))
		case "checkin_cycle_days":
			config.CheckinCycleDays = int(parseInt64(value, int64(config.CheckinCycleDays)))
		case "checkin_cycle_bonus":
			config.CheckinCycleBonus = parseInt64(value, config.CheckinCycleBonus)
		case "checkin_aliases":
			config.CheckinAliases = decodeAliases(value, config.CheckinAliases)
		case "points_aliases":
			config.PointsAliases = decodeAliases(value, config.PointsAliases)
		case "help_aliases":
			config.HelpAliases = decodeAliases(value, config.HelpAliases)
		}
	}
	if err := rows.Err(); err != nil {
		return DetailedConfig{}, err
	}
	if err := validateDetailedConfig(config); err != nil {
		return DetailedConfig{}, err
	}
	return config, nil
}

func (s *Service) UpdateDetailedConfig(ctx context.Context, input DetailedConfig) (DetailedConfig, error) {
	if err := validateDetailedConfig(input); err != nil {
		return DetailedConfig{}, err
	}
	if err := s.ensureCheckinPolicySchema(ctx); err != nil {
		return DetailedConfig{}, err
	}
	if _, err := s.UpdateConfig(ctx, input.CommandPrefix, input.DailyCheckinPoints); err != nil {
		return DetailedConfig{}, err
	}
	aliases := []struct {
		key   string
		value []string
	}{
		{key: "checkin_aliases", value: input.CheckinAliases},
		{key: "points_aliases", value: input.PointsAliases},
		{key: "help_aliases", value: input.HelpAliases},
	}
	values := []struct{ key, value string }{
		{key: "allow_bare_commands", value: strconv.FormatBool(input.AllowBareCommands)},
		{key: "checkin_streak_enabled", value: strconv.FormatBool(input.CheckinStreakEnabled)},
		{key: "checkin_streak_bonus_per_day", value: strconv.FormatInt(input.CheckinStreakBonusPerDay, 10)},
		{key: "checkin_streak_max_days", value: strconv.Itoa(input.CheckinStreakMaxDays)},
		{key: "checkin_cycle_days", value: strconv.Itoa(input.CheckinCycleDays)},
		{key: "checkin_cycle_bonus", value: strconv.FormatInt(input.CheckinCycleBonus, 10)},
	}
	for _, item := range aliases {
		encoded, err := json.Marshal(normalizeAliases(item.value))
		if err != nil {
			return DetailedConfig{}, err
		}
		values = append(values, struct{ key, value string }{key: item.key, value: string(encoded)})
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DetailedConfig{}, err
	}
	defer rollback(tx)
	now := s.timestamp()
	for _, item := range values {
		if _, err := tx.ExecContext(ctx, `INSERT INTO economy_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, item.key, item.value, now); err != nil {
			return DetailedConfig{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return DetailedConfig{}, err
	}
	return s.DetailedConfig(ctx)
}

func validateDetailedConfig(config DetailedConfig) error {
	if err := validateCommandPrefix(config.CommandPrefix); err != nil {
		return err
	}
	if config.DailyCheckinPoints < 0 || config.DailyCheckinPoints > maximumCheckinPoints ||
		config.CheckinStreakBonusPerDay < 0 || config.CheckinStreakBonusPerDay > maximumCheckinPoints ||
		config.CheckinCycleBonus < 0 || config.CheckinCycleBonus > maximumCheckinPoints {
		return ErrInvalidCheckinPoints
	}
	if config.CheckinStreakMaxDays < 1 || config.CheckinStreakMaxDays > maximumStreakDays ||
		config.CheckinCycleDays < 0 || config.CheckinCycleDays > maximumStreakDays {
		return fmt.Errorf("%w: streak days must be between 1 and 365; cycle days may be 0 to disable", ErrInvalidCheckinPolicy)
	}
	if err := validateAliases(config.CheckinAliases); err != nil {
		return fmt.Errorf("%w: checkin aliases: %v", ErrInvalidCheckinPolicy, err)
	}
	if err := validateAliases(config.PointsAliases); err != nil {
		return fmt.Errorf("%w: points aliases: %v", ErrInvalidCheckinPolicy, err)
	}
	if err := validateAliases(config.HelpAliases); err != nil {
		return fmt.Errorf("%w: help aliases: %v", ErrInvalidCheckinPolicy, err)
	}
	return nil
}

func validateAliases(values []string) error {
	values = normalizeAliases(values)
	if len(values) == 0 || len(values) > maximumAliasCount {
		return fmt.Errorf("must contain between 1 and %d aliases", maximumAliasCount)
	}
	for _, value := range values {
		if len([]rune(value)) > maximumAliasLength || strings.ContainsAny(value, "\r\n\t ") {
			return fmt.Errorf("alias %q is invalid", value)
		}
	}
	return nil
}

func normalizeAliases(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}

func decodeAliases(value string, fallback []string) []string {
	var result []string
	if json.Unmarshal([]byte(value), &result) != nil {
		return append([]string(nil), fallback...)
	}
	result = normalizeAliases(result)
	if len(result) == 0 {
		return append([]string(nil), fallback...)
	}
	return result
}

func parseInt64(value string, fallback int64) int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func (s *Service) CheckinWithPolicy(ctx context.Context, playerUID, nickname, steamID, localDate string, basePoints *int64, actor string) (DetailedCheckinResult, error) {
	playerUID = normalizePlayerUID(playerUID)
	if playerUID == "" {
		return DetailedCheckinResult{}, ErrInvalidPlayerUID
	}
	config, err := s.DetailedConfig(ctx)
	if err != nil {
		return DetailedCheckinResult{}, err
	}
	points := config.DailyCheckinPoints
	if basePoints != nil {
		points = *basePoints
	}
	if points < 0 || points > maximumCheckinPoints {
		return DetailedCheckinResult{}, ErrInvalidCheckinPoints
	}
	if strings.TrimSpace(localDate) == "" {
		localDate = s.LocalDate()
	}
	if _, err := time.Parse("2006-01-02", localDate); err != nil {
		return DetailedCheckinResult{}, errors.New("local_date must use YYYY-MM-DD")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DetailedCheckinResult{}, err
	}
	defer rollback(tx)
	if err := s.ensureAccountTx(ctx, tx, playerUID, nickname, steamID); err != nil {
		return DetailedCheckinResult{}, err
	}
	result, err := s.checkinWithPolicyTx(ctx, tx, playerUID, localDate, points, actor, config)
	if err != nil {
		return DetailedCheckinResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return DetailedCheckinResult{}, err
	}
	return result, nil
}

func (s *Service) checkinWithPolicyTx(ctx context.Context, tx *sql.Tx, playerUID, localDate string, basePoints int64, actor string, config DetailedConfig) (DetailedCheckinResult, error) {
	if existing, found, err := checkinRecordTx(ctx, tx, playerUID, localDate); err != nil {
		return DetailedCheckinResult{}, err
	} else if found {
		account, err := accountTx(ctx, tx, playerUID)
		if err != nil {
			return DetailedCheckinResult{}, err
		}
		existing.Account = account
		existing.NextDayPoints = calculateCheckinPoints(config, existing.StreakDay+1, basePoints)
		return existing, nil
	}
	streakDay := 1
	var previousDate string
	var previousStreak int
	err := tx.QueryRowContext(ctx, `SELECT local_date,streak_day FROM economy_checkins WHERE player_uid=? AND local_date<? ORDER BY local_date DESC LIMIT 1`, playerUID, localDate).Scan(&previousDate, &previousStreak)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return DetailedCheckinResult{}, err
	}
	if err == nil {
		currentDate, _ := time.Parse("2006-01-02", localDate)
		previous, parseErr := time.Parse("2006-01-02", previousDate)
		if parseErr == nil && previous.AddDate(0, 0, 1).Equal(currentDate) {
			streakDay = previousStreak + 1
		}
	}
	streakBonus, cycleBonus := calculateCheckinBonus(config, streakDay)
	total := basePoints + streakBonus + cycleBonus
	now := s.timestamp()
	if _, err := tx.ExecContext(ctx, `INSERT INTO economy_checkins(player_uid,local_date,points,created_at,base_points,streak_bonus,cycle_bonus,streak_day) VALUES(?,?,?,?,?,?,?,?)`, playerUID, localDate, total, now, basePoints, streakBonus, cycleBonus, streakDay); err != nil {
		return DetailedCheckinResult{}, err
	}
	result := DetailedCheckinResult{
		Awarded:       true,
		LocalDate:     localDate,
		Points:        total,
		BasePoints:    basePoints,
		StreakBonus:   streakBonus,
		CycleBonus:    cycleBonus,
		StreakDay:     streakDay,
		NextDayPoints: calculateCheckinPoints(config, streakDay+1, basePoints),
	}
	if total > 0 {
		entry, account, err := s.adjustTx(ctx, tx, Adjustment{
			PlayerUID:     playerUID,
			Delta:         total,
			Reason:        "daily_checkin",
			ReferenceType: "daily_checkin",
			ReferenceID:   localDate,
			Actor:         actor,
			Metadata: map[string]any{
				"base_points": basePoints, "streak_bonus": streakBonus, "cycle_bonus": cycleBonus, "streak_day": streakDay,
			},
		})
		if err != nil {
			return DetailedCheckinResult{}, err
		}
		result.Account = account
		result.Ledger = &entry
		return result, nil
	}
	account, err := accountTx(ctx, tx, playerUID)
	if err != nil {
		return DetailedCheckinResult{}, err
	}
	result.Account = account
	return result, nil
}

func calculateCheckinBonus(config DetailedConfig, streakDay int) (int64, int64) {
	if !config.CheckinStreakEnabled || streakDay < 1 {
		return 0, 0
	}
	bonusDay := streakDay
	if config.CheckinStreakMaxDays > 0 && bonusDay > config.CheckinStreakMaxDays {
		bonusDay = config.CheckinStreakMaxDays
	}
	streakBonus := int64(maxInt(bonusDay-1, 0)) * config.CheckinStreakBonusPerDay
	cycleBonus := int64(0)
	if config.CheckinCycleDays > 0 && streakDay%config.CheckinCycleDays == 0 {
		cycleBonus = config.CheckinCycleBonus
	}
	return streakBonus, cycleBonus
}

func calculateCheckinPoints(config DetailedConfig, streakDay int, basePoints int64) int64 {
	streakBonus, cycleBonus := calculateCheckinBonus(config, streakDay)
	return basePoints + streakBonus + cycleBonus
}

func checkinRecordTx(ctx context.Context, tx *sql.Tx, playerUID, localDate string) (DetailedCheckinResult, bool, error) {
	var result DetailedCheckinResult
	err := tx.QueryRowContext(ctx, `SELECT local_date,points,base_points,streak_bonus,cycle_bonus,streak_day FROM economy_checkins WHERE player_uid=? AND local_date=?`, playerUID, localDate).Scan(
		&result.LocalDate, &result.Points, &result.BasePoints, &result.StreakBonus, &result.CycleBonus, &result.StreakDay,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return DetailedCheckinResult{}, false, nil
	}
	if err != nil {
		return DetailedCheckinResult{}, false, err
	}
	return result, true, nil
}

func (s *Service) CheckinHistory(ctx context.Context, playerUID string, limit int) ([]CheckinHistoryEntry, error) {
	playerUID = normalizePlayerUID(playerUID)
	if playerUID == "" {
		return nil, ErrInvalidPlayerUID
	}
	if err := s.ensureCheckinPolicySchema(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > maximumCheckinHistoryRows {
		limit = maximumCheckinHistoryRows
	}
	rows, err := s.db.QueryContext(ctx, `SELECT local_date,points,base_points,streak_bonus,cycle_bonus,streak_day,created_at FROM economy_checkins WHERE player_uid=? ORDER BY local_date DESC LIMIT ?`, playerUID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CheckinHistoryEntry, 0)
	for rows.Next() {
		var item CheckinHistoryEntry
		if err := rows.Scan(&item.LocalDate, &item.Points, &item.BasePoints, &item.StreakBonus, &item.CycleBonus, &item.StreakDay, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ExecuteCommandDetailed(ctx context.Context, request CommandRequest) (CommandResult, error) {
	request.EventID = strings.TrimSpace(request.EventID)
	request.PlayerUID = normalizePlayerUID(request.PlayerUID)
	request.Message = strings.TrimSpace(request.Message)
	if request.PlayerUID == "" {
		return CommandResult{}, ErrInvalidPlayerUID
	}
	if request.EventID == "" {
		request.EventID = newID()
	}
	config, err := s.DetailedConfig(ctx)
	if err != nil {
		return CommandResult{}, err
	}
	command, handled := parseDetailedCommand(request.Message, config)
	if !handled {
		return CommandResult{EventID: request.EventID, PlayerUID: request.PlayerUID, Handled: false}, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CommandResult{}, err
	}
	defer rollback(tx)
	if previous, found, err := commandEventTx(ctx, tx, request.EventID); err != nil {
		return CommandResult{}, err
	} else if found {
		previous.Duplicate = true
		if err := tx.Commit(); err != nil {
			return CommandResult{}, err
		}
		return previous, nil
	}
	if err := s.ensureAccountTx(ctx, tx, request.PlayerUID, request.Nickname, request.SteamID); err != nil {
		return CommandResult{}, err
	}
	result := CommandResult{EventID: request.EventID, PlayerUID: request.PlayerUID, Handled: true, Command: command}
	switch command {
	case "checkin":
		basePoints := config.DailyCheckinPoints
		if request.Points > 0 {
			basePoints = request.Points
		}
		localDate := strings.TrimSpace(request.LocalDate)
		if localDate == "" {
			localDate = s.LocalDate()
		}
		checkin, err := s.checkinWithPolicyTx(ctx, tx, request.PlayerUID, localDate, basePoints, "game-command", config)
		if err != nil {
			return CommandResult{}, err
		}
		result.Awarded = checkin.Awarded
		result.Balance = checkin.Account.Balance
		result.LocalDate = localDate
		if checkin.Awarded {
			bonusText := ""
			if checkin.StreakBonus > 0 || checkin.CycleBonus > 0 {
				bonusText = fmt.Sprintf("（基础%d，连续奖励%d，周期奖励%d）", checkin.BasePoints, checkin.StreakBonus, checkin.CycleBonus)
			}
			result.Reply = fmt.Sprintf("签到成功，获得%d积分%s。连续签到%d天，当前积分%d。", checkin.Points, bonusText, checkin.StreakDay, checkin.Account.Balance)
		} else {
			result.Reply = fmt.Sprintf("今天已经签到过了。连续签到%d天，当前积分%d。", checkin.StreakDay, checkin.Account.Balance)
		}
	case "points":
		account, err := accountTx(ctx, tx, request.PlayerUID)
		if err != nil {
			return CommandResult{}, err
		}
		result.Balance = account.Balance
		result.Reply = fmt.Sprintf("当前积分：%d。", account.Balance)
	case "help":
		account, err := accountTx(ctx, tx, request.PlayerUID)
		if err != nil {
			return CommandResult{}, err
		}
		result.Balance = account.Balance
		result.Reply = fmt.Sprintf("可用命令：%s、%s、%s。", commandLabel(config.CommandPrefix, config.CheckinAliases[0]), commandLabel(config.CommandPrefix, config.PointsAliases[0]), commandLabel(config.CommandPrefix, config.HelpAliases[0]))
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return CommandResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO economy_command_events(event_id,player_uid,command,response_json,created_at) VALUES(?,?,?,?,?)`, request.EventID, request.PlayerUID, command, string(payload), s.timestamp()); err != nil {
		return CommandResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return CommandResult{}, err
	}
	return result, nil
}

func parseDetailedCommand(message string, config DetailedConfig) (string, bool) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", false
	}
	if config.CommandPrefix != "" {
		if strings.HasPrefix(message, config.CommandPrefix) {
			message = strings.TrimSpace(strings.TrimPrefix(message, config.CommandPrefix))
		} else if !config.AllowBareCommands {
			return "", false
		}
	}
	fields := strings.Fields(message)
	if len(fields) == 0 {
		return "", false
	}
	command := strings.ToLower(fields[0])
	for _, candidate := range config.CheckinAliases {
		if strings.EqualFold(command, candidate) {
			return "checkin", true
		}
	}
	for _, candidate := range config.PointsAliases {
		if strings.EqualFold(command, candidate) {
			return "points", true
		}
	}
	for _, candidate := range config.HelpAliases {
		if strings.EqualFold(command, candidate) {
			return "help", true
		}
	}
	return "", false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
