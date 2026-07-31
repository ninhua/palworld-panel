package tasks

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"palpanel/internal/economy"

	_ "modernc.org/sqlite"
	_ "time/tzdata"
)

var (
	ErrInvalidDefinition = errors.New("task definition is invalid")
	ErrRewardDelivery    = errors.New("task reward delivery failed")
	serviceCache         sync.Map
	eventTypePattern     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	fieldPathPattern     = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
)

const (
	defaultTimezone  = "Asia/Shanghai"
	maximumTaskValue = int64(1_000_000_000_000)
	maximumEventGain = int64(1_000_000_000)
)

type Service struct {
	db       *sql.DB
	location *time.Location
	now      func() time.Time
}

type DefinitionInput struct {
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	EventType    string         `json:"event_type"`
	TargetAmount int64          `json:"target_amount"`
	RewardPoints int64          `json:"reward_points"`
	Cycle        string         `json:"cycle"`
	AmountField  string         `json:"amount_field,omitempty"`
	Filters      map[string]any `json:"filters,omitempty"`
	Enabled      bool           `json:"enabled"`
}

type Definition struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	EventType    string         `json:"event_type"`
	TargetAmount int64          `json:"target_amount"`
	RewardPoints int64          `json:"reward_points"`
	Cycle        string         `json:"cycle"`
	AmountField  string         `json:"amount_field,omitempty"`
	Filters      map[string]any `json:"filters,omitempty"`
	Enabled      bool           `json:"enabled"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
	ArchivedAt   string         `json:"archived_at,omitempty"`
}

type Event struct {
	EventID    string
	Type       string
	PlayerUID  string
	Nickname   string
	SteamID    string
	OccurredAt string
	Payload    map[string]any
}

type Progress struct {
	TaskID        string         `json:"task_id"`
	TaskName      string         `json:"task_name"`
	Description   string         `json:"description,omitempty"`
	EventType     string         `json:"event_type"`
	Cycle         string         `json:"cycle"`
	CycleKey      string         `json:"cycle_key"`
	TargetAmount  int64          `json:"target_amount"`
	RewardPoints  int64          `json:"reward_points"`
	Progress      int64          `json:"progress"`
	Completed     bool           `json:"completed"`
	CompletedAt   string         `json:"completed_at,omitempty"`
	RewardStatus  string         `json:"reward_status"`
	RewardError   string         `json:"reward_error,omitempty"`
	RewardGranted bool           `json:"reward_granted"`
	UpdatedAt     string         `json:"updated_at,omitempty"`
	Filters       map[string]any `json:"filters,omitempty"`
	AmountField   string         `json:"amount_field,omitempty"`
}

type ProgressUpdate struct {
	TaskID        string `json:"task_id"`
	TaskName      string `json:"task_name"`
	CycleKey      string `json:"cycle_key"`
	Added         int64  `json:"added"`
	Progress      int64  `json:"progress"`
	TargetAmount  int64  `json:"target_amount"`
	Completed     bool   `json:"completed"`
	JustCompleted bool   `json:"just_completed"`
	RewardPoints  int64  `json:"reward_points"`
	RewardStatus  string `json:"reward_status"`
	RewardGranted bool   `json:"reward_granted"`
	Duplicate     bool   `json:"duplicate"`
}

type RetrySummary struct {
	Selected int `json:"selected"`
	Granted  int `json:"granted"`
	Failed   int `json:"failed"`
}

type rewardRecord struct {
	TaskID            string
	TaskName          string
	PlayerUID         string
	Nickname          string
	SteamID           string
	CycleKey          string
	Points            int64
	Status            string
	LedgerReferenceID string
	Attempts          int
	LastError         string
}

type applyResult struct {
	Update ProgressUpdate
	Reward *rewardRecord
}

func ForPath(path, timezone string) (*Service, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("task database path is empty")
	}
	location, err := loadLocation(timezone)
	if err != nil {
		return nil, err
	}
	key := path + "\x00" + location.String()
	if cached, ok := serviceCache.Load(key); ok {
		return cached.(*Service), nil
	}
	service, err := open(path, location)
	if err != nil {
		return nil, err
	}
	actual, loaded := serviceCache.LoadOrStore(key, service)
	if loaded {
		_ = service.Close()
		return actual.(*Service), nil
	}
	return service, nil
}

func Open(path, timezone string) (*Service, error) {
	location, err := loadLocation(timezone)
	if err != nil {
		return nil, err
	}
	return open(path, location)
}

func open(path string, location *time.Location) (*Service, error) {
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open task database: %w", err)
	}
	database.SetMaxOpenConns(1)
	service := &Service{db: database, location: location, now: time.Now}
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
			return fmt.Errorf("configure task database: %w", err)
		}
	}
	return nil
}

func (s *Service) ensureSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS operations_tasks (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			event_type TEXT NOT NULL,
			target_amount INTEGER NOT NULL CHECK(target_amount > 0),
			reward_points INTEGER NOT NULL CHECK(reward_points >= 0),
			cycle TEXT NOT NULL CHECK(cycle IN ('once','daily','weekly')),
			amount_field TEXT NOT NULL DEFAULT '',
			filters_json TEXT NOT NULL DEFAULT '{}',
			enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			archived_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_operations_tasks_event ON operations_tasks(event_type,enabled,archived_at)`,
		`CREATE TABLE IF NOT EXISTS operations_task_progress (
			task_id TEXT NOT NULL,
			player_uid TEXT NOT NULL,
			cycle_key TEXT NOT NULL,
			progress INTEGER NOT NULL DEFAULT 0 CHECK(progress >= 0),
			completed INTEGER NOT NULL DEFAULT 0 CHECK(completed IN (0,1)),
			completed_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY(task_id,player_uid,cycle_key),
			FOREIGN KEY(task_id) REFERENCES operations_tasks(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_operations_task_progress_player ON operations_task_progress(player_uid,updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS operations_task_event_receipts (
			task_id TEXT NOT NULL,
			player_uid TEXT NOT NULL,
			cycle_key TEXT NOT NULL,
			event_id TEXT NOT NULL,
			amount INTEGER NOT NULL CHECK(amount > 0),
			created_at TEXT NOT NULL,
			PRIMARY KEY(task_id,player_uid,cycle_key,event_id),
			FOREIGN KEY(task_id) REFERENCES operations_tasks(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_operations_task_receipts_event ON operations_task_event_receipts(event_id)`,
		`CREATE TABLE IF NOT EXISTS operations_task_rewards (
			task_id TEXT NOT NULL,
			player_uid TEXT NOT NULL,
			cycle_key TEXT NOT NULL,
			nickname TEXT NOT NULL DEFAULT '',
			steam_id TEXT NOT NULL DEFAULT '',
			points INTEGER NOT NULL CHECK(points > 0),
			status TEXT NOT NULL CHECK(status IN ('pending','granted','failed')),
			ledger_reference_id TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts >= 0),
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			granted_at TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(task_id,player_uid,cycle_key),
			FOREIGN KEY(task_id) REFERENCES operations_tasks(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_operations_task_rewards_status ON operations_task_rewards(status,updated_at)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create task schema: %w", err)
		}
	}
	return nil
}

func (s *Service) ListDefinitions(ctx context.Context, includeArchived bool) ([]Definition, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,description,event_type,target_amount,reward_points,cycle,amount_field,filters_json,enabled,created_at,updated_at,archived_at FROM operations_tasks WHERE (?=1 OR archived_at='') ORDER BY archived_at='',enabled DESC,created_at DESC`, boolInt(includeArchived))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Definition, 0)
	for rows.Next() {
		definition, err := scanDefinition(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, definition)
	}
	return items, rows.Err()
}

func (s *Service) GetDefinition(ctx context.Context, id string) (Definition, error) {
	return scanDefinition(s.db.QueryRowContext(ctx, `SELECT id,name,description,event_type,target_amount,reward_points,cycle,amount_field,filters_json,enabled,created_at,updated_at,archived_at FROM operations_tasks WHERE id=?`, strings.TrimSpace(id)))
}

func (s *Service) CreateDefinition(ctx context.Context, input DefinitionInput) (Definition, error) {
	input, filtersJSON, err := normalizeDefinition(input)
	if err != nil {
		return Definition{}, err
	}
	id := "task_" + newID()
	now := s.timestamp()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO operations_tasks(id,name,description,event_type,target_amount,reward_points,cycle,amount_field,filters_json,enabled,created_at,updated_at,archived_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?, '')`, id, input.Name, input.Description, input.EventType, input.TargetAmount, input.RewardPoints, input.Cycle, input.AmountField, filtersJSON, boolInt(input.Enabled), now, now); err != nil {
		return Definition{}, err
	}
	return s.GetDefinition(ctx, id)
}

func (s *Service) UpdateDefinition(ctx context.Context, id string, input DefinitionInput) (Definition, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Definition{}, ErrInvalidDefinition
	}
	input, filtersJSON, err := normalizeDefinition(input)
	if err != nil {
		return Definition{}, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE operations_tasks SET name=?,description=?,event_type=?,target_amount=?,reward_points=?,cycle=?,amount_field=?,filters_json=?,enabled=?,updated_at=? WHERE id=? AND archived_at=''`, input.Name, input.Description, input.EventType, input.TargetAmount, input.RewardPoints, input.Cycle, input.AmountField, filtersJSON, boolInt(input.Enabled), s.timestamp(), id)
	if err != nil {
		return Definition{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return Definition{}, sql.ErrNoRows
	}
	return s.GetDefinition(ctx, id)
}

func (s *Service) ArchiveDefinition(ctx context.Context, id string) (Definition, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Definition{}, ErrInvalidDefinition
	}
	now := s.timestamp()
	result, err := s.db.ExecContext(ctx, `UPDATE operations_tasks SET enabled=0,archived_at=CASE WHEN archived_at='' THEN ? ELSE archived_at END,updated_at=? WHERE id=?`, now, now, id)
	if err != nil {
		return Definition{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return Definition{}, sql.ErrNoRows
	}
	return s.GetDefinition(ctx, id)
}

func (s *Service) PlayerProgress(ctx context.Context, playerUID string) ([]Progress, error) {
	playerUID = strings.TrimSpace(playerUID)
	if playerUID == "" {
		return nil, economy.ErrInvalidPlayerUID
	}
	definitions, err := s.activeDefinitions(ctx, "")
	if err != nil {
		return nil, err
	}
	items := make([]Progress, 0, len(definitions))
	for _, definition := range definitions {
		cycleKey := s.cycleKey(definition.Cycle, s.now())
		item := Progress{
			TaskID: definition.ID, TaskName: definition.Name, Description: definition.Description,
			EventType: definition.EventType, Cycle: definition.Cycle, CycleKey: cycleKey,
			TargetAmount: definition.TargetAmount, RewardPoints: definition.RewardPoints,
			RewardStatus: "none", Filters: definition.Filters, AmountField: definition.AmountField,
		}
		var completed int
		err := s.db.QueryRowContext(ctx, `SELECT progress,completed,completed_at,updated_at FROM operations_task_progress WHERE task_id=? AND player_uid=? AND cycle_key=?`, definition.ID, playerUID, cycleKey).Scan(&item.Progress, &completed, &item.CompletedAt, &item.UpdatedAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		item.Completed = completed != 0
		if definition.RewardPoints > 0 {
			var status, lastError string
			err = s.db.QueryRowContext(ctx, `SELECT status,last_error FROM operations_task_rewards WHERE task_id=? AND player_uid=? AND cycle_key=?`, definition.ID, playerUID, cycleKey).Scan(&status, &lastError)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			if err == nil {
				item.RewardStatus = status
				item.RewardError = lastError
				item.RewardGranted = status == "granted"
			}
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) ProcessEvent(ctx context.Context, event Event, adjuster interface {
	Adjust(context.Context, economy.Adjustment) (economy.AdjustmentResult, error)
}) ([]ProgressUpdate, error) {
	event = normalizeEvent(event)
	if event.EventID == "" || event.Type == "" {
		return nil, ErrInvalidDefinition
	}
	if event.PlayerUID == "" {
		return []ProgressUpdate{}, nil
	}
	definitions, err := s.activeDefinitions(ctx, event.Type)
	if err != nil {
		return nil, err
	}
	occurredAt := s.eventTime(event.OccurredAt)
	updates := make([]ProgressUpdate, 0)
	for _, definition := range definitions {
		if !matchesFilters(event.Payload, definition.Filters) {
			continue
		}
		amount, valid := eventAmount(event.Payload, definition.AmountField)
		if !valid || amount <= 0 {
			continue
		}
		applied, err := s.applyEvent(ctx, definition, event, occurredAt, amount)
		if err != nil {
			return updates, err
		}
		if applied.Reward != nil && applied.Reward.Status != "granted" {
			if adjuster == nil {
				return updates, fmt.Errorf("%w: point adjuster is unavailable", ErrRewardDelivery)
			}
			if err := s.deliverReward(ctx, adjuster, applied.Reward); err != nil {
				applied.Update.RewardStatus = "failed"
				updates = append(updates, applied.Update)
				return updates, err
			}
			applied.Update.RewardStatus = "granted"
			applied.Update.RewardGranted = true
		}
		updates = append(updates, applied.Update)
	}
	return updates, nil
}

func (s *Service) RetryPendingRewards(ctx context.Context, adjuster interface {
	Adjust(context.Context, economy.Adjustment) (economy.AdjustmentResult, error)
}, limit int) (RetrySummary, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.task_id,t.name,r.player_uid,r.nickname,r.steam_id,r.cycle_key,r.points,r.status,r.ledger_reference_id,r.attempts,r.last_error FROM operations_task_rewards r JOIN operations_tasks t ON t.id=r.task_id WHERE r.status IN ('pending','failed') ORDER BY r.updated_at ASC LIMIT ?`, limit)
	if err != nil {
		return RetrySummary{}, err
	}
	rewards := make([]rewardRecord, 0)
	for rows.Next() {
		var reward rewardRecord
		if err := rows.Scan(&reward.TaskID, &reward.TaskName, &reward.PlayerUID, &reward.Nickname, &reward.SteamID, &reward.CycleKey, &reward.Points, &reward.Status, &reward.LedgerReferenceID, &reward.Attempts, &reward.LastError); err != nil {
			_ = rows.Close()
			return RetrySummary{}, err
		}
		rewards = append(rewards, reward)
	}
	if err := rows.Close(); err != nil {
		return RetrySummary{}, err
	}
	result := RetrySummary{Selected: len(rewards)}
	for index := range rewards {
		if adjuster == nil {
			result.Failed++
			continue
		}
		if err := s.deliverReward(ctx, adjuster, &rewards[index]); err != nil {
			result.Failed++
			continue
		}
		result.Granted++
	}
	return result, nil
}

func (s *Service) activeDefinitions(ctx context.Context, eventType string) ([]Definition, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,description,event_type,target_amount,reward_points,cycle,amount_field,filters_json,enabled,created_at,updated_at,archived_at FROM operations_tasks WHERE enabled=1 AND archived_at='' AND (?='' OR event_type=?) ORDER BY created_at ASC`, eventType, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Definition, 0)
	for rows.Next() {
		definition, err := scanDefinition(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, definition)
	}
	return items, rows.Err()
}

func (s *Service) applyEvent(ctx context.Context, definition Definition, event Event, occurredAt time.Time, amount int64) (applyResult, error) {
	cycleKey := s.cycleKey(definition.Cycle, occurredAt)
	update := ProgressUpdate{
		TaskID: definition.ID, TaskName: definition.Name, CycleKey: cycleKey,
		TargetAmount: definition.TargetAmount, RewardPoints: definition.RewardPoints,
		RewardStatus: "none",
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return applyResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	now := s.timestamp()
	if _, err := tx.ExecContext(ctx, `INSERT INTO operations_task_progress(task_id,player_uid,cycle_key,progress,completed,completed_at,updated_at) VALUES(?,?,?,0,0,'',?) ON CONFLICT(task_id,player_uid,cycle_key) DO NOTHING`, definition.ID, event.PlayerUID, cycleKey, now); err != nil {
		return applyResult{}, err
	}
	var progress int64
	var completed int
	var completedAt string
	if err := tx.QueryRowContext(ctx, `SELECT progress,completed,completed_at FROM operations_task_progress WHERE task_id=? AND player_uid=? AND cycle_key=?`, definition.ID, event.PlayerUID, cycleKey).Scan(&progress, &completed, &completedAt); err != nil {
		return applyResult{}, err
	}
	var receiptExists int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operations_task_event_receipts WHERE task_id=? AND player_uid=? AND cycle_key=? AND event_id=?)`, definition.ID, event.PlayerUID, cycleKey, event.EventID).Scan(&receiptExists); err != nil {
		return applyResult{}, err
	}
	update.Progress = progress
	update.Completed = completed != 0
	update.Duplicate = receiptExists != 0
	if completed == 0 && receiptExists == 0 {
		gain := amount
		if gain > definition.TargetAmount-progress {
			gain = definition.TargetAmount - progress
		}
		if gain < 0 {
			gain = 0
		}
		if gain > 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO operations_task_event_receipts(task_id,player_uid,cycle_key,event_id,amount,created_at) VALUES(?,?,?,?,?,?)`, definition.ID, event.PlayerUID, cycleKey, event.EventID, amount, now); err != nil {
				return applyResult{}, err
			}
			progress += gain
			update.Added = gain
			update.Progress = progress
			if progress >= definition.TargetAmount {
				completed = 1
				completedAt = now
				update.Completed = true
				update.JustCompleted = true
			}
			if _, err := tx.ExecContext(ctx, `UPDATE operations_task_progress SET progress=?,completed=?,completed_at=?,updated_at=? WHERE task_id=? AND player_uid=? AND cycle_key=?`, progress, completed, completedAt, now, definition.ID, event.PlayerUID, cycleKey); err != nil {
				return applyResult{}, err
			}
		}
	}
	if completed != 0 && definition.RewardPoints > 0 {
		referenceID := definition.ID + ":" + cycleKey
		if _, err := tx.ExecContext(ctx, `INSERT INTO operations_task_rewards(task_id,player_uid,cycle_key,nickname,steam_id,points,status,ledger_reference_id,attempts,last_error,created_at,updated_at,granted_at) VALUES(?,?,?,?,?,?,'pending',?,0,'',?,?,'') ON CONFLICT(task_id,player_uid,cycle_key) DO UPDATE SET nickname=CASE WHEN nickname='' THEN excluded.nickname ELSE nickname END,steam_id=CASE WHEN steam_id='' THEN excluded.steam_id ELSE steam_id END`, definition.ID, event.PlayerUID, cycleKey, event.Nickname, event.SteamID, definition.RewardPoints, referenceID, now, now); err != nil {
			return applyResult{}, err
		}
	}
	var pendingReward *rewardRecord
	reward, rewardErr := rewardTx(ctx, tx, definition.ID, definition.Name, event.PlayerUID, cycleKey)
	if rewardErr != nil && !errors.Is(rewardErr, sql.ErrNoRows) {
		return applyResult{}, rewardErr
	}
	if rewardErr == nil {
		update.RewardStatus = reward.Status
		update.RewardGranted = reward.Status == "granted"
		pendingReward = &reward
	}
	if err := tx.Commit(); err != nil {
		return applyResult{}, err
	}
	return applyResult{Update: update, Reward: pendingReward}, nil
}

func (s *Service) deliverReward(ctx context.Context, adjuster interface {
	Adjust(context.Context, economy.Adjustment) (economy.AdjustmentResult, error)
}, reward *rewardRecord) error {
	_, err := adjuster.Adjust(ctx, economy.Adjustment{
		PlayerUID: reward.PlayerUID, Nickname: reward.Nickname, SteamID: reward.SteamID,
		Delta: reward.Points, Reason: "任务奖励：" + reward.TaskName,
		ReferenceType: "task_reward", ReferenceID: reward.LedgerReferenceID,
		Actor:    "task-system",
		Metadata: map[string]any{"task_id": reward.TaskID, "cycle_key": reward.CycleKey},
	})
	now := s.timestamp()
	if err != nil {
		message := truncate(err.Error(), 1000)
		_, updateErr := s.db.ExecContext(ctx, `UPDATE operations_task_rewards SET status='failed',attempts=attempts+1,last_error=?,updated_at=? WHERE task_id=? AND player_uid=? AND cycle_key=?`, message, now, reward.TaskID, reward.PlayerUID, reward.CycleKey)
		if updateErr != nil {
			return fmt.Errorf("%w: grant error: %v; status update error: %v", ErrRewardDelivery, err, updateErr)
		}
		return fmt.Errorf("%w: %v", ErrRewardDelivery, err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE operations_task_rewards SET status='granted',attempts=attempts+1,last_error='',updated_at=?,granted_at=CASE WHEN granted_at='' THEN ? ELSE granted_at END WHERE task_id=? AND player_uid=? AND cycle_key=?`, now, now, reward.TaskID, reward.PlayerUID, reward.CycleKey); err != nil {
		return err
	}
	reward.Status = "granted"
	return nil
}

func rewardTx(ctx context.Context, tx *sql.Tx, taskID, taskName, playerUID, cycleKey string) (rewardRecord, error) {
	var reward rewardRecord
	reward.TaskName = taskName
	err := tx.QueryRowContext(ctx, `SELECT task_id,player_uid,nickname,steam_id,cycle_key,points,status,ledger_reference_id,attempts,last_error FROM operations_task_rewards WHERE task_id=? AND player_uid=? AND cycle_key=?`, taskID, playerUID, cycleKey).Scan(&reward.TaskID, &reward.PlayerUID, &reward.Nickname, &reward.SteamID, &reward.CycleKey, &reward.Points, &reward.Status, &reward.LedgerReferenceID, &reward.Attempts, &reward.LastError)
	return reward, err
}

func scanDefinition(row interface{ Scan(...any) error }) (Definition, error) {
	var definition Definition
	var filtersJSON string
	var enabled int
	if err := row.Scan(&definition.ID, &definition.Name, &definition.Description, &definition.EventType, &definition.TargetAmount, &definition.RewardPoints, &definition.Cycle, &definition.AmountField, &filtersJSON, &enabled, &definition.CreatedAt, &definition.UpdatedAt, &definition.ArchivedAt); err != nil {
		return Definition{}, err
	}
	definition.Enabled = enabled != 0
	definition.Filters = map[string]any{}
	if err := json.Unmarshal([]byte(filtersJSON), &definition.Filters); err != nil {
		return Definition{}, fmt.Errorf("decode task filters: %w", err)
	}
	return definition, nil
}

func normalizeDefinition(input DefinitionInput) (DefinitionInput, string, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.EventType = strings.ToUpper(strings.TrimSpace(input.EventType))
	input.Cycle = strings.ToLower(strings.TrimSpace(input.Cycle))
	input.AmountField = strings.TrimSpace(input.AmountField)
	if input.Filters == nil {
		input.Filters = map[string]any{}
	}
	if input.Name == "" || len([]rune(input.Name)) > 120 || len([]rune(input.Description)) > 1000 || !eventTypePattern.MatchString(input.EventType) {
		return DefinitionInput{}, "", ErrInvalidDefinition
	}
	if input.TargetAmount <= 0 || input.TargetAmount > maximumTaskValue || input.RewardPoints < 0 || input.RewardPoints > maximumTaskValue {
		return DefinitionInput{}, "", ErrInvalidDefinition
	}
	switch input.Cycle {
	case "once", "daily", "weekly":
	default:
		return DefinitionInput{}, "", ErrInvalidDefinition
	}
	if input.AmountField != "" && !fieldPathPattern.MatchString(input.AmountField) {
		return DefinitionInput{}, "", ErrInvalidDefinition
	}
	if len(input.Filters) > 16 {
		return DefinitionInput{}, "", ErrInvalidDefinition
	}
	for key, value := range input.Filters {
		if !fieldPathPattern.MatchString(strings.TrimSpace(key)) || !isScalar(value) {
			return DefinitionInput{}, "", ErrInvalidDefinition
		}
	}
	filtersJSON, err := json.Marshal(input.Filters)
	if err != nil {
		return DefinitionInput{}, "", ErrInvalidDefinition
	}
	return input, string(filtersJSON), nil
}

func normalizeEvent(event Event) Event {
	event.EventID = strings.TrimSpace(event.EventID)
	event.Type = strings.ToUpper(strings.TrimSpace(event.Type))
	event.PlayerUID = strings.TrimSpace(event.PlayerUID)
	event.Nickname = strings.TrimSpace(event.Nickname)
	event.SteamID = strings.TrimSpace(event.SteamID)
	event.OccurredAt = strings.TrimSpace(event.OccurredAt)
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	return event
}

func matchesFilters(payload map[string]any, filters map[string]any) bool {
	for path, expected := range filters {
		actual, ok := valueAt(payload, path)
		if !ok || scalarString(actual) != scalarString(expected) {
			return false
		}
	}
	return true
}

func eventAmount(payload map[string]any, field string) (int64, bool) {
	if strings.TrimSpace(field) == "" {
		return 1, true
	}
	value, ok := valueAt(payload, field)
	if !ok {
		return 0, false
	}
	amount, ok := positiveInt64(value)
	if !ok || amount > maximumEventGain {
		return 0, false
	}
	return amount, true
}

func valueAt(payload map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	var current any = payload
	for _, part := range parts {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func positiveInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), typed > 0
	case int8:
		return int64(typed), typed > 0
	case int16:
		return int64(typed), typed > 0
	case int32:
		return int64(typed), typed > 0
	case int64:
		return typed, typed > 0
	case uint:
		if uint64(typed) > uint64(^uint64(0)>>1) {
			return 0, false
		}
		return int64(typed), typed > 0
	case uint8:
		return int64(typed), typed > 0
	case uint16:
		return int64(typed), typed > 0
	case uint32:
		return int64(typed), typed > 0
	case uint64:
		if typed > uint64(^uint64(0)>>1) {
			return 0, false
		}
		return int64(typed), typed > 0
	case float64:
		if typed <= 0 || typed != float64(int64(typed)) {
			return 0, false
		}
		return int64(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil && parsed > 0
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed, err == nil && parsed > 0
	default:
		return 0, false
	}
}

func isScalar(value any) bool {
	switch value.(type) {
	case nil, string, bool, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number:
		return true
	default:
		return false
	}
}

func scalarString(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(body)
}

func (s *Service) cycleKey(cycle string, occurredAt time.Time) string {
	local := occurredAt.In(s.location)
	switch cycle {
	case "once":
		return "once"
	case "weekly":
		weekday := int(local.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		monday := local.AddDate(0, 0, -(weekday - 1))
		return monday.Format("2006-01-02")
	default:
		return local.Format("2006-01-02")
	}
}

func (s *Service) eventTime(value string) time.Time {
	if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value)); err == nil {
		return parsed
	}
	return s.now()
}

func loadLocation(value string) (*time.Location, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultTimezone
	}
	location, err := time.LoadLocation(value)
	if err != nil {
		return nil, fmt.Errorf("load task timezone %q: %w", value, err)
	}
	return location, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func truncate(value string, maximum int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maximum {
		return string(runes)
	}
	return string(runes[:maximum])
}

func (s *Service) timestamp() string {
	return s.now().UTC().Format(time.RFC3339Nano)
}

func newID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}
