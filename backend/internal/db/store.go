package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db            *sql.DB
	incidentMu    sync.Mutex
	incidentJobMu sync.Mutex
}

type Job struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Progress  int    `json:"progress"`
	Message   string `json:"message"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Mod struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Source        string   `json:"source"`
	PackageName   string   `json:"package_name"`
	Path          string   `json:"path"`
	Version       string   `json:"version,omitempty"`
	Enabled       bool     `json:"enabled"`
	WorkshopID    string   `json:"workshop_id,omitempty"`
	PreviewURL    string   `json:"preview_url,omitempty"`
	SteamURL      string   `json:"steam_url,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	FileSize      int64    `json:"file_size,omitempty"`
	Subscriptions int64    `json:"subscriptions,omitempty"`
	TimeUpdated   int64    `json:"time_updated,omitempty"`
	LastCheckedAt string   `json:"last_checked_at,omitempty"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

type AuditLog struct {
	ID        string `json:"id"`
	Actor     string `json:"actor"`
	Role      string `json:"role"`
	Action    string `json:"action"`
	Target    string `json:"target,omitempty"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	IP        string `json:"ip,omitempty"`
	CreatedAt string `json:"created_at"`
}

type PlayerAccessEntry struct {
	SteamID   string `json:"steam_id"`
	Nickname  string `json:"nickname,omitempty"`
	Reason    string `json:"reason,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type MonitorSample struct {
	ID                       string              `json:"id"`
	CreatedAt                string              `json:"created_at"`
	CPUAvailable             bool                `json:"cpu_available"`
	CPUPercent               float64             `json:"cpu_percent"`
	MemoryAvailable          bool                `json:"memory_available"`
	MemoryUsageBytes         int64               `json:"memory_usage_bytes"`
	MemoryLimitBytes         int64               `json:"memory_limit_bytes"`
	HostMemoryAvailable      bool                `json:"host_memory_available"`
	HostMemoryTotalBytes     int64               `json:"host_memory_total_bytes"`
	HostMemoryAvailableBytes int64               `json:"host_memory_available_bytes"`
	HostSwapTotalBytes       int64               `json:"host_swap_total_bytes"`
	HostSwapFreeBytes        int64               `json:"host_swap_free_bytes"`
	WorkloadMemoryAvailable  bool                `json:"workload_memory_available"`
	WorkloadMemoryUsageBytes int64               `json:"workload_memory_usage_bytes"`
	WorkloadMemoryLimitBytes int64               `json:"workload_memory_limit_bytes"`
	OOMKilled                bool                `json:"oom_killed"`
	LifecycleAvailable       bool                `json:"lifecycle_available"`
	ExitCode                 int                 `json:"exit_code"`
	RestartCount             int                 `json:"restart_count"`
	StartedAt                string              `json:"started_at,omitempty"`
	FinishedAt               string              `json:"finished_at,omitempty"`
	RiskReasons              []MonitorRiskReason `json:"risk_reasons"`
	DiskAvailable            bool                `json:"disk_available"`
	DiskFreeBytes            int64               `json:"disk_free_bytes"`
	DiskTotalBytes           int64               `json:"disk_total_bytes"`
	CurrentPlayers           int                 `json:"current_players"`
	MaxPlayers               int                 `json:"max_players"`
	RESTHealthy              bool                `json:"rest_healthy"`
	RCONHealthy              bool                `json:"rcon_healthy"`
	GamePortHealthy          bool                `json:"game_port_healthy"`
	QueryPortHealthy         bool                `json:"query_port_healthy"`
	UnavailableReason        string              `json:"unavailable_reason,omitempty"`
}

type MonitorRiskReason struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

type MonitorAlertState struct {
	Code           string `json:"code"`
	UnhealthyCount int    `json:"unhealthy_count"`
	HealthyCount   int    `json:"healthy_count"`
	Open           bool   `json:"open"`
	AlertID        string `json:"alert_id,omitempty"`
	UpdatedAt      string `json:"updated_at"`
}

type Schedule struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `json:"interval_minutes,omitempty"`
	TimeOfDay       string `json:"time_of_day,omitempty"`
	Timezone        string `json:"timezone,omitempty"`
	WaitTime        int    `json:"waittime,omitempty"`
	Message         string `json:"message,omitempty"`
	LastRunAt       string `json:"last_run_at,omitempty"`
	NextRunAt       string `json:"next_run_at,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type Alert struct {
	ID        string `json:"id"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	Source    string `json:"source"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	AckAt     string `json:"ack_at,omitempty"`
}

type AITranslation struct {
	WorkshopID     string `json:"workshop_id"`
	SourceSHA256   string `json:"source_sha256"`
	TargetLanguage string `json:"target_language"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	Translation    string `json:"translation"`
	CreatedAt      string `json:"created_at"`
}

type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
	Disabled     bool   `json:"disabled"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type Session struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	TokenHash  string `json:"-"`
	ExpiresAt  string `json:"expires_at"`
	LastSeenAt string `json:"last_seen_at"`
	CreatedAt  string `json:"created_at"`
}

type APIKey struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	TokenHash  string `json:"-"`
	LastUsedAt string `json:"last_used_at,omitempty"`
	RevokedAt  string `json:"revoked_at,omitempty"`
	CreatedAt  string `json:"created_at"`
}

type SaveSource struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	Path          string   `json:"path,omitempty"`
	Active        bool     `json:"active"`
	Fingerprint   string   `json:"fingerprint,omitempty"`
	ParserVersion string   `json:"parser_version,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
	IndexedAt     string   `json:"indexed_at,omitempty"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

type BreedingResult struct {
	ID          string `json:"id"`
	JobID       string `json:"job_id"`
	Subject     string `json:"subject"`
	SourceID    string `json:"source_id"`
	Fingerprint string `json:"fingerprint"`
	RequestJSON string `json:"request_json"`
	ResultJSON  string `json:"result_json"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type BreedingPreset struct {
	ID         string `json:"id"`
	Subject    string `json:"subject"`
	Name       string `json:"name"`
	ConfigJSON string `json:"config_json"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type CustomPalContainer struct {
	ID        string `json:"id"`
	Subject   string `json:"subject"`
	Name      string `json:"name"`
	PalsJSON  string `json:"pals_json"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type BreedSession struct {
	ID        string `json:"id"`
	Subject   string `json:"subject"`
	TokenHash string `json:"-"`
	PlayerUID string `json:"player_uid"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
}

var ErrAlreadyInitialized = errors.New("panel is already initialized")

func Open(path string) (*Store, error) {
	d, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1)
	if err := configureSQLite(d); err != nil {
		_ = d.Close()
		return nil, err
	}
	s := &Store{db: d}
	if err := s.Migrate(context.Background()); err != nil {
		_ = d.Close()
		return nil, err
	}
	return s, nil
}

func configureSQLite(d *sql.DB) error {
	for _, stmt := range []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			return fmt.Errorf("configure sqlite: %w", err)
		}
	}
	return nil
}

func (s *Store) Close() error {
	// Checkpoint WAL to release file locks on Windows before closing.
	if _, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		// ignore: best-effort cleanup
	}
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version)
	return version, err
}

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema migrations table: %w", err)
	}
	for _, migration := range migrations() {
		var applied int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=?`, migration.version).Scan(&applied); err != nil {
			return fmt.Errorf("read schema migration %d: %w", migration.version, err)
		}
		if applied > 0 {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin schema migration %d: %w", migration.version, err)
		}
		if err := migration.apply(ctx, tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply schema migration %d: %w", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, migration.version, now()); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record schema migration %d: %w", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit schema migration %d: %w", migration.version, err)
		}
	}
	return nil
}

type schemaMigration struct {
	version int
	apply   func(context.Context, *sql.Tx) error
}

func migrations() []schemaMigration {
	return []schemaMigration{
		{version: 1, apply: migrateBaseline},
		{version: 2, apply: migrateOperationalReliability},
		{version: 3, apply: migrateJobErrorCodes},
		{version: 4, apply: migrateAccountAuthentication},
		{version: 5, apply: migrateBreedingAndSaveSources},
		{version: 6, apply: migrateSaveSourceIndexMetadata},
		{version: 7, apply: migrateConfigDrafts},
		{version: 8, apply: migrateConfigDraftSafety},
		{version: 9, apply: migrateMonitorDiagnostics},
		{version: 10, apply: migrateMonitorLifecycleAvailability},
		{version: 11, apply: migrateConfigRevisions},
		{version: 12, apply: migrateCrashGuard},
		{version: 13, apply: migrateIncidents},
	}
}

func migrateMonitorLifecycleAvailability(ctx context.Context, tx *sql.Tx) error {
	if err := ensureColumn(ctx, tx, "monitor_samples", "lifecycle_available", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return execAll(ctx, tx,
		`UPDATE monitor_alert_states SET
			alert_id=COALESCE(
				(SELECT alerts.id FROM alerts WHERE alerts.id=monitor_alert_states.alert_id
					AND alerts.status='open' AND alerts.source='monitor:' || monitor_alert_states.code),
				(SELECT MIN(alerts.id) FROM alerts WHERE alerts.status='open'
					AND alerts.source='monitor:' || monitor_alert_states.code),
				''
			),
			open=CASE WHEN EXISTS (
				SELECT 1 FROM alerts WHERE alerts.status='open' AND alerts.source='monitor:' || monitor_alert_states.code
			) THEN 1 ELSE 0 END,
			unhealthy_count=CASE WHEN EXISTS (
				SELECT 1 FROM alerts WHERE alerts.status='open' AND alerts.source='monitor:' || monitor_alert_states.code
			) THEN unhealthy_count ELSE 0 END,
			healthy_count=CASE WHEN EXISTS (
				SELECT 1 FROM alerts WHERE alerts.status='open' AND alerts.source='monitor:' || monitor_alert_states.code
			) THEN healthy_count ELSE 0 END
			WHERE open=1`,
		`INSERT INTO monitor_alert_states(code,unhealthy_count,healthy_count,open,alert_id,updated_at)
			SELECT SUBSTR(alerts.source,9),3,0,1,MIN(alerts.id),CURRENT_TIMESTAMP
			FROM alerts
			WHERE alerts.status='open' AND alerts.source LIKE 'monitor:%'
				AND NOT EXISTS (
					SELECT 1 FROM monitor_alert_states state WHERE 'monitor:' || state.code=alerts.source
				)
			GROUP BY alerts.source`,
		`UPDATE monitor_alert_states SET
			open=1,
			alert_id=(SELECT MIN(alerts.id) FROM alerts WHERE alerts.status='open'
				AND alerts.source='monitor:' || monitor_alert_states.code),
			unhealthy_count=CASE WHEN unhealthy_count < 3 THEN 3 ELSE unhealthy_count END,
			healthy_count=0
			WHERE open=0 AND EXISTS (
				SELECT 1 FROM alerts WHERE alerts.status='open' AND alerts.source='monitor:' || monitor_alert_states.code
			)`,
		`UPDATE alerts SET status='resolved',ack_at=CASE WHEN ack_at='' THEN CURRENT_TIMESTAMP ELSE ack_at END
			WHERE status='open' AND source LIKE 'monitor:%' AND NOT EXISTS (
				SELECT 1 FROM monitor_alert_states state
				WHERE state.open=1 AND state.alert_id=alerts.id AND 'monitor:' || state.code=alerts.source
			)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_alerts_open_monitor_source ON alerts(source) WHERE status='open' AND source LIKE 'monitor:%'`,
	)
}

func migrateMonitorDiagnostics(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []struct{ name, definition string }{
		{"host_memory_available", "INTEGER NOT NULL DEFAULT 0"},
		{"host_memory_total_bytes", "INTEGER NOT NULL DEFAULT 0"},
		{"host_memory_available_bytes", "INTEGER NOT NULL DEFAULT 0"},
		{"host_swap_total_bytes", "INTEGER NOT NULL DEFAULT 0"},
		{"host_swap_free_bytes", "INTEGER NOT NULL DEFAULT 0"},
		{"workload_memory_available", "INTEGER NOT NULL DEFAULT 0"},
		{"workload_memory_usage_bytes", "INTEGER NOT NULL DEFAULT 0"},
		{"workload_memory_limit_bytes", "INTEGER NOT NULL DEFAULT 0"},
		{"oom_killed", "INTEGER NOT NULL DEFAULT 0"},
		{"exit_code", "INTEGER NOT NULL DEFAULT 0"},
		{"restart_count", "INTEGER NOT NULL DEFAULT 0"},
		{"started_at", "TEXT NOT NULL DEFAULT ''"},
		{"finished_at", "TEXT NOT NULL DEFAULT ''"},
		{"risk_reasons_json", "TEXT NOT NULL DEFAULT '[]'"},
	} {
		if err := ensureColumn(ctx, tx, "monitor_samples", column.name, column.definition); err != nil {
			return err
		}
	}
	return execAll(ctx, tx, `CREATE TABLE IF NOT EXISTS monitor_alert_states (
		code TEXT PRIMARY KEY,
		unhealthy_count INTEGER NOT NULL DEFAULT 0,
		healthy_count INTEGER NOT NULL DEFAULT 0,
		open INTEGER NOT NULL DEFAULT 0,
		alert_id TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL
	)`)
}

func migrateSaveSourceIndexMetadata(ctx context.Context, tx *sql.Tx) error {
	return execAll(ctx, tx,
		`ALTER TABLE save_sources ADD COLUMN fingerprint TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE save_sources ADD COLUMN parser_version TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE save_sources ADD COLUMN warnings_json TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE save_sources ADD COLUMN indexed_at TEXT NOT NULL DEFAULT ''`,
	)
}

func migrateBreedingAndSaveSources(ctx context.Context, tx *sql.Tx) error {
	return execAll(ctx, tx,
		`CREATE TABLE IF NOT EXISTS save_sources (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			path TEXT NOT NULL DEFAULT '',
			active INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_save_sources_active ON save_sources(active) WHERE active=1`,
		`CREATE TABLE IF NOT EXISTS breeding_results (
			id TEXT PRIMARY KEY,
			job_id TEXT NOT NULL UNIQUE,
			subject TEXT NOT NULL,
			source_id TEXT NOT NULL,
			fingerprint TEXT NOT NULL,
			request_json TEXT NOT NULL,
			result_json TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_breeding_results_subject ON breeding_results(subject,created_at)`,
		`CREATE TABLE IF NOT EXISTS breeding_presets (
			id TEXT PRIMARY KEY,
			subject TEXT NOT NULL,
			name TEXT NOT NULL,
			config_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(subject,name)
		)`,
		`CREATE TABLE IF NOT EXISTS custom_pal_containers (
			id TEXT PRIMARY KEY,
			subject TEXT NOT NULL,
			name TEXT NOT NULL,
			pals_json TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS breed_sessions (
			id TEXT PRIMARY KEY,
			subject TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			player_uid TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
	)
}

func migrateBaseline(ctx context.Context, tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			status TEXT NOT NULL,
			progress INTEGER NOT NULL DEFAULT 0,
			message TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			error_code TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS mods (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			source TEXT NOT NULL,
			package_name TEXT NOT NULL,
			path TEXT NOT NULL,
			version TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 0,
			workshop_id TEXT NOT NULL DEFAULT '',
			preview_url TEXT NOT NULL DEFAULT '',
			steam_url TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			tags_json TEXT NOT NULL DEFAULT '[]',
			file_size INTEGER NOT NULL DEFAULT 0,
			subscriptions INTEGER NOT NULL DEFAULT 0,
			time_updated INTEGER NOT NULL DEFAULT 0,
			last_checked_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS kv (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id TEXT PRIMARY KEY,
			actor TEXT NOT NULL,
			role TEXT NOT NULL,
			action TEXT NOT NULL,
			target TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT '',
			ip TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS player_access (
			list_type TEXT NOT NULL,
			steam_id TEXT NOT NULL,
			nickname TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (list_type, steam_id)
		)`,
		`CREATE TABLE IF NOT EXISTS monitor_samples (
			id TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			cpu_available INTEGER NOT NULL DEFAULT 0,
			cpu_percent REAL NOT NULL DEFAULT 0,
			memory_available INTEGER NOT NULL DEFAULT 0,
			memory_usage_bytes INTEGER NOT NULL DEFAULT 0,
			memory_limit_bytes INTEGER NOT NULL DEFAULT 0,
			disk_available INTEGER NOT NULL DEFAULT 0,
			disk_free_bytes INTEGER NOT NULL DEFAULT 0,
			disk_total_bytes INTEGER NOT NULL DEFAULT 0,
			current_players INTEGER NOT NULL DEFAULT 0,
			max_players INTEGER NOT NULL DEFAULT 0,
			rest_healthy INTEGER NOT NULL DEFAULT 0,
			rcon_healthy INTEGER NOT NULL DEFAULT 0,
			game_port_healthy INTEGER NOT NULL DEFAULT 0,
			query_port_healthy INTEGER NOT NULL DEFAULT 0,
			unavailable_reason TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS schedules (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			interval_minutes INTEGER NOT NULL DEFAULT 0,
			time_of_day TEXT NOT NULL DEFAULT '',
			waittime INTEGER NOT NULL DEFAULT 30,
			message TEXT NOT NULL DEFAULT '',
			last_run_at TEXT NOT NULL DEFAULT '',
			next_run_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS alerts (
			id TEXT PRIMARY KEY,
			severity TEXT NOT NULL,
			title TEXT NOT NULL,
			message TEXT NOT NULL,
			source TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			ack_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS ai_translations (
			workshop_id TEXT NOT NULL,
			source_sha256 TEXT NOT NULL,
			target_language TEXT NOT NULL,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			translation TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (workshop_id, source_sha256, target_language, provider, model)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	for _, column := range []struct{ name, definition string }{
		{"workshop_id", "TEXT NOT NULL DEFAULT ''"},
		{"preview_url", "TEXT NOT NULL DEFAULT ''"},
		{"steam_url", "TEXT NOT NULL DEFAULT ''"},
		{"summary", "TEXT NOT NULL DEFAULT ''"},
		{"tags_json", "TEXT NOT NULL DEFAULT '[]'"},
		{"file_size", "INTEGER NOT NULL DEFAULT 0"},
		{"subscriptions", "INTEGER NOT NULL DEFAULT 0"},
		{"time_updated", "INTEGER NOT NULL DEFAULT 0"},
		{"last_checked_at", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := ensureColumn(ctx, tx, "mods", column.name, column.definition); err != nil {
			return err
		}
	}
	return execAll(ctx, tx,
		`CREATE INDEX IF NOT EXISTS idx_mods_workshop_id ON mods(workshop_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_translations_workshop_id ON ai_translations(workshop_id)`,
	)
}

func migrateOperationalReliability(ctx context.Context, tx *sql.Tx) error {
	if err := ensureColumn(ctx, tx, "schedules", "timezone", "TEXT NOT NULL DEFAULT 'UTC'"); err != nil {
		return err
	}
	return execAll(ctx, tx, `CREATE INDEX IF NOT EXISTS idx_monitor_samples_created_at ON monitor_samples(created_at)`)
}

func migrateJobErrorCodes(ctx context.Context, tx *sql.Tx) error {
	return ensureColumn(ctx, tx, "jobs", "error_code", "TEXT NOT NULL DEFAULT ''")
}

func migrateAccountAuthentication(ctx context.Context, tx *sql.Tx) error {
	return execAll(ctx, tx,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE COLLATE NOCASE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL,
			disabled INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token_hash TEXT NOT NULL UNIQUE,
			expires_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			prefix TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			last_used_at TEXT NOT NULL DEFAULT '',
			revoked_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_token_hash ON api_keys(token_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id)`,
	)
}

func execAll(ctx context.Context, tx *sql.Tx, statements ...string) error {
	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func ensureColumn(ctx context.Context, tx *sql.Tx, table, column, definition string) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+definition)
	return err
}

func (s *Store) CreateAuditLog(ctx context.Context, log AuditLog) error {
	if log.ID == "" {
		return errors.New("audit log id is required")
	}
	if log.CreatedAt == "" {
		log.CreatedAt = now()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_logs (id,actor,role,action,target,status,message,ip,created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		log.ID, log.Actor, log.Role, log.Action, log.Target, log.Status, log.Message, log.IP, log.CreatedAt)
	return err
}

func (s *Store) ListAuditLogs(ctx context.Context, limit int) ([]AuditLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,actor,role,action,target,status,message,ip,created_at FROM audit_logs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditLog
	for rows.Next() {
		var item AuditLog
		if err := rows.Scan(&item.ID, &item.Actor, &item.Role, &item.Action, &item.Target, &item.Status, &item.Message, &item.IP, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListPlayerAccess(ctx context.Context, listType string) ([]PlayerAccessEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT steam_id,nickname,reason,created_at,updated_at FROM player_access WHERE list_type=? ORDER BY updated_at DESC`, listType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlayerAccessEntry
	for rows.Next() {
		var item PlayerAccessEntry
		if err := rows.Scan(&item.SteamID, &item.Nickname, &item.Reason, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpsertPlayerAccess(ctx context.Context, listType string, item PlayerAccessEntry) error {
	if item.SteamID == "" {
		return errors.New("steam_id is required")
	}
	now := now()
	if item.CreatedAt == "" {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO player_access (list_type,steam_id,nickname,reason,created_at,updated_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(list_type, steam_id) DO UPDATE SET nickname=excluded.nickname, reason=excluded.reason, updated_at=excluded.updated_at`,
		listType, item.SteamID, item.Nickname, item.Reason, item.CreatedAt, item.UpdatedAt)
	return err
}

func (s *Store) DeletePlayerAccess(ctx context.Context, listType, steamID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM player_access WHERE list_type=? AND steam_id=?`, listType, steamID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ReplacePlayerAccess(ctx context.Context, listType string, items []PlayerAccessEntry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM player_access WHERE list_type=?`, listType); err != nil {
		_ = tx.Rollback()
		return err
	}
	now := now()
	for _, item := range items {
		if item.SteamID == "" {
			_ = tx.Rollback()
			return errors.New("steam_id is required")
		}
		if item.CreatedAt == "" {
			item.CreatedAt = now
		}
		item.UpdatedAt = now
		if _, err := tx.ExecContext(ctx, `INSERT INTO player_access (list_type,steam_id,nickname,reason,created_at,updated_at) VALUES (?,?,?,?,?,?)`,
			listType, item.SteamID, item.Nickname, item.Reason, item.CreatedAt, item.UpdatedAt); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) InsertMonitorSample(ctx context.Context, sample MonitorSample) error {
	if sample.ID == "" {
		return errors.New("monitor sample id is required")
	}
	if sample.CreatedAt == "" {
		sample.CreatedAt = now()
	}
	riskReasons, err := json.Marshal(sample.RiskReasons)
	if err != nil {
		return fmt.Errorf("encode monitor risk reasons: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO monitor_samples (
		id,created_at,cpu_available,cpu_percent,memory_available,memory_usage_bytes,memory_limit_bytes,
		disk_available,disk_free_bytes,disk_total_bytes,current_players,max_players,rest_healthy,rcon_healthy,
		game_port_healthy,query_port_healthy,unavailable_reason,
		host_memory_available,host_memory_total_bytes,host_memory_available_bytes,host_swap_total_bytes,host_swap_free_bytes,
		workload_memory_available,workload_memory_usage_bytes,workload_memory_limit_bytes,oom_killed,exit_code,restart_count,
		started_at,finished_at,risk_reasons_json,lifecycle_available
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sample.ID, sample.CreatedAt, boolInt(sample.CPUAvailable), sample.CPUPercent,
		boolInt(sample.MemoryAvailable), sample.MemoryUsageBytes, sample.MemoryLimitBytes,
		boolInt(sample.DiskAvailable), sample.DiskFreeBytes, sample.DiskTotalBytes,
		sample.CurrentPlayers, sample.MaxPlayers, boolInt(sample.RESTHealthy), boolInt(sample.RCONHealthy),
		boolInt(sample.GamePortHealthy), boolInt(sample.QueryPortHealthy), sample.UnavailableReason,
		boolInt(sample.HostMemoryAvailable), sample.HostMemoryTotalBytes, sample.HostMemoryAvailableBytes,
		sample.HostSwapTotalBytes, sample.HostSwapFreeBytes, boolInt(sample.WorkloadMemoryAvailable),
		sample.WorkloadMemoryUsageBytes, sample.WorkloadMemoryLimitBytes, boolInt(sample.OOMKilled), sample.ExitCode,
		sample.RestartCount, sample.StartedAt, sample.FinishedAt, string(riskReasons), boolInt(sample.LifecycleAvailable))
	return err
}

func (s *Store) ListMonitorSamples(ctx context.Context, limit int) ([]MonitorSample, error) {
	if limit <= 0 || limit > 1000 {
		limit = 120
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,created_at,cpu_available,cpu_percent,memory_available,memory_usage_bytes,memory_limit_bytes,
		disk_available,disk_free_bytes,disk_total_bytes,current_players,max_players,rest_healthy,rcon_healthy,
		game_port_healthy,query_port_healthy,unavailable_reason,
		host_memory_available,host_memory_total_bytes,host_memory_available_bytes,host_swap_total_bytes,host_swap_free_bytes,
		workload_memory_available,workload_memory_usage_bytes,workload_memory_limit_bytes,oom_killed,exit_code,restart_count,
		started_at,finished_at,risk_reasons_json,lifecycle_available
		FROM monitor_samples ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MonitorSample
	for rows.Next() {
		var item MonitorSample
		var cpuAvailable, memoryAvailable, diskAvailable, restHealthy, rconHealthy, gameHealthy, queryHealthy int
		var hostMemoryAvailable, workloadMemoryAvailable, oomKilled, lifecycleAvailable int
		var riskReasonsJSON string
		if err := rows.Scan(&item.ID, &item.CreatedAt, &cpuAvailable, &item.CPUPercent, &memoryAvailable,
			&item.MemoryUsageBytes, &item.MemoryLimitBytes, &diskAvailable, &item.DiskFreeBytes,
			&item.DiskTotalBytes, &item.CurrentPlayers, &item.MaxPlayers, &restHealthy, &rconHealthy,
			&gameHealthy, &queryHealthy, &item.UnavailableReason,
			&hostMemoryAvailable, &item.HostMemoryTotalBytes, &item.HostMemoryAvailableBytes,
			&item.HostSwapTotalBytes, &item.HostSwapFreeBytes, &workloadMemoryAvailable,
			&item.WorkloadMemoryUsageBytes, &item.WorkloadMemoryLimitBytes, &oomKilled, &item.ExitCode,
			&item.RestartCount, &item.StartedAt, &item.FinishedAt, &riskReasonsJSON, &lifecycleAvailable); err != nil {
			return nil, err
		}
		item.CPUAvailable = cpuAvailable == 1
		item.MemoryAvailable = memoryAvailable == 1
		item.DiskAvailable = diskAvailable == 1
		item.RESTHealthy = restHealthy == 1
		item.RCONHealthy = rconHealthy == 1
		item.GamePortHealthy = gameHealthy == 1
		item.QueryPortHealthy = queryHealthy == 1
		item.HostMemoryAvailable = hostMemoryAvailable == 1
		item.WorkloadMemoryAvailable = workloadMemoryAvailable == 1
		item.OOMKilled = oomKilled == 1
		item.LifecycleAvailable = lifecycleAvailable == 1
		if err := json.Unmarshal([]byte(riskReasonsJSON), &item.RiskReasons); err != nil {
			return nil, fmt.Errorf("decode monitor risk reasons: %w", err)
		}
		if item.RiskReasons == nil {
			item.RiskReasons = []MonitorRiskReason{}
		}
		out = append(out, item)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

func (s *Store) DeleteMonitorSamplesBefore(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if batchSize <= 0 || batchSize > 10000 {
		batchSize = 1000
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM monitor_samples WHERE id IN (
		SELECT id FROM monitor_samples WHERE created_at < ? ORDER BY created_at LIMIT ?
	)`, cutoff.UTC().Format(time.RFC3339Nano), batchSize)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) UpsertMonitorAlertState(ctx context.Context, state MonitorAlertState) error {
	if strings.TrimSpace(state.Code) == "" {
		return errors.New("monitor alert state code is required")
	}
	state.UpdatedAt = now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO monitor_alert_states(code,unhealthy_count,healthy_count,open,alert_id,updated_at)
		VALUES (?,?,?,?,?,?) ON CONFLICT(code) DO UPDATE SET unhealthy_count=excluded.unhealthy_count,
		healthy_count=excluded.healthy_count,open=excluded.open,alert_id=excluded.alert_id,updated_at=excluded.updated_at`,
		state.Code, state.UnhealthyCount, state.HealthyCount, boolInt(state.Open), state.AlertID, state.UpdatedAt)
	return err
}

func (s *Store) GetMonitorAlertState(ctx context.Context, code string) (MonitorAlertState, bool, error) {
	var state MonitorAlertState
	var open int
	err := s.db.QueryRowContext(ctx, `SELECT code,unhealthy_count,healthy_count,open,alert_id,updated_at FROM monitor_alert_states WHERE code=?`, code).
		Scan(&state.Code, &state.UnhealthyCount, &state.HealthyCount, &open, &state.AlertID, &state.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return MonitorAlertState{}, false, nil
	}
	if err != nil {
		return MonitorAlertState{}, false, err
	}
	state.Open = open == 1
	return state, true, nil
}

func (s *Store) ApplyMonitorAlertSample(ctx context.Context, code string, unhealthy, immediate bool, alert Alert) error {
	if strings.TrimSpace(code) == "" {
		return errors.New("monitor alert state code is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	state := MonitorAlertState{Code: code}
	var open int
	err = tx.QueryRowContext(ctx, `SELECT unhealthy_count,healthy_count,open,alert_id,updated_at FROM monitor_alert_states WHERE code=?`, code).
		Scan(&state.UnhealthyCount, &state.HealthyCount, &open, &state.AlertID, &state.UpdatedAt)
	found := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	state.Open = open == 1

	if unhealthy {
		state.HealthyCount = 0
		if immediate {
			state.UnhealthyCount = 3
		} else if state.UnhealthyCount < 3 {
			state.UnhealthyCount++
		}
		if !state.Open && state.UnhealthyCount >= 3 {
			if strings.TrimSpace(alert.ID) == "" {
				return errors.New("monitor alert id is required when opening a round")
			}
			if alert.CreatedAt == "" {
				alert.CreatedAt = now()
			}
			if alert.Status == "" {
				alert.Status = "open"
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO alerts(id,severity,title,message,source,status,created_at,ack_at) VALUES (?,?,?,?,?,?,?,?)`,
				alert.ID, alert.Severity, alert.Title, alert.Message, alert.Source, alert.Status, alert.CreatedAt, alert.AckAt); err != nil {
				return err
			}
			state.Open = true
			state.AlertID = alert.ID
		} else if state.Open && immediate {
			result, err := tx.ExecContext(ctx, `UPDATE alerts SET severity=?,title=?,message=?,source=? WHERE id=?`,
				alert.Severity, alert.Title, alert.Message, alert.Source, state.AlertID)
			if err != nil {
				return err
			}
			if count, err := result.RowsAffected(); err != nil || count == 0 {
				if err != nil {
					return err
				}
				return sql.ErrNoRows
			}
		}
	} else if found {
		state.UnhealthyCount = 0
		if state.Open {
			state.HealthyCount++
			if state.HealthyCount >= 3 {
				result, err := tx.ExecContext(ctx, `UPDATE alerts SET
					status=CASE WHEN status='open' THEN 'resolved' ELSE status END,
					ack_at=CASE WHEN status='open' THEN ? ELSE ack_at END WHERE id=?`, now(), state.AlertID)
				if err != nil {
					return err
				}
				if count, err := result.RowsAffected(); err != nil || count == 0 {
					if err != nil {
						return err
					}
					return sql.ErrNoRows
				}
				state.Open = false
				state.HealthyCount = 0
				state.AlertID = ""
			}
		} else {
			state.HealthyCount = 0
		}
	}
	if !found && !unhealthy {
		return tx.Commit()
	}
	state.UpdatedAt = now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO monitor_alert_states(code,unhealthy_count,healthy_count,open,alert_id,updated_at)
		VALUES (?,?,?,?,?,?) ON CONFLICT(code) DO UPDATE SET unhealthy_count=excluded.unhealthy_count,
		healthy_count=excluded.healthy_count,open=excluded.open,alert_id=excluded.alert_id,updated_at=excluded.updated_at`,
		state.Code, state.UnhealthyCount, state.HealthyCount, boolInt(state.Open), state.AlertID, state.UpdatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpsertSchedule(ctx context.Context, item Schedule) error {
	if item.ID == "" {
		return errors.New("schedule id is required")
	}
	if item.Type == "" {
		return errors.New("schedule type is required")
	}
	now := now()
	if item.CreatedAt == "" {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO schedules (id,type,enabled,interval_minutes,time_of_day,timezone,waittime,message,last_run_at,next_run_at,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET type=excluded.type, enabled=excluded.enabled, interval_minutes=excluded.interval_minutes,
		time_of_day=excluded.time_of_day, timezone=excluded.timezone, waittime=excluded.waittime, message=excluded.message, last_run_at=excluded.last_run_at,
		next_run_at=excluded.next_run_at, updated_at=excluded.updated_at`,
		item.ID, item.Type, boolInt(item.Enabled), item.IntervalMinutes, item.TimeOfDay, item.Timezone, item.WaitTime,
		item.Message, item.LastRunAt, item.NextRunAt, item.CreatedAt, item.UpdatedAt)
	return err
}

func (s *Store) ListSchedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,type,enabled,interval_minutes,time_of_day,timezone,waittime,message,last_run_at,next_run_at,created_at,updated_at
		FROM schedules ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Schedule
	for rows.Next() {
		var item Schedule
		var enabled int
		if err := rows.Scan(&item.ID, &item.Type, &enabled, &item.IntervalMinutes, &item.TimeOfDay, &item.Timezone, &item.WaitTime,
			&item.Message, &item.LastRunAt, &item.NextRunAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetSchedule(ctx context.Context, id string) (Schedule, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,type,enabled,interval_minutes,time_of_day,timezone,waittime,message,last_run_at,next_run_at,created_at,updated_at
		FROM schedules WHERE id=?`, id)
	var item Schedule
	var enabled int
	err := row.Scan(&item.ID, &item.Type, &enabled, &item.IntervalMinutes, &item.TimeOfDay, &item.Timezone, &item.WaitTime,
		&item.Message, &item.LastRunAt, &item.NextRunAt, &item.CreatedAt, &item.UpdatedAt)
	item.Enabled = enabled == 1
	return item, err
}

func (s *Store) DeleteSchedule(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM schedules WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) CreateAlert(ctx context.Context, item Alert) error {
	if item.ID == "" {
		return errors.New("alert id is required")
	}
	if item.CreatedAt == "" {
		item.CreatedAt = now()
	}
	if item.Status == "" {
		item.Status = "open"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO alerts (id,severity,title,message,source,status,created_at,ack_at) VALUES (?,?,?,?,?,?,?,?)`,
		item.ID, item.Severity, item.Title, item.Message, item.Source, item.Status, item.CreatedAt, item.AckAt)
	if err == nil {
		_ = s.RecordAlertIncident(ctx, item)
	}
	return err
}

func (s *Store) UpdateAlert(ctx context.Context, item Alert) error {
	res, err := s.db.ExecContext(ctx, `UPDATE alerts SET severity=?,title=?,message=?,source=? WHERE id=?`,
		item.Severity, item.Title, item.Message, item.Source, item.ID)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ResolveAlert(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE alerts SET status='resolved',ack_at=? WHERE id=?`, now(), id)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	_ = s.SyncAlertIncidentStatus(ctx, id, "resolved")
	return nil
}

func (s *Store) ListAlerts(ctx context.Context, limit int) ([]Alert, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,severity,title,message,source,status,created_at,ack_at FROM alerts ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var item Alert
		if err := rows.Scan(&item.ID, &item.Severity, &item.Title, &item.Message, &item.Source, &item.Status, &item.CreatedAt, &item.AckAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) AckAlert(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE alerts SET status='acked', ack_at=? WHERE id=?`, now(), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	_ = s.SyncAlertIncidentStatus(ctx, id, "acknowledged")
	return nil
}

func (s *Store) CreateJob(ctx context.Context, id, typ, message string) (Job, error) {
	now := now()
	j := Job{ID: id, Type: typ, Status: "queued", Progress: 0, Message: message, CreatedAt: now, UpdatedAt: now}
	_, err := s.db.ExecContext(ctx, `INSERT INTO jobs (id,type,status,progress,message,error,error_code,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		j.ID, j.Type, j.Status, j.Progress, j.Message, j.Error, j.ErrorCode, j.CreatedAt, j.UpdatedAt)
	return j, err
}

func (s *Store) UpdateJob(ctx context.Context, id, status string, progress int, message, errText string) error {
	return s.UpdateJobWithCode(ctx, id, status, progress, message, errText, "")

}

func (s *Store) UpdateJobWithCode(ctx context.Context, id, status string, progress int, message, errText, errorCode string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET status=?, progress=?, message=?, error=?, error_code=?, updated_at=? WHERE id=?`,
		status, progress, message, errText, errorCode, now(), id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	if status == "failed" {
		job, readErr := s.GetJob(ctx, id)
		if readErr == nil {
			_ = s.RecordFailedJobIncident(ctx, job)
		}
	}
	return err
}

func (s *Store) FailIncompleteJobs(ctx context.Context, errorCode, message string) (int64, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE jobs
		SET status='failed', message=?, error=?, error_code=?, updated_at=?
		WHERE status IN ('queued', 'waiting', 'running')`, message, message, errorCode, now())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) GetJob(ctx context.Context, id string) (Job, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,type,status,progress,message,error,error_code,created_at,updated_at FROM jobs WHERE id=?`, id)
	var j Job
	err := row.Scan(&j.ID, &j.Type, &j.Status, &j.Progress, &j.Message, &j.Error, &j.ErrorCode, &j.CreatedAt, &j.UpdatedAt)
	return j, err
}

func (s *Store) ListJobs(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,type,status,progress,message,error,error_code,created_at,updated_at FROM jobs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.Type, &j.Status, &j.Progress, &j.Message, &j.Error, &j.ErrorCode, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (s *Store) UpsertMod(ctx context.Context, m Mod) error {
	now := now()
	if m.CreatedAt == "" {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	tagsJSON := encodeTags(m.Tags)
	_, err := s.db.ExecContext(ctx, `INSERT INTO mods (
			id,name,source,package_name,path,version,enabled,workshop_id,preview_url,steam_url,summary,tags_json,
			file_size,subscriptions,time_updated,last_checked_at,created_at,updated_at
		)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, source=excluded.source, package_name=excluded.package_name,
		path=excluded.path, version=excluded.version, enabled=excluded.enabled, workshop_id=excluded.workshop_id,
		preview_url=excluded.preview_url, steam_url=excluded.steam_url, summary=excluded.summary, tags_json=excluded.tags_json,
		file_size=excluded.file_size, subscriptions=excluded.subscriptions, time_updated=excluded.time_updated,
		last_checked_at=excluded.last_checked_at, updated_at=excluded.updated_at`,
		m.ID, m.Name, m.Source, m.PackageName, m.Path, m.Version, boolInt(m.Enabled), m.WorkshopID, m.PreviewURL, m.SteamURL,
		m.Summary, tagsJSON, m.FileSize, m.Subscriptions, m.TimeUpdated, m.LastCheckedAt, m.CreatedAt, m.UpdatedAt)
	return err
}

func (s *Store) ListMods(ctx context.Context) ([]Mod, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,source,package_name,path,version,enabled,workshop_id,preview_url,steam_url,summary,tags_json,file_size,subscriptions,time_updated,last_checked_at,created_at,updated_at FROM mods ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mods []Mod
	for rows.Next() {
		var m Mod
		var enabled int
		var tagsJSON string
		if err := rows.Scan(
			&m.ID, &m.Name, &m.Source, &m.PackageName, &m.Path, &m.Version, &enabled, &m.WorkshopID, &m.PreviewURL,
			&m.SteamURL, &m.Summary, &tagsJSON, &m.FileSize, &m.Subscriptions, &m.TimeUpdated, &m.LastCheckedAt,
			&m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, err
		}
		m.Enabled = enabled == 1
		m.Tags = decodeTags(tagsJSON)
		mods = append(mods, m)
	}
	return mods, rows.Err()
}

func (s *Store) GetMod(ctx context.Context, id string) (Mod, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,source,package_name,path,version,enabled,workshop_id,preview_url,steam_url,summary,tags_json,file_size,subscriptions,time_updated,last_checked_at,created_at,updated_at FROM mods WHERE id=?`, id)
	var m Mod
	var enabled int
	var tagsJSON string
	err := row.Scan(
		&m.ID, &m.Name, &m.Source, &m.PackageName, &m.Path, &m.Version, &enabled, &m.WorkshopID, &m.PreviewURL,
		&m.SteamURL, &m.Summary, &tagsJSON, &m.FileSize, &m.Subscriptions, &m.TimeUpdated, &m.LastCheckedAt,
		&m.CreatedAt, &m.UpdatedAt,
	)
	m.Enabled = enabled == 1
	m.Tags = decodeTags(tagsJSON)
	return m, err
}

func (s *Store) SetModEnabled(ctx context.Context, id string, enabled bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE mods SET enabled=?, updated_at=? WHERE id=?`, boolInt(enabled), now(), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteMod(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM mods WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) SetKV(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES (?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, value, now())
	return err
}

func (s *Store) GetKV(ctx context.Context, key string) (string, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key=?`, key)
	var v string
	err := row.Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) DeleteKV(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM kv WHERE key=?`, key)
	return err
}

func (s *Store) GetAITranslation(ctx context.Context, workshopID, sourceSHA256, targetLanguage, provider, model string) (AITranslation, error) {
	row := s.db.QueryRowContext(ctx, `SELECT workshop_id,source_sha256,target_language,provider,model,translation,created_at
		FROM ai_translations WHERE workshop_id=? AND source_sha256=? AND target_language=? AND provider=? AND model=?`,
		workshopID, sourceSHA256, targetLanguage, provider, model)
	var item AITranslation
	err := row.Scan(&item.WorkshopID, &item.SourceSHA256, &item.TargetLanguage, &item.Provider, &item.Model, &item.Translation, &item.CreatedAt)
	return item, err
}

func (s *Store) UpsertAITranslation(ctx context.Context, item AITranslation) error {
	if item.CreatedAt == "" {
		item.CreatedAt = now()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO ai_translations (
		workshop_id,source_sha256,target_language,provider,model,translation,created_at
	) VALUES (?,?,?,?,?,?,?)
	ON CONFLICT(workshop_id,source_sha256,target_language,provider,model)
	DO UPDATE SET translation=excluded.translation, created_at=excluded.created_at`,
		item.WorkshopID, item.SourceSHA256, item.TargetLanguage, item.Provider, item.Model, item.Translation, item.CreatedAt)
	return err
}

func (s *Store) UserCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func (s *Store) CreateInitialUser(ctx context.Context, user User) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return ErrAlreadyInitialized
	}
	if user.CreatedAt == "" {
		user.CreatedAt = now()
	}
	user.UpdatedAt = user.CreatedAt
	_, err = tx.ExecContext(ctx, `INSERT INTO users (id,username,password_hash,role,disabled,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`,
		user.ID, user.Username, user.PasswordHash, user.Role, boolInt(user.Disabled), user.CreatedAt, user.UpdatedAt)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	var user User
	var disabled int
	err := s.db.QueryRowContext(ctx, `SELECT id,username,password_hash,role,disabled,created_at,updated_at FROM users WHERE username=?`, username).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &disabled, &user.CreatedAt, &user.UpdatedAt)
	user.Disabled = disabled == 1
	return user, err
}

func (s *Store) FirstUser(ctx context.Context) (User, error) {
	var user User
	var disabled int
	err := s.db.QueryRowContext(ctx, `SELECT id,username,password_hash,role,disabled,created_at,updated_at FROM users ORDER BY created_at LIMIT 1`).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &disabled, &user.CreatedAt, &user.UpdatedAt)
	user.Disabled = disabled == 1
	return user, err
}

func (s *Store) CreateSession(ctx context.Context, session Session) error {
	if session.CreatedAt == "" {
		session.CreatedAt = now()
	}
	if session.LastSeenAt == "" {
		session.LastSeenAt = session.CreatedAt
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id,user_id,token_hash,expires_at,last_seen_at,created_at) VALUES (?,?,?,?,?,?)`,
		session.ID, session.UserID, session.TokenHash, session.ExpiresAt, session.LastSeenAt, session.CreatedAt)
	return err
}

func (s *Store) GetUserBySessionHash(ctx context.Context, tokenHash string, at time.Time) (User, Session, error) {
	var user User
	var session Session
	var disabled int
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.username,u.password_hash,u.role,u.disabled,u.created_at,u.updated_at,
		s.id,s.user_id,s.token_hash,s.expires_at,s.last_seen_at,s.created_at
		FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=? AND s.expires_at>? AND u.disabled=0`, tokenHash, at.UTC().Format(time.RFC3339Nano)).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &disabled, &user.CreatedAt, &user.UpdatedAt,
			&session.ID, &session.UserID, &session.TokenHash, &session.ExpiresAt, &session.LastSeenAt, &session.CreatedAt)
	user.Disabled = disabled == 1
	return user, session, err
}

func (s *Store) TouchSession(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at=? WHERE id=?`, at.UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *Store) DeleteSessionByHash(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at<=?`, at.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) CreateAPIKey(ctx context.Context, key APIKey) error {
	if key.CreatedAt == "" {
		key.CreatedAt = now()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO api_keys (id,user_id,name,prefix,token_hash,last_used_at,revoked_at,created_at) VALUES (?,?,?,?,?,?,?,?)`,
		key.ID, key.UserID, key.Name, key.Prefix, key.TokenHash, key.LastUsedAt, key.RevokedAt, key.CreatedAt)
	return err
}

func (s *Store) ListAPIKeys(ctx context.Context, userID string) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,user_id,name,prefix,last_used_at,revoked_at,created_at FROM api_keys WHERE user_id=? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]APIKey, 0)
	for rows.Next() {
		var key APIKey
		if err := rows.Scan(&key.ID, &key.UserID, &key.Name, &key.Prefix, &key.LastUsedAt, &key.RevokedAt, &key.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, key)
	}
	return items, rows.Err()
}

func (s *Store) GetUserByAPIKeyHash(ctx context.Context, tokenHash string) (User, APIKey, error) {
	var user User
	var key APIKey
	var disabled int
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.username,u.password_hash,u.role,u.disabled,u.created_at,u.updated_at,
		k.id,k.user_id,k.name,k.prefix,k.token_hash,k.last_used_at,k.revoked_at,k.created_at
		FROM api_keys k JOIN users u ON u.id=k.user_id
		WHERE k.token_hash=? AND k.revoked_at='' AND u.disabled=0`, tokenHash).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &disabled, &user.CreatedAt, &user.UpdatedAt,
			&key.ID, &key.UserID, &key.Name, &key.Prefix, &key.TokenHash, &key.LastUsedAt, &key.RevokedAt, &key.CreatedAt)
	user.Disabled = disabled == 1
	return user, key, err
}

func (s *Store) TouchAPIKey(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at=? WHERE id=?`, at.UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *Store) RevokeAPIKey(ctx context.Context, userID, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE api_keys SET revoked_at=? WHERE id=? AND user_id=? AND revoked_at=''`, now(), id, userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ResetUserPassword(ctx context.Context, username, passwordHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=?,updated_at=? WHERE username=?`, passwordHash, now(), username)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	var userID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE username=?`, username).Scan(&userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE api_keys SET revoked_at=? WHERE user_id=? AND revoked_at=''`, now(), userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpsertSaveSource(ctx context.Context, source SaveSource) error {
	if strings.TrimSpace(source.ID) == "" || strings.TrimSpace(source.Name) == "" || strings.TrimSpace(source.Kind) == "" {
		return errors.New("save source id, name and kind are required")
	}
	stamp := now()
	if source.CreatedAt == "" {
		source.CreatedAt = stamp
	}
	source.UpdatedAt = stamp
	_, err := s.db.ExecContext(ctx, `INSERT INTO save_sources(id,name,kind,path,active,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,kind=excluded.kind,path=excluded.path,updated_at=excluded.updated_at`,
		source.ID, source.Name, source.Kind, source.Path, boolInt(source.Active), source.CreatedAt, source.UpdatedAt)
	return err
}

func (s *Store) ListSaveSources(ctx context.Context) ([]SaveSource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,kind,path,active,fingerprint,parser_version,warnings_json,indexed_at,created_at,updated_at FROM save_sources ORDER BY active DESC,created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SaveSource{}
	for rows.Next() {
		var item SaveSource
		var active int
		var warningsJSON string
		if err := rows.Scan(&item.ID, &item.Name, &item.Kind, &item.Path, &active, &item.Fingerprint, &item.ParserVersion, &warningsJSON, &item.IndexedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Active = active == 1
		item.Warnings = decodeTags(warningsJSON)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetSaveSource(ctx context.Context, id string) (SaveSource, error) {
	var item SaveSource
	var active int
	var warningsJSON string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,kind,path,active,fingerprint,parser_version,warnings_json,indexed_at,created_at,updated_at FROM save_sources WHERE id=?`, id).
		Scan(&item.ID, &item.Name, &item.Kind, &item.Path, &active, &item.Fingerprint, &item.ParserVersion, &warningsJSON, &item.IndexedAt, &item.CreatedAt, &item.UpdatedAt)
	item.Active = active == 1
	item.Warnings = decodeTags(warningsJSON)
	return item, err
}

func (s *Store) ActiveSaveSource(ctx context.Context) (SaveSource, error) {
	var item SaveSource
	var active int
	var warningsJSON string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,kind,path,active,fingerprint,parser_version,warnings_json,indexed_at,created_at,updated_at FROM save_sources WHERE active=1 LIMIT 1`).
		Scan(&item.ID, &item.Name, &item.Kind, &item.Path, &active, &item.Fingerprint, &item.ParserVersion, &warningsJSON, &item.IndexedAt, &item.CreatedAt, &item.UpdatedAt)
	item.Active = active == 1
	item.Warnings = decodeTags(warningsJSON)
	return item, err
}

func (s *Store) UpdateSaveSourceIndex(ctx context.Context, id, fingerprint, parserVersion string, warnings []string, indexedAt string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE save_sources SET fingerprint=?,parser_version=?,warnings_json=?,indexed_at=?,updated_at=? WHERE id=?`, fingerprint, parserVersion, encodeTags(warnings), indexedAt, now(), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ActivateSaveSource(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE save_sources SET active=0,updated_at=? WHERE active=1`, now()); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE save_sources SET active=1,updated_at=? WHERE id=?`, now(), id)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) DeleteSaveSource(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM save_sources WHERE id=? AND kind!='server' AND active=0`, id)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) CreateBreedingResult(ctx context.Context, item BreedingResult) error {
	stamp := now()
	if item.CreatedAt == "" {
		item.CreatedAt = stamp
	}
	item.UpdatedAt = stamp
	_, err := s.db.ExecContext(ctx, `INSERT INTO breeding_results(id,job_id,subject,source_id,fingerprint,request_json,result_json,status,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, item.ID, item.JobID, item.Subject, item.SourceID, item.Fingerprint, item.RequestJSON, item.ResultJSON, item.Status, item.CreatedAt, item.UpdatedAt)
	return err
}

func (s *Store) CompleteBreedingResult(ctx context.Context, jobID, status, resultJSON string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE breeding_results SET status=?,result_json=?,updated_at=? WHERE job_id=?`, status, resultJSON, now(), jobID)
	return err
}

func (s *Store) GetBreedingResultByJob(ctx context.Context, jobID string) (BreedingResult, error) {
	var item BreedingResult
	err := s.db.QueryRowContext(ctx, `SELECT id,job_id,subject,source_id,fingerprint,request_json,result_json,status,created_at,updated_at FROM breeding_results WHERE job_id=?`, jobID).
		Scan(&item.ID, &item.JobID, &item.Subject, &item.SourceID, &item.Fingerprint, &item.RequestJSON, &item.ResultJSON, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *Store) ListBreedingResults(ctx context.Context, subject string, limit int) ([]BreedingResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,job_id,subject,source_id,fingerprint,request_json,result_json,status,created_at,updated_at FROM breeding_results WHERE subject=? ORDER BY created_at DESC LIMIT ?`, subject, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BreedingResult{}
	for rows.Next() {
		var item BreedingResult
		if err := rows.Scan(&item.ID, &item.JobID, &item.Subject, &item.SourceID, &item.Fingerprint, &item.RequestJSON, &item.ResultJSON, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) FindCachedBreedingResult(ctx context.Context, sourceID, fingerprint, requestJSON string) (BreedingResult, error) {
	var item BreedingResult
	err := s.db.QueryRowContext(ctx, `SELECT id,job_id,subject,source_id,fingerprint,request_json,result_json,status,created_at,updated_at
		FROM breeding_results WHERE source_id=? AND fingerprint=? AND request_json=? AND status='completed' AND result_json!=''
		ORDER BY updated_at DESC LIMIT 1`, sourceID, fingerprint, requestJSON).
		Scan(&item.ID, &item.JobID, &item.Subject, &item.SourceID, &item.Fingerprint, &item.RequestJSON, &item.ResultJSON, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *Store) ListBreedingPresets(ctx context.Context, subject string) ([]BreedingPreset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,subject,name,config_json,created_at,updated_at FROM breeding_presets WHERE subject=? ORDER BY name`, subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BreedingPreset{}
	for rows.Next() {
		var item BreedingPreset
		if err := rows.Scan(&item.ID, &item.Subject, &item.Name, &item.ConfigJSON, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpsertBreedingPreset(ctx context.Context, item BreedingPreset) error {
	stamp := now()
	if item.CreatedAt == "" {
		item.CreatedAt = stamp
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO breeding_presets(id,subject,name,config_json,created_at,updated_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name,config_json=excluded.config_json,updated_at=excluded.updated_at WHERE breeding_presets.subject=excluded.subject`,
		item.ID, item.Subject, item.Name, item.ConfigJSON, item.CreatedAt, stamp)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteBreedingPreset(ctx context.Context, subject, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM breeding_presets WHERE subject=? AND id=?`, subject, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListCustomPalContainers(ctx context.Context, subject string) ([]CustomPalContainer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,subject,name,pals_json,created_at,updated_at FROM custom_pal_containers WHERE subject=? ORDER BY name`, subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CustomPalContainer{}
	for rows.Next() {
		var item CustomPalContainer
		if err := rows.Scan(&item.ID, &item.Subject, &item.Name, &item.PalsJSON, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetCustomPalContainer(ctx context.Context, subject, id string) (CustomPalContainer, error) {
	var item CustomPalContainer
	err := s.db.QueryRowContext(ctx, `SELECT id,subject,name,pals_json,created_at,updated_at FROM custom_pal_containers WHERE subject=? AND id=?`, subject, id).
		Scan(&item.ID, &item.Subject, &item.Name, &item.PalsJSON, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *Store) UpsertCustomPalContainer(ctx context.Context, item CustomPalContainer) error {
	stamp := now()
	if item.CreatedAt == "" {
		item.CreatedAt = stamp
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO custom_pal_containers(id,subject,name,pals_json,created_at,updated_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name,pals_json=excluded.pals_json,updated_at=excluded.updated_at WHERE custom_pal_containers.subject=excluded.subject`,
		item.ID, item.Subject, item.Name, item.PalsJSON, item.CreatedAt, stamp)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteCustomPalContainer(ctx context.Context, subject, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM custom_pal_containers WHERE subject=? AND id=?`, subject, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) CreateBreedSession(ctx context.Context, item BreedSession) error {
	if item.CreatedAt == "" {
		item.CreatedAt = now()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO breed_sessions(id,subject,token_hash,player_uid,expires_at,created_at) VALUES(?,?,?,?,?,?)`,
		item.ID, item.Subject, item.TokenHash, item.PlayerUID, item.ExpiresAt, item.CreatedAt)
	return err
}

func (s *Store) GetBreedSession(ctx context.Context, tokenHash string, at time.Time) (BreedSession, error) {
	var item BreedSession
	err := s.db.QueryRowContext(ctx, `SELECT id,subject,token_hash,player_uid,expires_at,created_at FROM breed_sessions WHERE token_hash=? AND expires_at>?`, tokenHash, at.UTC().Format(time.RFC3339Nano)).
		Scan(&item.ID, &item.Subject, &item.TokenHash, &item.PlayerUID, &item.ExpiresAt, &item.CreatedAt)
	return item, err
}

func (s *Store) DeleteExpiredBreedSessions(ctx context.Context, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM breed_sessions WHERE expires_at<=?`, at.UTC().Format(time.RFC3339Nano))
	return err
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func encodeTags(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	data, err := json.Marshal(tags)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func decodeTags(value string) []string {
	if value == "" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(value), &tags); err != nil {
		return nil
	}
	return tags
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
