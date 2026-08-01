package boss

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrInvalidReward         = errors.New("boss reward is invalid")
	ErrRewardNotFound        = errors.New("boss reward not found")
	ErrInvalidTemplate       = errors.New("boss template is invalid")
	ErrTemplateNotFound      = errors.New("boss template not found")
	ErrTemplateDisabled      = errors.New("boss template is disabled")
	ErrInvalidWave           = errors.New("boss wave is invalid")
	ErrWaveNotFound          = errors.New("boss wave not found")
	ErrInvalidSummon         = errors.New("boss summon request is invalid")
	ErrSummonNotFound        = errors.New("boss summon not found")
	ErrInvalidTransition     = errors.New("boss summon transition is invalid")
	ErrInvalidWaveTransition = errors.New("boss wave transition is invalid")

	identifierPattern   = regexp.MustCompile(`^[A-Za-z0-9_:-]{1,128}$`)
	templateNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
	serviceCache        sync.Map
)

const (
	defaultLimit = 50
	maximumLimit = 500
	maximumJSON  = 64 << 10

	SummonStatusPending   = "pending"
	SummonStatusActive    = "active"
	SummonStatusCompleted = "completed"
	SummonStatusFailed    = "failed"
	SummonStatusCancelled = "cancelled"

	ExecutionModeRecordOnly = "record_only"

	WaveKindMain          = "main"
	WaveKindMinion        = "minion"
	WaveKindReinforcement = "reinforcement"

	WaveStatusPending   = "pending"
	WaveStatusActive    = "active"
	WaveStatusCompleted = "completed"
	WaveStatusFailed    = "failed"
	WaveStatusSkipped   = "skipped"

	maximumWaves = 20
)

type Service struct {
	db  *sql.DB
	now func() time.Time
}

type Location struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Z     float64 `json:"z"`
	Label string  `json:"label,omitempty"`
}

type RewardItem struct {
	ItemID string `json:"item_id"`
	Count  int64  `json:"count"`
}

type RewardInput struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Points       int64          `json:"points"`
	Items        []RewardItem   `json:"items"`
	PalTemplates []string       `json:"pal_templates"`
	Enabled      bool           `json:"enabled"`
	Metadata     map[string]any `json:"metadata"`
}

type Reward struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	Points       int64          `json:"points"`
	Items        []RewardItem   `json:"items,omitempty"`
	PalTemplates []string       `json:"pal_templates,omitempty"`
	Enabled      bool           `json:"enabled"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
	ArchivedAt   string         `json:"archived_at,omitempty"`
}

type TemplateInput struct {
	Name              string         `json:"name"`
	Description       string         `json:"description"`
	PalID             string         `json:"pal_id"`
	Level             int            `json:"level"`
	Count             int            `json:"count"`
	HPMultiplier      float64        `json:"hp_multiplier"`
	AttackMultiplier  float64        `json:"attack_multiplier"`
	DefenseMultiplier float64        `json:"defense_multiplier"`
	SpawnRadius       float64        `json:"spawn_radius"`
	Capturable        bool           `json:"capturable"`
	CooldownSeconds   int            `json:"cooldown_seconds"`
	RewardID          string         `json:"reward_id"`
	Location          Location       `json:"location"`
	Enabled           bool           `json:"enabled"`
	Metadata          map[string]any `json:"metadata"`
}

type Template struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	Description       string         `json:"description,omitempty"`
	PalID             string         `json:"pal_id"`
	Level             int            `json:"level"`
	Count             int            `json:"count"`
	HPMultiplier      float64        `json:"hp_multiplier"`
	AttackMultiplier  float64        `json:"attack_multiplier"`
	DefenseMultiplier float64        `json:"defense_multiplier"`
	SpawnRadius       float64        `json:"spawn_radius"`
	Capturable        bool           `json:"capturable"`
	CooldownSeconds   int            `json:"cooldown_seconds"`
	RewardID          string         `json:"reward_id,omitempty"`
	Location          Location       `json:"location"`
	Enabled           bool           `json:"enabled"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
	ArchivedAt        string         `json:"archived_at,omitempty"`
}

type WaveInput struct {
	Name              string         `json:"name"`
	Kind              string         `json:"kind"`
	PalID             string         `json:"pal_id"`
	Level             int            `json:"level"`
	Count             int            `json:"count"`
	HPMultiplier      float64        `json:"hp_multiplier"`
	AttackMultiplier  float64        `json:"attack_multiplier"`
	DefenseMultiplier float64        `json:"defense_multiplier"`
	SpawnRadius       float64        `json:"spawn_radius"`
	DelaySeconds      int            `json:"delay_seconds"`
	Capturable        bool           `json:"capturable"`
	Metadata          map[string]any `json:"metadata"`
}

type Wave struct {
	ID                string         `json:"id"`
	TemplateID        string         `json:"template_id"`
	Position          int            `json:"position"`
	Name              string         `json:"name"`
	Kind              string         `json:"kind"`
	PalID             string         `json:"pal_id"`
	Level             int            `json:"level"`
	Count             int            `json:"count"`
	HPMultiplier      float64        `json:"hp_multiplier"`
	AttackMultiplier  float64        `json:"attack_multiplier"`
	DefenseMultiplier float64        `json:"defense_multiplier"`
	SpawnRadius       float64        `json:"spawn_radius"`
	DelaySeconds      int            `json:"delay_seconds"`
	Capturable        bool           `json:"capturable"`
	Metadata          map[string]any `json:"metadata"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

type SummonWave struct {
	ID                int64          `json:"id"`
	SummonID          string         `json:"summon_id"`
	SourceWaveID      string         `json:"source_wave_id,omitempty"`
	Position          int            `json:"position"`
	Name              string         `json:"name"`
	Kind              string         `json:"kind"`
	PalID             string         `json:"pal_id"`
	Level             int            `json:"level"`
	Count             int            `json:"count"`
	HPMultiplier      float64        `json:"hp_multiplier"`
	AttackMultiplier  float64        `json:"attack_multiplier"`
	DefenseMultiplier float64        `json:"defense_multiplier"`
	SpawnRadius       float64        `json:"spawn_radius"`
	DelaySeconds      int            `json:"delay_seconds"`
	Capturable        bool           `json:"capturable"`
	Status            string         `json:"status"`
	Actor             string         `json:"actor,omitempty"`
	Metadata          map[string]any `json:"metadata"`
	Result            map[string]any `json:"result"`
	Failure           string         `json:"failure,omitempty"`
	StartedAt         string         `json:"started_at,omitempty"`
	CompletedAt       string         `json:"completed_at,omitempty"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

type WaveTransitionRequest struct {
	Status  string         `json:"status"`
	Message string         `json:"message"`
	Result  map[string]any `json:"result"`
}

type CreateSummonRequest struct {
	TemplateID       string         `json:"template_id"`
	RequestKey       string         `json:"request_key"`
	LocationOverride *Location      `json:"location_override,omitempty"`
	Notes            string         `json:"notes"`
	Metadata         map[string]any `json:"metadata"`
}

type Summon struct {
	ID                string         `json:"id"`
	RequestKey        string         `json:"request_key"`
	TemplateID        string         `json:"template_id"`
	TemplateName      string         `json:"template_name"`
	RewardID          string         `json:"reward_id,omitempty"`
	Status            string         `json:"status"`
	ExecutionMode     string         `json:"execution_mode"`
	Actor             string         `json:"actor,omitempty"`
	PalID             string         `json:"pal_id"`
	Level             int            `json:"level"`
	Count             int            `json:"count"`
	HPMultiplier      float64        `json:"hp_multiplier"`
	AttackMultiplier  float64        `json:"attack_multiplier"`
	DefenseMultiplier float64        `json:"defense_multiplier"`
	SpawnRadius       float64        `json:"spawn_radius"`
	Capturable        bool           `json:"capturable"`
	Location          Location       `json:"location"`
	Notes             string         `json:"notes,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	Result            map[string]any `json:"result,omitempty"`
	Failure           string         `json:"failure,omitempty"`
	RequestedAt       string         `json:"requested_at"`
	StartedAt         string         `json:"started_at,omitempty"`
	CompletedAt       string         `json:"completed_at,omitempty"`
	UpdatedAt         string         `json:"updated_at"`
}

type SummonResult struct {
	Summon    Summon `json:"summon"`
	Duplicate bool   `json:"duplicate"`
}

type TransitionRequest struct {
	Status  string         `json:"status"`
	Message string         `json:"message"`
	Result  map[string]any `json:"result"`
}

type SummonEvent struct {
	ID        int64          `json:"id"`
	SummonID  string         `json:"summon_id"`
	From      string         `json:"from_status,omitempty"`
	To        string         `json:"to_status"`
	Actor     string         `json:"actor,omitempty"`
	Message   string         `json:"message,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
	CreatedAt string         `json:"created_at"`
}

type SummonFilter struct {
	Status     string
	TemplateID string
	Limit      int
	Offset     int
}

type Summary struct {
	Rewards          int64 `json:"rewards"`
	EnabledRewards   int64 `json:"enabled_rewards"`
	Templates        int64 `json:"templates"`
	EnabledTemplates int64 `json:"enabled_templates"`
	PendingSummons   int64 `json:"pending_summons"`
	ActiveSummons    int64 `json:"active_summons"`
	CompletedSummons int64 `json:"completed_summons"`
	FailedSummons    int64 `json:"failed_summons"`
	CancelledSummons int64 `json:"cancelled_summons"`
	TemplateWaves    int64 `json:"template_waves"`
	PendingWaves     int64 `json:"pending_waves"`
	ActiveWaves      int64 `json:"active_waves"`
	CompletedWaves   int64 `json:"completed_waves"`
	FailedWaves      int64 `json:"failed_waves"`
	SkippedWaves     int64 `json:"skipped_waves"`
}

func ForPath(path string) (*Service, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("boss database path is empty")
	}
	if cached, ok := serviceCache.Load(path); ok {
		return cached.(*Service), nil
	}
	service, err := Open(path)
	if err != nil {
		return nil, err
	}
	actual, loaded := serviceCache.LoadOrStore(path, service)
	if loaded {
		_ = service.Close()
		return actual.(*Service), nil
	}
	return service, nil
}

func Open(path string) (*Service, error) {
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open boss database: %w", err)
	}
	database.SetMaxOpenConns(1)
	service := &Service{db: database, now: time.Now}
	if err := service.configure(context.Background()); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := service.ensureSchema(context.Background()); err != nil {
		_ = database.Close()
		return nil, err
	}
	return service, nil
}

func (s *Service) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Service) configure(ctx context.Context) error {
	for _, statement := range []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure boss database: %w", err)
		}
	}
	return nil
}

func (s *Service) ensureSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS boss_rewards (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			points INTEGER NOT NULL DEFAULT 0 CHECK(points >= 0),
			items_json TEXT NOT NULL DEFAULT '[]',
			pal_templates_json TEXT NOT NULL DEFAULT '[]',
			enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
			metadata_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			archived_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_rewards_enabled ON boss_rewards(enabled,archived_at,updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS boss_templates (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			pal_id TEXT NOT NULL,
			level INTEGER NOT NULL CHECK(level BETWEEN 1 AND 100),
			spawn_count INTEGER NOT NULL CHECK(spawn_count BETWEEN 1 AND 100),
			hp_multiplier REAL NOT NULL CHECK(hp_multiplier BETWEEN 0.1 AND 100),
			attack_multiplier REAL NOT NULL CHECK(attack_multiplier BETWEEN 0.1 AND 100),
			defense_multiplier REAL NOT NULL CHECK(defense_multiplier BETWEEN 0.1 AND 100),
			spawn_radius REAL NOT NULL CHECK(spawn_radius BETWEEN 0 AND 100000),
			capturable INTEGER NOT NULL DEFAULT 0 CHECK(capturable IN (0,1)),
			cooldown_seconds INTEGER NOT NULL DEFAULT 0 CHECK(cooldown_seconds BETWEEN 0 AND 604800),
			reward_id TEXT NOT NULL DEFAULT '',
			location_json TEXT NOT NULL DEFAULT '{}',
			enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
			metadata_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			archived_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_templates_enabled ON boss_templates(enabled,archived_at,updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_templates_reward ON boss_templates(reward_id)`,
		`CREATE TABLE IF NOT EXISTS boss_template_waves (
			id TEXT PRIMARY KEY,
			template_id TEXT NOT NULL,
			position INTEGER NOT NULL CHECK(position BETWEEN 1 AND 20),
			name TEXT NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('main','minion','reinforcement')),
			pal_id TEXT NOT NULL,
			level INTEGER NOT NULL CHECK(level BETWEEN 1 AND 100),
			spawn_count INTEGER NOT NULL CHECK(spawn_count BETWEEN 1 AND 100),
			hp_multiplier REAL NOT NULL CHECK(hp_multiplier BETWEEN 0.1 AND 100),
			attack_multiplier REAL NOT NULL CHECK(attack_multiplier BETWEEN 0.1 AND 100),
			defense_multiplier REAL NOT NULL CHECK(defense_multiplier BETWEEN 0.1 AND 100),
			spawn_radius REAL NOT NULL CHECK(spawn_radius BETWEEN 0 AND 100000),
			delay_seconds INTEGER NOT NULL DEFAULT 0 CHECK(delay_seconds BETWEEN 0 AND 86400),
			capturable INTEGER NOT NULL DEFAULT 0 CHECK(capturable IN (0,1)),
			metadata_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(template_id,position),
			FOREIGN KEY(template_id) REFERENCES boss_templates(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_template_waves_template ON boss_template_waves(template_id,position)`,
		`CREATE TABLE IF NOT EXISTS boss_summons (
			id TEXT PRIMARY KEY,
			request_key TEXT NOT NULL UNIQUE,
			template_id TEXT NOT NULL,
			template_name TEXT NOT NULL,
			reward_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL CHECK(status IN ('pending','active','completed','failed','cancelled')),
			execution_mode TEXT NOT NULL DEFAULT 'record_only' CHECK(execution_mode IN ('record_only')),
			actor TEXT NOT NULL DEFAULT '',
			pal_id TEXT NOT NULL,
			level INTEGER NOT NULL,
			spawn_count INTEGER NOT NULL,
			hp_multiplier REAL NOT NULL,
			attack_multiplier REAL NOT NULL,
			defense_multiplier REAL NOT NULL,
			spawn_radius REAL NOT NULL,
			capturable INTEGER NOT NULL,
			location_json TEXT NOT NULL,
			notes TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			result_json TEXT NOT NULL DEFAULT '{}',
			failure TEXT NOT NULL DEFAULT '',
			requested_at TEXT NOT NULL,
			started_at TEXT NOT NULL DEFAULT '',
			completed_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_summons_status ON boss_summons(status,updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_summons_template ON boss_summons(template_id,updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS boss_summon_waves (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			summon_id TEXT NOT NULL,
			source_wave_id TEXT NOT NULL DEFAULT '',
			position INTEGER NOT NULL CHECK(position BETWEEN 1 AND 20),
			name TEXT NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('main','minion','reinforcement')),
			pal_id TEXT NOT NULL,
			level INTEGER NOT NULL,
			spawn_count INTEGER NOT NULL,
			hp_multiplier REAL NOT NULL,
			attack_multiplier REAL NOT NULL,
			defense_multiplier REAL NOT NULL,
			spawn_radius REAL NOT NULL,
			delay_seconds INTEGER NOT NULL DEFAULT 0,
			capturable INTEGER NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('pending','active','completed','failed','skipped')),
			actor TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			result_json TEXT NOT NULL DEFAULT '{}',
			failure TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT '',
			completed_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(summon_id,position),
			FOREIGN KEY(summon_id) REFERENCES boss_summons(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_summon_waves_summon ON boss_summon_waves(summon_id,position)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_summon_waves_status ON boss_summon_waves(status,updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS boss_summon_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			summon_id TEXT NOT NULL,
			from_status TEXT NOT NULL DEFAULT '',
			to_status TEXT NOT NULL,
			actor TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			details_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			FOREIGN KEY(summon_id) REFERENCES boss_summons(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_summon_events_summon ON boss_summon_events(summon_id,id DESC)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure boss schema: %w", err)
		}
	}
	return nil
}

func (s *Service) Summary(ctx context.Context) (Summary, error) {
	var result Summary
	queries := []struct {
		destination *int64
		query       string
	}{
		{&result.Rewards, `SELECT COUNT(*) FROM boss_rewards WHERE archived_at=''`},
		{&result.EnabledRewards, `SELECT COUNT(*) FROM boss_rewards WHERE archived_at='' AND enabled=1`},
		{&result.Templates, `SELECT COUNT(*) FROM boss_templates WHERE archived_at=''`},
		{&result.EnabledTemplates, `SELECT COUNT(*) FROM boss_templates WHERE archived_at='' AND enabled=1`},
		{&result.PendingSummons, `SELECT COUNT(*) FROM boss_summons WHERE status='pending'`},
		{&result.ActiveSummons, `SELECT COUNT(*) FROM boss_summons WHERE status='active'`},
		{&result.CompletedSummons, `SELECT COUNT(*) FROM boss_summons WHERE status='completed'`},
		{&result.FailedSummons, `SELECT COUNT(*) FROM boss_summons WHERE status='failed'`},
		{&result.CancelledSummons, `SELECT COUNT(*) FROM boss_summons WHERE status='cancelled'`},
		{&result.TemplateWaves, `SELECT COUNT(*) FROM boss_template_waves`},
		{&result.PendingWaves, `SELECT COUNT(*) FROM boss_summon_waves WHERE status='pending'`},
		{&result.ActiveWaves, `SELECT COUNT(*) FROM boss_summon_waves WHERE status='active'`},
		{&result.CompletedWaves, `SELECT COUNT(*) FROM boss_summon_waves WHERE status='completed'`},
		{&result.FailedWaves, `SELECT COUNT(*) FROM boss_summon_waves WHERE status='failed'`},
		{&result.SkippedWaves, `SELECT COUNT(*) FROM boss_summon_waves WHERE status='skipped'`},
	}
	for _, item := range queries {
		if err := s.db.QueryRowContext(ctx, item.query).Scan(item.destination); err != nil {
			return Summary{}, err
		}
	}
	return result, nil
}

func (s *Service) CreateReward(ctx context.Context, input RewardInput) (Reward, error) {
	normalized, err := normalizeRewardInput(input)
	if err != nil {
		return Reward{}, err
	}
	now := s.timestamp()
	reward := Reward{
		ID: newID("reward"), Name: normalized.Name, Description: normalized.Description,
		Points: normalized.Points, Items: normalized.Items, PalTemplates: normalized.PalTemplates,
		Enabled: normalized.Enabled, Metadata: normalized.Metadata, CreatedAt: now, UpdatedAt: now,
	}
	items, _ := json.Marshal(reward.Items)
	templates, _ := json.Marshal(reward.PalTemplates)
	metadata, _ := json.Marshal(reward.Metadata)
	_, err = s.db.ExecContext(ctx, `INSERT INTO boss_rewards(id,name,description,points,items_json,pal_templates_json,enabled,metadata_json,created_at,updated_at,archived_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		reward.ID, reward.Name, reward.Description, reward.Points, string(items), string(templates), boolInt(reward.Enabled), string(metadata), reward.CreatedAt, reward.UpdatedAt, "")
	if err != nil {
		return Reward{}, err
	}
	return reward, nil
}

func (s *Service) UpdateReward(ctx context.Context, id string, input RewardInput) (Reward, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Reward{}, ErrRewardNotFound
	}
	normalized, err := normalizeRewardInput(input)
	if err != nil {
		return Reward{}, err
	}
	items, _ := json.Marshal(normalized.Items)
	templates, _ := json.Marshal(normalized.PalTemplates)
	metadata, _ := json.Marshal(normalized.Metadata)
	result, err := s.db.ExecContext(ctx, `UPDATE boss_rewards SET name=?,description=?,points=?,items_json=?,pal_templates_json=?,enabled=?,metadata_json=?,updated_at=? WHERE id=? AND archived_at=''`,
		normalized.Name, normalized.Description, normalized.Points, string(items), string(templates), boolInt(normalized.Enabled), string(metadata), s.timestamp(), id)
	if err != nil {
		return Reward{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return Reward{}, ErrRewardNotFound
	}
	return s.GetReward(ctx, id)
}

func (s *Service) ArchiveReward(ctx context.Context, id string) (Reward, error) {
	id = strings.TrimSpace(id)
	now := s.timestamp()
	result, err := s.db.ExecContext(ctx, `UPDATE boss_rewards SET enabled=0,archived_at=?,updated_at=? WHERE id=? AND archived_at=''`, now, now, id)
	if err != nil {
		return Reward{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return Reward{}, ErrRewardNotFound
	}
	return s.GetReward(ctx, id)
}

func (s *Service) GetReward(ctx context.Context, id string) (Reward, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,description,points,items_json,pal_templates_json,enabled,metadata_json,created_at,updated_at,archived_at FROM boss_rewards WHERE id=?`, strings.TrimSpace(id))
	reward, err := scanReward(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Reward{}, ErrRewardNotFound
	}
	return reward, err
}

func (s *Service) ListRewards(ctx context.Context, includeArchived bool, limit, offset int) ([]Reward, error) {
	limit, offset = normalizePage(limit, offset)
	query := `SELECT id,name,description,points,items_json,pal_templates_json,enabled,metadata_json,created_at,updated_at,archived_at FROM boss_rewards`
	if !includeArchived {
		query += ` WHERE archived_at=''`
	}
	query += ` ORDER BY (archived_at='') DESC,enabled DESC,updated_at DESC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Reward{}
	for rows.Next() {
		item, scanErr := scanReward(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) CreateTemplate(ctx context.Context, input TemplateInput) (Template, error) {
	normalized, err := s.normalizeTemplateInput(ctx, input)
	if err != nil {
		return Template{}, err
	}
	now := s.timestamp()
	template := Template{
		ID: newID("template"), Name: normalized.Name, Description: normalized.Description,
		PalID: normalized.PalID, Level: normalized.Level, Count: normalized.Count,
		HPMultiplier: normalized.HPMultiplier, AttackMultiplier: normalized.AttackMultiplier,
		DefenseMultiplier: normalized.DefenseMultiplier, SpawnRadius: normalized.SpawnRadius,
		Capturable: normalized.Capturable, CooldownSeconds: normalized.CooldownSeconds,
		RewardID: normalized.RewardID, Location: normalized.Location, Enabled: normalized.Enabled,
		Metadata: normalized.Metadata, CreatedAt: now, UpdatedAt: now,
	}
	location, _ := json.Marshal(template.Location)
	metadata, _ := json.Marshal(template.Metadata)
	_, err = s.db.ExecContext(ctx, `INSERT INTO boss_templates(id,name,description,pal_id,level,spawn_count,hp_multiplier,attack_multiplier,defense_multiplier,spawn_radius,capturable,cooldown_seconds,reward_id,location_json,enabled,metadata_json,created_at,updated_at,archived_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		template.ID, template.Name, template.Description, template.PalID, template.Level, template.Count,
		template.HPMultiplier, template.AttackMultiplier, template.DefenseMultiplier, template.SpawnRadius,
		boolInt(template.Capturable), template.CooldownSeconds, template.RewardID, string(location), boolInt(template.Enabled),
		string(metadata), template.CreatedAt, template.UpdatedAt, "")
	if err != nil {
		return Template{}, err
	}
	return template, nil
}

func (s *Service) UpdateTemplate(ctx context.Context, id string, input TemplateInput) (Template, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Template{}, ErrTemplateNotFound
	}
	normalized, err := s.normalizeTemplateInput(ctx, input)
	if err != nil {
		return Template{}, err
	}
	location, _ := json.Marshal(normalized.Location)
	metadata, _ := json.Marshal(normalized.Metadata)
	result, err := s.db.ExecContext(ctx, `UPDATE boss_templates SET name=?,description=?,pal_id=?,level=?,spawn_count=?,hp_multiplier=?,attack_multiplier=?,defense_multiplier=?,spawn_radius=?,capturable=?,cooldown_seconds=?,reward_id=?,location_json=?,enabled=?,metadata_json=?,updated_at=? WHERE id=? AND archived_at=''`,
		normalized.Name, normalized.Description, normalized.PalID, normalized.Level, normalized.Count,
		normalized.HPMultiplier, normalized.AttackMultiplier, normalized.DefenseMultiplier, normalized.SpawnRadius,
		boolInt(normalized.Capturable), normalized.CooldownSeconds, normalized.RewardID, string(location), boolInt(normalized.Enabled), string(metadata), s.timestamp(), id)
	if err != nil {
		return Template{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return Template{}, ErrTemplateNotFound
	}
	return s.GetTemplate(ctx, id)
}

func (s *Service) ArchiveTemplate(ctx context.Context, id string) (Template, error) {
	id = strings.TrimSpace(id)
	now := s.timestamp()
	result, err := s.db.ExecContext(ctx, `UPDATE boss_templates SET enabled=0,archived_at=?,updated_at=? WHERE id=? AND archived_at=''`, now, now, id)
	if err != nil {
		return Template{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return Template{}, ErrTemplateNotFound
	}
	return s.GetTemplate(ctx, id)
}

func (s *Service) GetTemplate(ctx context.Context, id string) (Template, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,description,pal_id,level,spawn_count,hp_multiplier,attack_multiplier,defense_multiplier,spawn_radius,capturable,cooldown_seconds,reward_id,location_json,enabled,metadata_json,created_at,updated_at,archived_at FROM boss_templates WHERE id=?`, strings.TrimSpace(id))
	template, err := scanTemplate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Template{}, ErrTemplateNotFound
	}
	return template, err
}

func (s *Service) ListTemplates(ctx context.Context, includeArchived bool, limit, offset int) ([]Template, error) {
	limit, offset = normalizePage(limit, offset)
	query := `SELECT id,name,description,pal_id,level,spawn_count,hp_multiplier,attack_multiplier,defense_multiplier,spawn_radius,capturable,cooldown_seconds,reward_id,location_json,enabled,metadata_json,created_at,updated_at,archived_at FROM boss_templates`
	if !includeArchived {
		query += ` WHERE archived_at=''`
	}
	query += ` ORDER BY (archived_at='') DESC,enabled DESC,updated_at DESC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Template{}
	for rows.Next() {
		item, scanErr := scanTemplate(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ReplaceTemplateWaves(ctx context.Context, templateID string, inputs []WaveInput) ([]Wave, error) {
	templateID = strings.TrimSpace(templateID)
	if templateID == "" || len(inputs) > maximumWaves {
		return nil, ErrInvalidWave
	}
	template, err := s.GetTemplate(ctx, templateID)
	if err != nil {
		return nil, err
	}
	if template.ArchivedAt != "" {
		return nil, ErrTemplateNotFound
	}
	normalized := make([]WaveInput, 0, len(inputs))
	for index, input := range inputs {
		item, normalizeErr := normalizeWaveInput(input, index+1)
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		normalized = append(normalized, item)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(ctx, `DELETE FROM boss_template_waves WHERE template_id=?`, templateID); err != nil {
		return nil, err
	}
	now := s.timestamp()
	result := make([]Wave, 0, len(normalized))
	for index, input := range normalized {
		metadata, _ := json.Marshal(input.Metadata)
		wave := Wave{
			ID: newID("wave"), TemplateID: templateID, Position: index + 1, Name: input.Name,
			Kind: input.Kind, PalID: input.PalID, Level: input.Level, Count: input.Count,
			HPMultiplier: input.HPMultiplier, AttackMultiplier: input.AttackMultiplier,
			DefenseMultiplier: input.DefenseMultiplier, SpawnRadius: input.SpawnRadius,
			DelaySeconds: input.DelaySeconds, Capturable: input.Capturable, Metadata: input.Metadata,
			CreatedAt: now, UpdatedAt: now,
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO boss_template_waves(id,template_id,position,name,kind,pal_id,level,spawn_count,hp_multiplier,attack_multiplier,defense_multiplier,spawn_radius,delay_seconds,capturable,metadata_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			wave.ID, wave.TemplateID, wave.Position, wave.Name, wave.Kind, wave.PalID, wave.Level, wave.Count,
			wave.HPMultiplier, wave.AttackMultiplier, wave.DefenseMultiplier, wave.SpawnRadius,
			wave.DelaySeconds, boolInt(wave.Capturable), string(metadata), wave.CreatedAt, wave.UpdatedAt)
		if err != nil {
			return nil, err
		}
		result = append(result, wave)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) ListTemplateWaves(ctx context.Context, templateID string) ([]Wave, error) {
	templateID = strings.TrimSpace(templateID)
	if templateID == "" {
		return nil, ErrTemplateNotFound
	}
	if _, err := s.GetTemplate(ctx, templateID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,template_id,position,name,kind,pal_id,level,spawn_count,hp_multiplier,attack_multiplier,defense_multiplier,spawn_radius,delay_seconds,capturable,metadata_json,created_at,updated_at FROM boss_template_waves WHERE template_id=? ORDER BY position`, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Wave{}
	for rows.Next() {
		item, scanErr := scanWave(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ListSummonWaves(ctx context.Context, summonID string) ([]SummonWave, error) {
	summonID = strings.TrimSpace(summonID)
	if summonID == "" {
		return nil, ErrSummonNotFound
	}
	if _, err := s.GetSummon(ctx, summonID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, summonWaveSelect+` WHERE summon_id=? ORDER BY position`, summonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SummonWave{}
	for rows.Next() {
		item, scanErr := scanSummonWave(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) TransitionSummonWave(ctx context.Context, summonID string, position int, request WaveTransitionRequest, actor string) (SummonWave, error) {
	summonID = strings.TrimSpace(summonID)
	request.Status = strings.ToLower(strings.TrimSpace(request.Status))
	request.Message = strings.TrimSpace(request.Message)
	actor = strings.TrimSpace(actor)
	if summonID == "" || position < 1 || position > maximumWaves || !validWaveStatus(request.Status) || len(request.Message) > 4096 {
		return SummonWave{}, ErrInvalidWaveTransition
	}
	if _, err := marshalBounded(request.Result); err != nil {
		return SummonWave{}, ErrInvalidWaveTransition
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SummonWave{}, err
	}
	defer rollback(tx)
	summon, err := getSummonTx(ctx, tx, summonID)
	if errors.Is(err, sql.ErrNoRows) {
		return SummonWave{}, ErrSummonNotFound
	}
	if err != nil {
		return SummonWave{}, err
	}
	if terminalStatus(summon.Status) {
		return SummonWave{}, ErrInvalidWaveTransition
	}
	current, err := scanSummonWave(tx.QueryRowContext(ctx, summonWaveSelect+` WHERE summon_id=? AND position=?`, summonID, position))
	if errors.Is(err, sql.ErrNoRows) {
		return SummonWave{}, ErrWaveNotFound
	}
	if err != nil {
		return SummonWave{}, err
	}
	if current.Status == request.Status {
		return current, tx.Commit()
	}
	if !waveTransitionAllowed(current.Status, request.Status) {
		return SummonWave{}, ErrInvalidWaveTransition
	}
	if request.Status == WaveStatusActive {
		var blocking int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM boss_summon_waves WHERE summon_id=? AND position<? AND status IN ('pending','active')`, summonID, position).Scan(&blocking); err != nil {
			return SummonWave{}, err
		}
		if blocking > 0 {
			return SummonWave{}, ErrInvalidWaveTransition
		}
	}
	now := s.timestamp()
	startedAt := current.StartedAt
	completedAt := current.CompletedAt
	failure := ""
	if request.Status == WaveStatusActive && startedAt == "" {
		startedAt = now
	}
	if terminalWaveStatus(request.Status) {
		completedAt = now
	}
	if request.Status == WaveStatusFailed {
		failure = request.Message
	}
	result := normalizedMap(request.Result)
	resultJSON, _ := json.Marshal(result)
	_, err = tx.ExecContext(ctx, `UPDATE boss_summon_waves SET status=?,actor=?,result_json=?,failure=?,started_at=?,completed_at=?,updated_at=? WHERE summon_id=? AND position=?`,
		request.Status, actor, string(resultJSON), failure, startedAt, completedAt, now, summonID, position)
	if err != nil {
		return SummonWave{}, err
	}
	overallStatus := summon.Status
	overallMessage := fmt.Sprintf("wave %d changed from %s to %s", position, current.Status, request.Status)
	if request.Message != "" {
		overallMessage = request.Message
	}
	if request.Status == WaveStatusActive && summon.Status == SummonStatusPending {
		overallStatus = SummonStatusActive
		_, err = tx.ExecContext(ctx, `UPDATE boss_summons SET status=?,started_at=CASE WHEN started_at='' THEN ? ELSE started_at END,updated_at=? WHERE id=?`, overallStatus, now, now, summonID)
		if err != nil {
			return SummonWave{}, err
		}
	}
	if request.Status == WaveStatusFailed {
		overallStatus = SummonStatusFailed
		_, err = tx.ExecContext(ctx, `UPDATE boss_summons SET status=?,failure=?,completed_at=?,updated_at=? WHERE id=?`, overallStatus, overallMessage, now, now, summonID)
		if err != nil {
			return SummonWave{}, err
		}
	} else {
		var remaining, failed int
		if err := tx.QueryRowContext(ctx, `SELECT SUM(CASE WHEN status IN ('pending','active') THEN 1 ELSE 0 END),SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END) FROM boss_summon_waves WHERE summon_id=?`, summonID).Scan(&remaining, &failed); err != nil {
			return SummonWave{}, err
		}
		if remaining == 0 && failed == 0 {
			overallStatus = SummonStatusCompleted
			_, err = tx.ExecContext(ctx, `UPDATE boss_summons SET status=?,started_at=CASE WHEN started_at='' THEN ? ELSE started_at END,completed_at=?,updated_at=? WHERE id=?`, overallStatus, now, now, now, summonID)
			if err != nil {
				return SummonWave{}, err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE boss_summon_waves SET status='skipped',actor=?,failure='summon terminated',completed_at=?,updated_at=? WHERE summon_id=? AND status IN ('pending','active') AND position<>? AND ? IN ('failed','completed')`, actor, now, now, summonID, position, overallStatus); err != nil {
		return SummonWave{}, err
	}
	if err := insertSummonEvent(ctx, tx, summonID, summon.Status, overallStatus, actor, overallMessage, map[string]any{
		"wave_position": position, "wave_name": current.Name, "wave_from_status": current.Status,
		"wave_to_status": request.Status, "wave_result": result,
	}, now); err != nil {
		return SummonWave{}, err
	}
	if err := tx.Commit(); err != nil {
		return SummonWave{}, err
	}
	return s.getSummonWave(ctx, summonID, position)
}

func (s *Service) CreateSummon(ctx context.Context, request CreateSummonRequest, actor string) (SummonResult, error) {
	request.TemplateID = strings.TrimSpace(request.TemplateID)
	request.RequestKey = strings.TrimSpace(request.RequestKey)
	request.Notes = strings.TrimSpace(request.Notes)
	actor = strings.TrimSpace(actor)
	if request.TemplateID == "" || request.RequestKey == "" || len(request.RequestKey) > 128 || len(request.Notes) > 4096 {
		return SummonResult{}, ErrInvalidSummon
	}
	if existing, err := s.summonByRequestKey(ctx, request.RequestKey); err == nil {
		return SummonResult{Summon: existing, Duplicate: true}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return SummonResult{}, err
	}
	template, err := s.GetTemplate(ctx, request.TemplateID)
	if err != nil {
		return SummonResult{}, err
	}
	if !template.Enabled || template.ArchivedAt != "" {
		return SummonResult{}, ErrTemplateDisabled
	}
	location := template.Location
	if request.LocationOverride != nil {
		location = *request.LocationOverride
		location.Label = strings.TrimSpace(location.Label)
		if len(location.Label) > 128 {
			return SummonResult{}, ErrInvalidSummon
		}
		if err := validateLocation(location); err != nil {
			return SummonResult{}, err
		}
	}
	if _, err := marshalBounded(request.Metadata); err != nil {
		return SummonResult{}, ErrInvalidSummon
	}
	waves, err := s.ListTemplateWaves(ctx, template.ID)
	if err != nil {
		return SummonResult{}, err
	}
	if len(waves) == 0 {
		waves = []Wave{{
			ID: "", TemplateID: template.ID, Position: 1, Name: template.Name, Kind: WaveKindMain,
			PalID: template.PalID, Level: template.Level, Count: template.Count,
			HPMultiplier: template.HPMultiplier, AttackMultiplier: template.AttackMultiplier,
			DefenseMultiplier: template.DefenseMultiplier, SpawnRadius: template.SpawnRadius,
			Capturable: template.Capturable, Metadata: map[string]any{"implicit": true},
		}}
	}
	now := s.timestamp()
	summon := Summon{
		ID: newID("summon"), RequestKey: request.RequestKey, TemplateID: template.ID,
		TemplateName: template.Name, RewardID: template.RewardID, Status: SummonStatusPending,
		ExecutionMode: ExecutionModeRecordOnly, Actor: actor, PalID: template.PalID, Level: template.Level,
		Count: template.Count, HPMultiplier: template.HPMultiplier, AttackMultiplier: template.AttackMultiplier,
		DefenseMultiplier: template.DefenseMultiplier, SpawnRadius: template.SpawnRadius,
		Capturable: template.Capturable, Location: location, Notes: request.Notes,
		Metadata: normalizedMap(request.Metadata), Result: map[string]any{}, RequestedAt: now, UpdatedAt: now,
	}
	locationJSON, _ := json.Marshal(summon.Location)
	metadataJSON, _ := json.Marshal(summon.Metadata)
	resultJSON, _ := json.Marshal(summon.Result)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SummonResult{}, err
	}
	defer rollback(tx)
	_, err = tx.ExecContext(ctx, `INSERT INTO boss_summons(id,request_key,template_id,template_name,reward_id,status,execution_mode,actor,pal_id,level,spawn_count,hp_multiplier,attack_multiplier,defense_multiplier,spawn_radius,capturable,location_json,notes,metadata_json,result_json,failure,requested_at,started_at,completed_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		summon.ID, summon.RequestKey, summon.TemplateID, summon.TemplateName, summon.RewardID, summon.Status,
		summon.ExecutionMode, summon.Actor, summon.PalID, summon.Level, summon.Count, summon.HPMultiplier,
		summon.AttackMultiplier, summon.DefenseMultiplier, summon.SpawnRadius, boolInt(summon.Capturable),
		string(locationJSON), summon.Notes, string(metadataJSON), string(resultJSON), "", summon.RequestedAt, "", "", summon.UpdatedAt)
	if err != nil {
		if existing, loadErr := summonByRequestKeyTx(ctx, tx, request.RequestKey); loadErr == nil {
			return SummonResult{Summon: existing, Duplicate: true}, nil
		}
		return SummonResult{}, err
	}
	for _, wave := range waves {
		metadata, _ := json.Marshal(wave.Metadata)
		_, err = tx.ExecContext(ctx, `INSERT INTO boss_summon_waves(summon_id,source_wave_id,position,name,kind,pal_id,level,spawn_count,hp_multiplier,attack_multiplier,defense_multiplier,spawn_radius,delay_seconds,capturable,status,actor,metadata_json,result_json,failure,started_at,completed_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			summon.ID, wave.ID, wave.Position, wave.Name, wave.Kind, wave.PalID, wave.Level, wave.Count,
			wave.HPMultiplier, wave.AttackMultiplier, wave.DefenseMultiplier, wave.SpawnRadius,
			wave.DelaySeconds, boolInt(wave.Capturable), WaveStatusPending, "", string(metadata), "{}", "", "", "", now, now)
		if err != nil {
			return SummonResult{}, err
		}
	}
	if err := insertSummonEvent(ctx, tx, summon.ID, "", SummonStatusPending, actor, "manual summon record created", map[string]any{"execution_mode": ExecutionModeRecordOnly, "wave_count": len(waves)}, now); err != nil {
		return SummonResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return SummonResult{}, err
	}
	return SummonResult{Summon: summon}, nil
}

func (s *Service) TransitionSummon(ctx context.Context, id string, request TransitionRequest, actor string) (Summon, error) {
	id = strings.TrimSpace(id)
	request.Status = strings.ToLower(strings.TrimSpace(request.Status))
	request.Message = strings.TrimSpace(request.Message)
	actor = strings.TrimSpace(actor)
	if id == "" || !validSummonStatus(request.Status) || len(request.Message) > 4096 {
		return Summon{}, ErrInvalidTransition
	}
	if _, err := marshalBounded(request.Result); err != nil {
		return Summon{}, ErrInvalidTransition
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Summon{}, err
	}
	defer rollback(tx)
	current, err := getSummonTx(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Summon{}, ErrSummonNotFound
	}
	if err != nil {
		return Summon{}, err
	}
	if current.Status == request.Status {
		return current, tx.Commit()
	}
	if !transitionAllowed(current.Status, request.Status) {
		return Summon{}, ErrInvalidTransition
	}
	now := s.timestamp()
	startedAt := current.StartedAt
	completedAt := current.CompletedAt
	failure := current.Failure
	if request.Status == SummonStatusActive && startedAt == "" {
		startedAt = now
	}
	if terminalStatus(request.Status) {
		completedAt = now
	}
	if request.Status == SummonStatusFailed {
		failure = request.Message
	} else if request.Status != SummonStatusFailed {
		failure = ""
	}
	result := normalizedMap(request.Result)
	resultJSON, _ := json.Marshal(result)
	_, err = tx.ExecContext(ctx, `UPDATE boss_summons SET status=?,result_json=?,failure=?,started_at=?,completed_at=?,updated_at=? WHERE id=?`,
		request.Status, string(resultJSON), failure, startedAt, completedAt, now, id)
	if err != nil {
		return Summon{}, err
	}
	if terminalStatus(request.Status) {
		skipReason := ""
		if request.Status != SummonStatusCompleted {
			skipReason = request.Message
			if skipReason == "" {
				skipReason = "summon closed manually"
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE boss_summon_waves SET status='skipped',actor=?,failure=?,completed_at=?,updated_at=? WHERE summon_id=? AND status IN ('pending','active')`, actor, skipReason, now, now, id); err != nil {
			return Summon{}, err
		}
	}
	if err := insertSummonEvent(ctx, tx, id, current.Status, request.Status, actor, request.Message, result, now); err != nil {
		return Summon{}, err
	}
	if err := tx.Commit(); err != nil {
		return Summon{}, err
	}
	return s.GetSummon(ctx, id)
}

func (s *Service) GetSummon(ctx context.Context, id string) (Summon, error) {
	row := s.db.QueryRowContext(ctx, summonSelect+` WHERE id=?`, strings.TrimSpace(id))
	summon, err := scanSummon(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Summon{}, ErrSummonNotFound
	}
	return summon, err
}

func (s *Service) ListSummons(ctx context.Context, filter SummonFilter) ([]Summon, error) {
	filter.Status = strings.ToLower(strings.TrimSpace(filter.Status))
	filter.TemplateID = strings.TrimSpace(filter.TemplateID)
	if filter.Status != "" && !validSummonStatus(filter.Status) {
		return nil, ErrInvalidSummon
	}
	filter.Limit, filter.Offset = normalizePage(filter.Limit, filter.Offset)
	query := summonSelect + ` WHERE 1=1`
	args := []any{}
	if filter.Status != "" {
		query += ` AND status=?`
		args = append(args, filter.Status)
	}
	if filter.TemplateID != "" {
		query += ` AND template_id=?`
		args = append(args, filter.TemplateID)
	}
	query += ` ORDER BY requested_at DESC LIMIT ? OFFSET ?`
	args = append(args, filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Summon{}
	for rows.Next() {
		item, scanErr := scanSummon(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ListSummonEvents(ctx context.Context, summonID string, limit, offset int) ([]SummonEvent, error) {
	summonID = strings.TrimSpace(summonID)
	if summonID == "" {
		return nil, ErrSummonNotFound
	}
	if _, err := s.GetSummon(ctx, summonID); err != nil {
		return nil, err
	}
	limit, offset = normalizePage(limit, offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id,summon_id,from_status,to_status,actor,message,details_json,created_at FROM boss_summon_events WHERE summon_id=? ORDER BY id DESC LIMIT ? OFFSET ?`, summonID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SummonEvent{}
	for rows.Next() {
		var item SummonEvent
		var details string
		if err := rows.Scan(&item.ID, &item.SummonID, &item.From, &item.To, &item.Actor, &item.Message, &details, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Details = decodeObject(details)
		items = append(items, item)
	}
	return items, rows.Err()
}

func normalizeRewardInput(input RewardInput) (RewardInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if input.Name == "" || len(input.Name) > 128 || len(input.Description) > 4096 || input.Points < 0 || input.Points > 1_000_000_000_000 {
		return RewardInput{}, ErrInvalidReward
	}
	if len(input.Items) > 100 || len(input.PalTemplates) > 100 {
		return RewardInput{}, ErrInvalidReward
	}
	seenItems := map[string]bool{}
	for index := range input.Items {
		input.Items[index].ItemID = strings.TrimSpace(input.Items[index].ItemID)
		if !identifierPattern.MatchString(input.Items[index].ItemID) || input.Items[index].Count < 1 || input.Items[index].Count > 2_147_483_647 {
			return RewardInput{}, ErrInvalidReward
		}
		key := strings.ToLower(input.Items[index].ItemID)
		if seenItems[key] {
			return RewardInput{}, ErrInvalidReward
		}
		seenItems[key] = true
	}
	seenTemplates := map[string]bool{}
	for index := range input.PalTemplates {
		input.PalTemplates[index] = strings.TrimSpace(input.PalTemplates[index])
		if !templateNamePattern.MatchString(input.PalTemplates[index]) {
			return RewardInput{}, ErrInvalidReward
		}
		key := strings.ToLower(input.PalTemplates[index])
		if seenTemplates[key] {
			return RewardInput{}, ErrInvalidReward
		}
		seenTemplates[key] = true
	}
	if _, err := marshalBounded(input.Metadata); err != nil {
		return RewardInput{}, ErrInvalidReward
	}
	input.Metadata = normalizedMap(input.Metadata)
	return input, nil
}

func (s *Service) normalizeTemplateInput(ctx context.Context, input TemplateInput) (TemplateInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.PalID = strings.TrimSpace(input.PalID)
	input.RewardID = strings.TrimSpace(input.RewardID)
	input.Location.Label = strings.TrimSpace(input.Location.Label)
	if input.Name == "" || len(input.Name) > 128 || len(input.Description) > 4096 || !identifierPattern.MatchString(input.PalID) {
		return TemplateInput{}, ErrInvalidTemplate
	}
	if input.Level < 1 || input.Level > 100 || input.Count < 1 || input.Count > 100 {
		return TemplateInput{}, ErrInvalidTemplate
	}
	if !validMultiplier(input.HPMultiplier) || !validMultiplier(input.AttackMultiplier) || !validMultiplier(input.DefenseMultiplier) {
		return TemplateInput{}, ErrInvalidTemplate
	}
	if math.IsNaN(input.SpawnRadius) || math.IsInf(input.SpawnRadius, 0) || input.SpawnRadius < 0 || input.SpawnRadius > 100000 {
		return TemplateInput{}, ErrInvalidTemplate
	}
	if input.CooldownSeconds < 0 || input.CooldownSeconds > 604800 || len(input.Location.Label) > 128 {
		return TemplateInput{}, ErrInvalidTemplate
	}
	if err := validateLocation(input.Location); err != nil {
		return TemplateInput{}, err
	}
	if input.RewardID != "" {
		if _, err := s.GetReward(ctx, input.RewardID); err != nil {
			if errors.Is(err, ErrRewardNotFound) {
				return TemplateInput{}, ErrRewardNotFound
			}
			return TemplateInput{}, err
		}
	}
	if _, err := marshalBounded(input.Metadata); err != nil {
		return TemplateInput{}, ErrInvalidTemplate
	}
	input.Metadata = normalizedMap(input.Metadata)
	return input, nil
}

func normalizeWaveInput(input WaveInput, position int) (WaveInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	input.PalID = strings.TrimSpace(input.PalID)
	if input.Name == "" {
		input.Name = fmt.Sprintf("第%d波", position)
	}
	if input.Kind == "" {
		if position == 1 {
			input.Kind = WaveKindMain
		} else {
			input.Kind = WaveKindReinforcement
		}
	}
	if len(input.Name) > 128 || !validWaveKind(input.Kind) || !identifierPattern.MatchString(input.PalID) {
		return WaveInput{}, ErrInvalidWave
	}
	if input.Level < 1 || input.Level > 100 || input.Count < 1 || input.Count > 100 {
		return WaveInput{}, ErrInvalidWave
	}
	if !validMultiplier(input.HPMultiplier) || !validMultiplier(input.AttackMultiplier) || !validMultiplier(input.DefenseMultiplier) {
		return WaveInput{}, ErrInvalidWave
	}
	if math.IsNaN(input.SpawnRadius) || math.IsInf(input.SpawnRadius, 0) || input.SpawnRadius < 0 || input.SpawnRadius > 100000 {
		return WaveInput{}, ErrInvalidWave
	}
	if input.DelaySeconds < 0 || input.DelaySeconds > 86400 {
		return WaveInput{}, ErrInvalidWave
	}
	if _, err := marshalBounded(input.Metadata); err != nil {
		return WaveInput{}, ErrInvalidWave
	}
	input.Metadata = normalizedMap(input.Metadata)
	return input, nil
}

func validWaveKind(kind string) bool {
	return kind == WaveKindMain || kind == WaveKindMinion || kind == WaveKindReinforcement
}

func validWaveStatus(status string) bool {
	switch status {
	case WaveStatusPending, WaveStatusActive, WaveStatusCompleted, WaveStatusFailed, WaveStatusSkipped:
		return true
	default:
		return false
	}
}

func terminalWaveStatus(status string) bool {
	return status == WaveStatusCompleted || status == WaveStatusFailed || status == WaveStatusSkipped
}

func waveTransitionAllowed(from, to string) bool {
	switch from {
	case WaveStatusPending:
		return to == WaveStatusActive || to == WaveStatusFailed || to == WaveStatusSkipped
	case WaveStatusActive:
		return to == WaveStatusCompleted || to == WaveStatusFailed || to == WaveStatusSkipped
	default:
		return false
	}
}

func validateLocation(location Location) error {
	for _, value := range []float64{location.X, location.Y, location.Z} {
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 10_000_000 {
			return ErrInvalidTemplate
		}
	}
	return nil
}

func validMultiplier(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0.1 && value <= 100
}

func validSummonStatus(status string) bool {
	switch status {
	case SummonStatusPending, SummonStatusActive, SummonStatusCompleted, SummonStatusFailed, SummonStatusCancelled:
		return true
	default:
		return false
	}
}

func terminalStatus(status string) bool {
	return status == SummonStatusCompleted || status == SummonStatusFailed || status == SummonStatusCancelled
}

func transitionAllowed(from, to string) bool {
	switch from {
	case SummonStatusPending:
		return to == SummonStatusActive || to == SummonStatusFailed || to == SummonStatusCancelled
	case SummonStatusActive:
		return to == SummonStatusCompleted || to == SummonStatusFailed || to == SummonStatusCancelled
	default:
		return false
	}
}

func scanReward(scanner interface{ Scan(...any) error }) (Reward, error) {
	var item Reward
	var itemsJSON, templatesJSON, metadataJSON string
	var enabled int
	if err := scanner.Scan(&item.ID, &item.Name, &item.Description, &item.Points, &itemsJSON, &templatesJSON, &enabled, &metadataJSON, &item.CreatedAt, &item.UpdatedAt, &item.ArchivedAt); err != nil {
		return Reward{}, err
	}
	item.Enabled = enabled == 1
	_ = json.Unmarshal([]byte(itemsJSON), &item.Items)
	_ = json.Unmarshal([]byte(templatesJSON), &item.PalTemplates)
	item.Metadata = decodeObject(metadataJSON)
	if item.Items == nil {
		item.Items = []RewardItem{}
	}
	if item.PalTemplates == nil {
		item.PalTemplates = []string{}
	}
	return item, nil
}

func scanTemplate(scanner interface{ Scan(...any) error }) (Template, error) {
	var item Template
	var locationJSON, metadataJSON string
	var capturable, enabled int
	if err := scanner.Scan(&item.ID, &item.Name, &item.Description, &item.PalID, &item.Level, &item.Count,
		&item.HPMultiplier, &item.AttackMultiplier, &item.DefenseMultiplier, &item.SpawnRadius,
		&capturable, &item.CooldownSeconds, &item.RewardID, &locationJSON, &enabled, &metadataJSON,
		&item.CreatedAt, &item.UpdatedAt, &item.ArchivedAt); err != nil {
		return Template{}, err
	}
	item.Capturable = capturable == 1
	item.Enabled = enabled == 1
	_ = json.Unmarshal([]byte(locationJSON), &item.Location)
	item.Metadata = decodeObject(metadataJSON)
	return item, nil
}

func scanWave(scanner interface{ Scan(...any) error }) (Wave, error) {
	var item Wave
	var capturable int
	var metadataJSON string
	if err := scanner.Scan(&item.ID, &item.TemplateID, &item.Position, &item.Name, &item.Kind, &item.PalID,
		&item.Level, &item.Count, &item.HPMultiplier, &item.AttackMultiplier, &item.DefenseMultiplier,
		&item.SpawnRadius, &item.DelaySeconds, &capturable, &metadataJSON, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return Wave{}, err
	}
	item.Capturable = capturable == 1
	item.Metadata = decodeObject(metadataJSON)
	return item, nil
}

const summonWaveSelect = `SELECT id,summon_id,source_wave_id,position,name,kind,pal_id,level,spawn_count,hp_multiplier,attack_multiplier,defense_multiplier,spawn_radius,delay_seconds,capturable,status,actor,metadata_json,result_json,failure,started_at,completed_at,created_at,updated_at FROM boss_summon_waves`

func scanSummonWave(scanner interface{ Scan(...any) error }) (SummonWave, error) {
	var item SummonWave
	var capturable int
	var metadataJSON, resultJSON string
	if err := scanner.Scan(&item.ID, &item.SummonID, &item.SourceWaveID, &item.Position, &item.Name, &item.Kind,
		&item.PalID, &item.Level, &item.Count, &item.HPMultiplier, &item.AttackMultiplier,
		&item.DefenseMultiplier, &item.SpawnRadius, &item.DelaySeconds, &capturable, &item.Status,
		&item.Actor, &metadataJSON, &resultJSON, &item.Failure, &item.StartedAt, &item.CompletedAt,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		return SummonWave{}, err
	}
	item.Capturable = capturable == 1
	item.Metadata = decodeObject(metadataJSON)
	item.Result = decodeObject(resultJSON)
	return item, nil
}

func (s *Service) getSummonWave(ctx context.Context, summonID string, position int) (SummonWave, error) {
	item, err := scanSummonWave(s.db.QueryRowContext(ctx, summonWaveSelect+` WHERE summon_id=? AND position=?`, summonID, position))
	if errors.Is(err, sql.ErrNoRows) {
		return SummonWave{}, ErrWaveNotFound
	}
	return item, err
}

const summonSelect = `SELECT id,request_key,template_id,template_name,reward_id,status,execution_mode,actor,pal_id,level,spawn_count,hp_multiplier,attack_multiplier,defense_multiplier,spawn_radius,capturable,location_json,notes,metadata_json,result_json,failure,requested_at,started_at,completed_at,updated_at FROM boss_summons`

func scanSummon(scanner interface{ Scan(...any) error }) (Summon, error) {
	var item Summon
	var locationJSON, metadataJSON, resultJSON string
	var capturable int
	if err := scanner.Scan(&item.ID, &item.RequestKey, &item.TemplateID, &item.TemplateName, &item.RewardID,
		&item.Status, &item.ExecutionMode, &item.Actor, &item.PalID, &item.Level, &item.Count,
		&item.HPMultiplier, &item.AttackMultiplier, &item.DefenseMultiplier, &item.SpawnRadius,
		&capturable, &locationJSON, &item.Notes, &metadataJSON, &resultJSON, &item.Failure,
		&item.RequestedAt, &item.StartedAt, &item.CompletedAt, &item.UpdatedAt); err != nil {
		return Summon{}, err
	}
	item.Capturable = capturable == 1
	_ = json.Unmarshal([]byte(locationJSON), &item.Location)
	item.Metadata = decodeObject(metadataJSON)
	item.Result = decodeObject(resultJSON)
	return item, nil
}

func (s *Service) summonByRequestKey(ctx context.Context, key string) (Summon, error) {
	return scanSummon(s.db.QueryRowContext(ctx, summonSelect+` WHERE request_key=?`, strings.TrimSpace(key)))
}

func getSummonTx(ctx context.Context, tx *sql.Tx, id string) (Summon, error) {
	return scanSummon(tx.QueryRowContext(ctx, summonSelect+` WHERE id=?`, strings.TrimSpace(id)))
}

func summonByRequestKeyTx(ctx context.Context, tx *sql.Tx, key string) (Summon, error) {
	return scanSummon(tx.QueryRowContext(ctx, summonSelect+` WHERE request_key=?`, strings.TrimSpace(key)))
}

func insertSummonEvent(ctx context.Context, tx *sql.Tx, summonID, from, to, actor, message string, details map[string]any, createdAt string) error {
	body, _ := json.Marshal(normalizedMap(details))
	_, err := tx.ExecContext(ctx, `INSERT INTO boss_summon_events(summon_id,from_status,to_status,actor,message,details_json,created_at) VALUES(?,?,?,?,?,?,?)`,
		summonID, from, to, actor, message, string(body), createdAt)
	return err
}

func marshalBounded(value any) ([]byte, error) {
	if value == nil {
		value = map[string]any{}
	}
	body, err := json.Marshal(value)
	if err != nil || len(body) > maximumJSON {
		return nil, errors.New("JSON payload is invalid or too large")
	}
	return body, nil
}

func decodeObject(value string) map[string]any {
	result := map[string]any{}
	if strings.TrimSpace(value) != "" {
		_ = json.Unmarshal([]byte(value), &result)
	}
	if result == nil {
		result = map[string]any{}
	}
	return result
}

func normalizedMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func normalizePage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maximumLimit {
		limit = maximumLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func (s *Service) timestamp() string {
	return s.now().UTC().Format(time.RFC3339Nano)
}

func newID(prefix string) string {
	var random [12]byte
	if _, err := rand.Read(random[:]); err == nil {
		return prefix + "_" + hex.EncodeToString(random[:])
	}
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func rollback(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}
