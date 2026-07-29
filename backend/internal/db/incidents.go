package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const incidentWebhookEnabledKey = "incident_webhook_enabled"

type Incident struct {
	ID             string         `json:"id"`
	DedupeKey      string         `json:"-"`
	Kind           string         `json:"kind"`
	Severity       string         `json:"severity"`
	Source         string         `json:"source"`
	Title          string         `json:"title"`
	Summary        string         `json:"summary"`
	Status         string         `json:"status"`
	Occurrences    int            `json:"occurrences"`
	FirstSeenAt    string         `json:"first_seen_at"`
	LastSeenAt     string         `json:"last_seen_at"`
	AcknowledgedAt string         `json:"acknowledged_at,omitempty"`
	ResolvedAt     string         `json:"resolved_at,omitempty"`
	Details        map[string]any `json:"details,omitempty"`
	UpdatedAt      string         `json:"updated_at"`
}

type IncidentEvent struct {
	ID         string `json:"id"`
	IncidentID string `json:"incident_id"`
	Type       string `json:"type"`
	Actor      string `json:"actor,omitempty"`
	Message    string `json:"message,omitempty"`
	CreatedAt  string `json:"created_at"`
}

type IncidentInput struct {
	DedupeKey string
	Kind      string
	Severity  string
	Source    string
	Title     string
	Summary   string
	Details   map[string]any
	Actor     string
}

type IncidentListFilter struct {
	Status   string
	Severity string
	Source   string
	Query    string
	Limit    int
	Offset   int
}

type IncidentDelivery struct {
	ID            string `json:"id"`
	IncidentID    string `json:"incident_id"`
	EventID       string `json:"event_id"`
	Status        string `json:"status"`
	Attempts      int    `json:"attempts"`
	NextAttemptAt string `json:"next_attempt_at,omitempty"`
	DeliveredAt   string `json:"delivered_at,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type IncidentDeliveryPayload struct {
	Delivery IncidentDelivery
	Incident Incident
	Event    IncidentEvent
}

func migrateIncidents(ctx context.Context, tx *sql.Tx) error {
	return execAll(ctx, tx,
		`CREATE TABLE IF NOT EXISTS incidents (
			id TEXT PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128 AND id NOT GLOB '*[^A-Za-z0-9_-]*'),
			dedupe_key TEXT NOT NULL UNIQUE CHECK(length(dedupe_key) BETWEEN 1 AND 256),
			kind TEXT NOT NULL CHECK(length(kind) BETWEEN 1 AND 64),
			severity TEXT NOT NULL CHECK(severity IN ('info','warning','error','critical')),
			source TEXT NOT NULL CHECK(length(source) BETWEEN 1 AND 128),
			title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 256),
			summary TEXT NOT NULL DEFAULT '' CHECK(length(summary)<=2048),
			status TEXT NOT NULL CHECK(status IN ('open','acknowledged','resolved')),
			occurrences INTEGER NOT NULL DEFAULT 1 CHECK(occurrences BETWEEN 1 AND 1000000),
			first_seen_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			acknowledged_at TEXT NOT NULL DEFAULT '',
			resolved_at TEXT NOT NULL DEFAULT '',
			details_json TEXT NOT NULL DEFAULT '{}',
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_incidents_status_seen ON incidents(status,last_seen_at DESC,id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_incidents_source_seen ON incidents(source,last_seen_at DESC,id DESC)`,
		`CREATE TABLE IF NOT EXISTS incident_events (
			id TEXT PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128 AND id NOT GLOB '*[^A-Za-z0-9_-]*'),
			incident_id TEXT NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
			event_type TEXT NOT NULL CHECK(event_type IN ('opened','occurred','reopened','acknowledged','resolved','delivery_failed')),
			actor TEXT NOT NULL DEFAULT '' CHECK(length(actor)<=256),
			message TEXT NOT NULL DEFAULT '' CHECK(length(message)<=2048),
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_incident_events_incident ON incident_events(incident_id,created_at DESC,id DESC)`,
		`CREATE TABLE IF NOT EXISTS incident_job_links (
			job_id TEXT PRIMARY KEY,
			incident_id TEXT NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS incident_deliveries (
			id TEXT PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128 AND id NOT GLOB '*[^A-Za-z0-9_-]*'),
			incident_id TEXT NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
			event_id TEXT NOT NULL UNIQUE REFERENCES incident_events(id) ON DELETE CASCADE,
			channel TEXT NOT NULL CHECK(channel='webhook'),
			status TEXT NOT NULL CHECK(status IN ('pending','delivered','failed')),
			attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 100),
			next_attempt_at TEXT NOT NULL,
			delivered_at TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_incident_deliveries_due ON incident_deliveries(status,next_attempt_at,id)`,
	)
}

func newIncidentID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		stamp := strings.NewReplacer("-", "", ":", "", ".", "").Replace(time.Now().UTC().Format(time.RFC3339Nano))
		return prefix + "_" + stamp
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func trimIncidentText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

func sanitizeIncidentText(value string, limit int) string {
	value = strings.Map(func(character rune) rune {
		if character == '\n' || character == '\r' || character == '\t' {
			return ' '
		}
		if character < 0x20 || character == 0x7f {
			return -1
		}
		return character
	}, value)
	fields := strings.Fields(value)
	for index, field := range fields {
		trimmed := strings.Trim(field, "\"'()[]{}<>,;")
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, `\\`) ||
			(len(trimmed) >= 3 && trimmed[1] == ':' && (trimmed[2] == '\\' || trimmed[2] == '/')) {
			fields[index] = "[redacted-path]"
			continue
		}
		if parsed, err := url.Parse(trimmed); err == nil && parsed.IsAbs() && parsed.Hostname() != "" {
			fields[index] = parsed.Scheme + "://" + parsed.Host
			continue
		}
		if separator := strings.IndexByte(trimmed, '='); separator > 0 {
			name := lower[:separator]
			value := trimmed[separator+1:]
			if strings.Contains(name, "token") || strings.Contains(name, "secret") || strings.Contains(name, "password") || strings.Contains(name, "passwd") {
				fields[index] = trimmed[:separator+1] + "[redacted]"
				continue
			}
			if parsed, err := url.Parse(value); err == nil && parsed.IsAbs() && parsed.Hostname() != "" {
				fields[index] = trimmed[:separator+1] + parsed.Scheme + "://" + parsed.Host
			}
		}
	}
	return trimIncidentText(strings.Join(fields, " "), limit)
}

func sanitizeIncidentDetails(value any) any {
	switch typed := value.(type) {
	case string:
		return sanitizeIncidentText(typed, 2048)
	case []string:
		result := make([]string, len(typed))
		for index, item := range typed {
			result[index] = sanitizeIncidentText(item, 2048)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = sanitizeIncidentDetails(item)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "path") || strings.Contains(lower, "log") || strings.Contains(lower, "command") {
				continue
			}
			result[key] = sanitizeIncidentDetails(item)
		}
		return result
	default:
		return value
	}
}

func sanitizeIncidentDetailMap(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	result, _ := sanitizeIncidentDetails(details).(map[string]any)
	return result
}

func normalizeIncidentSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical":
		return "critical"
	case "error":
		return "error"
	case "warning", "warn":
		return "warning"
	default:
		return "info"
	}
}

func severityRank(value string) int {
	switch value {
	case "critical":
		return 4
	case "error":
		return 3
	case "warning":
		return 2
	default:
		return 1
	}
}

func validIncidentStatus(value string) bool {
	return value == "open" || value == "acknowledged" || value == "resolved"
}

func marshalIncidentDetails(details map[string]any) string {
	if len(details) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(details)
	if err != nil || len(encoded) > 16*1024 {
		return "{}"
	}
	return string(encoded)
}

func scanIncident(scanner interface{ Scan(...any) error }) (Incident, error) {
	var item Incident
	var detailsJSON string
	err := scanner.Scan(&item.ID, &item.DedupeKey, &item.Kind, &item.Severity, &item.Source, &item.Title,
		&item.Summary, &item.Status, &item.Occurrences, &item.FirstSeenAt, &item.LastSeenAt,
		&item.AcknowledgedAt, &item.ResolvedAt, &detailsJSON, &item.UpdatedAt)
	if err == nil && detailsJSON != "" && detailsJSON != "{}" {
		_ = json.Unmarshal([]byte(detailsJSON), &item.Details)
	}
	return item, err
}

func (s *Store) webhookEnabledTx(ctx context.Context, tx *sql.Tx) bool {
	var value string
	return tx.QueryRowContext(ctx, `SELECT value FROM kv WHERE key=?`, incidentWebhookEnabledKey).Scan(&value) == nil && value == "1"
}

func (s *Store) queueIncidentDeliveryTx(ctx context.Context, tx *sql.Tx, incidentID, eventID, stamp string) error {
	if !s.webhookEnabledTx(ctx, tx) {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO incident_deliveries
		(id,incident_id,event_id,channel,status,attempts,next_attempt_at,created_at,updated_at)
		VALUES(?,?,?,'webhook','pending',0,?,?,?)`, newIncidentID("delivery"), incidentID, eventID, stamp, stamp, stamp)
	return err
}

func (s *Store) UpsertIncident(ctx context.Context, input IncidentInput) (Incident, IncidentEvent, error) {
	s.incidentMu.Lock()
	defer s.incidentMu.Unlock()
	input.DedupeKey = trimIncidentText(input.DedupeKey, 256)
	input.Kind = trimIncidentText(input.Kind, 64)
	input.Source = trimIncidentText(input.Source, 128)
	input.Title = sanitizeIncidentText(input.Title, 256)
	input.Summary = sanitizeIncidentText(input.Summary, 2048)
	input.Details = sanitizeIncidentDetailMap(input.Details)
	input.Actor = sanitizeIncidentText(input.Actor, 256)
	input.Severity = normalizeIncidentSeverity(input.Severity)
	if input.DedupeKey == "" || input.Kind == "" || input.Source == "" || input.Title == "" {
		return Incident{}, IncidentEvent{}, errors.New("invalid incident input")
	}
	stamp := now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	defer tx.Rollback()

	item, readErr := scanIncident(tx.QueryRowContext(ctx, `SELECT id,dedupe_key,kind,severity,source,title,summary,status,occurrences,
		first_seen_at,last_seen_at,acknowledged_at,resolved_at,details_json,updated_at FROM incidents WHERE dedupe_key=?`, input.DedupeKey))
	eventType := "occurred"
	if errors.Is(readErr, sql.ErrNoRows) {
		item = Incident{ID: newIncidentID("incident"), DedupeKey: input.DedupeKey, Kind: input.Kind, Severity: input.Severity,
			Source: input.Source, Title: input.Title, Summary: input.Summary, Status: "open", Occurrences: 1,
			FirstSeenAt: stamp, LastSeenAt: stamp, Details: input.Details, UpdatedAt: stamp}
		_, err = tx.ExecContext(ctx, `INSERT INTO incidents
			(id,dedupe_key,kind,severity,source,title,summary,status,occurrences,first_seen_at,last_seen_at,details_json,updated_at)
			VALUES(?,?,?,?,?,?,?,'open',1,?,?,?,?)`, item.ID, item.DedupeKey, item.Kind, item.Severity, item.Source,
			item.Title, item.Summary, item.FirstSeenAt, item.LastSeenAt, marshalIncidentDetails(item.Details), item.UpdatedAt)
		eventType = "opened"
	} else if readErr != nil {
		return Incident{}, IncidentEvent{}, readErr
	} else {
		if severityRank(input.Severity) > severityRank(item.Severity) {
			item.Severity = input.Severity
		}
		item.Kind, item.Source, item.Title, item.Summary = input.Kind, input.Source, input.Title, input.Summary
		item.Occurrences++
		item.LastSeenAt, item.UpdatedAt, item.Details = stamp, stamp, input.Details
		if item.Status == "resolved" {
			item.Status, item.ResolvedAt, item.AcknowledgedAt = "open", "", ""
			eventType = "reopened"
		}
		_, err = tx.ExecContext(ctx, `UPDATE incidents SET kind=?,severity=?,source=?,title=?,summary=?,status=?,occurrences=?,
			last_seen_at=?,acknowledged_at=?,resolved_at=?,details_json=?,updated_at=? WHERE id=?`, item.Kind, item.Severity,
			item.Source, item.Title, item.Summary, item.Status, item.Occurrences, item.LastSeenAt, item.AcknowledgedAt,
			item.ResolvedAt, marshalIncidentDetails(item.Details), item.UpdatedAt, item.ID)
	}
	if err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	event := IncidentEvent{ID: newIncidentID("event"), IncidentID: item.ID, Type: eventType, Actor: input.Actor,
		Message: input.Summary, CreatedAt: stamp}
	if _, err = tx.ExecContext(ctx, `INSERT INTO incident_events(id,incident_id,event_type,actor,message,created_at) VALUES(?,?,?,?,?,?)`,
		event.ID, event.IncidentID, event.Type, event.Actor, event.Message, event.CreatedAt); err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	if err = s.queueIncidentDeliveryTx(ctx, tx, item.ID, event.ID, stamp); err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	if err = tx.Commit(); err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	return item, event, nil
}

func (s *Store) GetIncident(ctx context.Context, id string) (Incident, error) {
	return scanIncident(s.db.QueryRowContext(ctx, `SELECT id,dedupe_key,kind,severity,source,title,summary,status,occurrences,
		first_seen_at,last_seen_at,acknowledged_at,resolved_at,details_json,updated_at FROM incidents WHERE id=?`, id))
}

func (s *Store) ListIncidents(ctx context.Context, filter IncidentListFilter) ([]Incident, int, error) {
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 50
	}
	if filter.Offset < 0 || filter.Offset > 1000000 {
		filter.Offset = 0
	}
	clauses := []string{"1=1"}
	args := make([]any, 0, 8)
	if validIncidentStatus(filter.Status) {
		clauses = append(clauses, "status=?")
		args = append(args, filter.Status)
	}
	if severity := normalizeIncidentSeverity(filter.Severity); filter.Severity != "" {
		clauses = append(clauses, "severity=?")
		args = append(args, severity)
	}
	if source := strings.TrimSpace(filter.Source); source != "" {
		clauses = append(clauses, "source=?")
		args = append(args, source)
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		clauses = append(clauses, "(title LIKE ? ESCAPE '\\' OR summary LIKE ? ESCAPE '\\' OR source LIKE ? ESCAPE '\\')")
		query = "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(query) + "%"
		args = append(args, query, query, query)
	}
	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM incidents WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]any(nil), args...), filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id,dedupe_key,kind,severity,source,title,summary,status,occurrences,
		first_seen_at,last_seen_at,acknowledged_at,resolved_at,details_json,updated_at FROM incidents WHERE `+where+
		` ORDER BY CASE status WHEN 'open' THEN 0 WHEN 'acknowledged' THEN 1 ELSE 2 END,
		CASE severity WHEN 'critical' THEN 0 WHEN 'error' THEN 1 WHEN 'warning' THEN 2 ELSE 3 END,last_seen_at DESC,id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Incident, 0)
	for rows.Next() {
		item, err := scanIncident(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *Store) ListIncidentEvents(ctx context.Context, incidentID string, limit int) ([]IncidentEvent, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,incident_id,event_type,actor,message,created_at FROM incident_events
		WHERE incident_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, incidentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]IncidentEvent, 0)
	for rows.Next() {
		var item IncidentEvent
		if err := rows.Scan(&item.ID, &item.IncidentID, &item.Type, &item.Actor, &item.Message, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SetIncidentStatus(ctx context.Context, id, status, actor, message string) (Incident, IncidentEvent, error) {
	s.incidentMu.Lock()
	defer s.incidentMu.Unlock()
	if !validIncidentStatus(status) || status == "open" {
		return Incident{}, IncidentEvent{}, errors.New("invalid incident status transition")
	}
	stamp := now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	defer tx.Rollback()
	item, err := scanIncident(tx.QueryRowContext(ctx, `SELECT id,dedupe_key,kind,severity,source,title,summary,status,occurrences,
		first_seen_at,last_seen_at,acknowledged_at,resolved_at,details_json,updated_at FROM incidents WHERE id=?`, id))
	if err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	if item.Status == status {
		return item, IncidentEvent{}, tx.Commit()
	}
	item.Status, item.UpdatedAt = status, stamp
	if status == "acknowledged" {
		item.AcknowledgedAt = stamp
	} else {
		item.ResolvedAt = stamp
		if item.AcknowledgedAt == "" {
			item.AcknowledgedAt = stamp
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE incidents SET status=?,acknowledged_at=?,resolved_at=?,updated_at=? WHERE id=?`,
		item.Status, item.AcknowledgedAt, item.ResolvedAt, item.UpdatedAt, item.ID); err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	event := IncidentEvent{ID: newIncidentID("event"), IncidentID: item.ID, Type: status, Actor: sanitizeIncidentText(actor, 256),
		Message: sanitizeIncidentText(message, 2048), CreatedAt: stamp}
	if _, err := tx.ExecContext(ctx, `INSERT INTO incident_events(id,incident_id,event_type,actor,message,created_at) VALUES(?,?,?,?,?,?)`,
		event.ID, event.IncidentID, event.Type, event.Actor, event.Message, event.CreatedAt); err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	if err := s.queueIncidentDeliveryTx(ctx, tx, item.ID, event.ID, stamp); err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	if err := tx.Commit(); err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	return item, event, nil
}

func (s *Store) ReopenIncident(ctx context.Context, id, actor, message string) (Incident, IncidentEvent, error) {
	s.incidentMu.Lock()
	defer s.incidentMu.Unlock()
	stamp := now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	defer tx.Rollback()
	item, err := scanIncident(tx.QueryRowContext(ctx, `SELECT id,dedupe_key,kind,severity,source,title,summary,status,occurrences,
		first_seen_at,last_seen_at,acknowledged_at,resolved_at,details_json,updated_at FROM incidents WHERE id=?`, id))
	if err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	if item.Status == "open" {
		return item, IncidentEvent{}, tx.Commit()
	}
	item.Status, item.AcknowledgedAt, item.ResolvedAt, item.UpdatedAt = "open", "", "", stamp
	if _, err := tx.ExecContext(ctx, `UPDATE incidents SET status='open',acknowledged_at='',resolved_at='',updated_at=? WHERE id=?`, stamp, id); err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	event := IncidentEvent{ID: newIncidentID("event"), IncidentID: id, Type: "reopened", Actor: sanitizeIncidentText(actor, 256), Message: sanitizeIncidentText(message, 2048), CreatedAt: stamp}
	if _, err := tx.ExecContext(ctx, `INSERT INTO incident_events(id,incident_id,event_type,actor,message,created_at) VALUES(?,?,?,?,?,?)`, event.ID, id, event.Type, event.Actor, event.Message, stamp); err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	if err := s.queueIncidentDeliveryTx(ctx, tx, id, event.ID, stamp); err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	if err := tx.Commit(); err != nil {
		return Incident{}, IncidentEvent{}, err
	}
	return item, event, nil
}

func (s *Store) ResolveIncidentsBySource(ctx context.Context, source, actor, message string) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM incidents WHERE source=? AND status!='resolved'`, source)
	if err != nil {
		return err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, _, err := s.SetIncidentStatus(ctx, id, "resolved", actor, message); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return nil
}

func incidentJobSeverity(job Job) string {
	switch job.Type {
	case "palworld_config_apply", "world_reset":
		return "critical"
	case "backup", "install", "update", "panel_update", "safe_restart":
		return "error"
	default:
		return "warning"
	}
}

func incidentJobTitle(jobType string) string {
	labels := map[string]string{
		"backup": "备份任务失败", "install": "服务端安装失败", "update": "服务端更新失败",
		"panel_update": "面板更新失败", "palworld_config_apply": "配置应用失败", "world_reset": "世界重置失败",
		"safe_restart": "安全重启失败", "workshop_download": "Workshop 下载失败", "mod_import": "Mod 导入失败",
	}
	if value := labels[jobType]; value != "" {
		return value
	}
	return "后台任务失败"
}

func (s *Store) RecordFailedJobIncident(ctx context.Context, job Job) error {
	s.incidentJobMu.Lock()
	defer s.incidentJobMu.Unlock()
	if job.ID == "" || job.Status != "failed" || job.ErrorCode == "interrupted_by_shutdown" {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var linked string
	readErr := tx.QueryRowContext(ctx, `SELECT incident_id FROM incident_job_links WHERE job_id=?`, job.ID).Scan(&linked)
	if readErr == nil {
		_ = tx.Rollback()
		return nil
	}
	if !errors.Is(readErr, sql.ErrNoRows) {
		tx.Rollback()
		return readErr
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	code := trimIncidentText(job.ErrorCode, 96)
	if code == "" {
		code = "failed"
	}
	item, _, err := s.UpsertIncident(ctx, IncidentInput{
		DedupeKey: "job:" + trimIncidentText(job.Type, 96) + ":" + code,
		Kind:      "job_failure", Severity: incidentJobSeverity(job), Source: "job:" + trimIncidentText(job.Type, 96),
		Title: incidentJobTitle(job.Type), Summary: trimIncidentText(job.Message, 512), Actor: "system",
		Details: map[string]any{"job_id": job.ID, "job_type": job.Type, "error_code": code},
	})
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO incident_job_links(job_id,incident_id,created_at) VALUES(?,?,?)`, job.ID, item.ID, now())
	return err
}

func (s *Store) RecordAlertIncident(ctx context.Context, alert Alert) error {
	if alert.ID == "" || alert.Status == "resolved" {
		return nil
	}
	kind := "alert"
	if strings.HasPrefix(alert.Source, "monitor:") {
		kind = "monitor_alert"
	}
	_, _, err := s.UpsertIncident(ctx, IncidentInput{DedupeKey: "alert:" + alert.Source, Kind: kind,
		Severity: alert.Severity, Source: alert.Source, Title: alert.Title, Summary: alert.Message, Actor: "system",
		Details: map[string]any{"alert_id": alert.ID}})
	return err
}

func (s *Store) SyncAlertIncidentStatus(ctx context.Context, alertID, status string) error {
	var source string
	if err := s.db.QueryRowContext(ctx, `SELECT source FROM alerts WHERE id=?`, alertID).Scan(&source); err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM incidents WHERE dedupe_key=? AND status!='resolved'`, "alert:"+source)
	if err != nil {
		return err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		target := "acknowledged"
		if status == "resolved" {
			target = "resolved"
		}
		if _, _, err := s.SetIncidentStatus(ctx, id, target, "system", "linked alert status changed"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SetIncidentWebhookEnabled(ctx context.Context, enabled bool) error {
	value := "0"
	if enabled {
		value = "1"
	}
	return s.SetKV(ctx, incidentWebhookEnabledKey, value)
}

func (s *Store) ListDueIncidentDeliveries(ctx context.Context, before string, limit int) ([]IncidentDeliveryPayload, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.id,d.incident_id,d.event_id,d.status,d.attempts,d.next_attempt_at,d.delivered_at,d.created_at,d.updated_at,
		i.id,i.dedupe_key,i.kind,i.severity,i.source,i.title,i.summary,i.status,i.occurrences,i.first_seen_at,i.last_seen_at,i.acknowledged_at,i.resolved_at,i.details_json,i.updated_at,
		e.id,e.incident_id,e.event_type,e.actor,e.message,e.created_at
		FROM incident_deliveries d JOIN incidents i ON i.id=d.incident_id JOIN incident_events e ON e.id=d.event_id
		WHERE d.status='pending' AND d.next_attempt_at<=? ORDER BY d.next_attempt_at,d.id LIMIT ?`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]IncidentDeliveryPayload, 0)
	for rows.Next() {
		var item IncidentDeliveryPayload
		var detailsJSON string
		if err := rows.Scan(&item.Delivery.ID, &item.Delivery.IncidentID, &item.Delivery.EventID, &item.Delivery.Status,
			&item.Delivery.Attempts, &item.Delivery.NextAttemptAt, &item.Delivery.DeliveredAt, &item.Delivery.CreatedAt, &item.Delivery.UpdatedAt,
			&item.Incident.ID, &item.Incident.DedupeKey, &item.Incident.Kind, &item.Incident.Severity, &item.Incident.Source,
			&item.Incident.Title, &item.Incident.Summary, &item.Incident.Status, &item.Incident.Occurrences,
			&item.Incident.FirstSeenAt, &item.Incident.LastSeenAt, &item.Incident.AcknowledgedAt, &item.Incident.ResolvedAt,
			&detailsJSON, &item.Incident.UpdatedAt, &item.Event.ID, &item.Event.IncidentID, &item.Event.Type,
			&item.Event.Actor, &item.Event.Message, &item.Event.CreatedAt); err != nil {
			return nil, err
		}
		if detailsJSON != "" && detailsJSON != "{}" {
			_ = json.Unmarshal([]byte(detailsJSON), &item.Incident.Details)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) MarkIncidentDeliveryDelivered(ctx context.Context, id string) error {
	stamp := now()
	_, err := s.db.ExecContext(ctx, `UPDATE incident_deliveries SET status='delivered',attempts=attempts+1,delivered_at=?,last_error='',updated_at=? WHERE id=? AND status='pending'`, stamp, stamp, id)
	return err
}

func (s *Store) MarkIncidentDeliveryRetry(ctx context.Context, id, nextAttempt, errorText string, final bool) error {
	status := "pending"
	if final {
		status = "failed"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE incident_deliveries SET status=?,attempts=attempts+1,next_attempt_at=?,last_error=?,updated_at=? WHERE id=? AND status='pending'`,
		status, nextAttempt, trimIncidentText(errorText, 512), now(), id)
	return err
}

func (s *Store) ListIncidentDeliveries(ctx context.Context, incidentID string, limit int) ([]IncidentDelivery, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,incident_id,event_id,status,attempts,next_attempt_at,delivered_at,created_at,updated_at
		FROM incident_deliveries WHERE incident_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, incidentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]IncidentDelivery, 0)
	for rows.Next() {
		var item IncidentDelivery
		if err := rows.Scan(&item.ID, &item.IncidentID, &item.EventID, &item.Status, &item.Attempts, &item.NextAttemptAt,
			&item.DeliveredAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) IncidentSummary(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status,COUNT(*) FROM incidents GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{"open": 0, "acknowledged": 0, "resolved": 0}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		out[status] = count
	}
	return out, rows.Err()
}

func (s *Store) DeleteResolvedIncidentsBefore(ctx context.Context, before string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM incidents WHERE status='resolved' AND resolved_at!='' AND resolved_at<?`, before)
	return err
}

func (s *Store) ValidateIncidentID(id string) error {
	if !validConfigRevisionID(id) {
		return fmt.Errorf("invalid incident id")
	}
	return nil
}
