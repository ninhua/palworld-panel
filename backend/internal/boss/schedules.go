package boss

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "time/tzdata"
)

const (
	ScheduleModeDaily = "daily"
	ScheduleModeCron  = "cron"

	ScheduleEventWarning = "warning"
	ScheduleEventSummon  = "summon"

	ScheduleEventSuccess = "success"
	ScheduleEventFailed  = "failed"
	ScheduleEventSkipped = "skipped"

	maximumWarningMinutes = 10080
)

var (
	ErrInvalidSchedule     = errors.New("boss schedule is invalid")
	ErrScheduleNotFound    = errors.New("boss schedule not found")
	ErrScheduleDisabled    = errors.New("boss schedule is disabled")
	ErrScheduleRunConflict = errors.New("boss schedule run conflict")
)

type ScheduleInput struct {
	Name             string         `json:"name"`
	TemplateID       string         `json:"template_id"`
	Mode             string         `json:"mode"`
	DailyTime        string         `json:"daily_time"`
	Cron             string         `json:"cron"`
	Timezone         string         `json:"timezone"`
	WarningMinutes   int            `json:"warning_minutes"`
	WarningTitle     string         `json:"warning_title"`
	WarningMessage   string         `json:"warning_message"`
	LocationOverride *Location      `json:"location_override,omitempty"`
	Enabled          bool           `json:"enabled"`
	Metadata         map[string]any `json:"metadata"`
}

type Schedule struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	TemplateID       string         `json:"template_id"`
	TemplateName     string         `json:"template_name"`
	Mode             string         `json:"mode"`
	DailyTime        string         `json:"daily_time,omitempty"`
	Cron             string         `json:"cron,omitempty"`
	Timezone         string         `json:"timezone"`
	WarningMinutes   int            `json:"warning_minutes"`
	WarningTitle     string         `json:"warning_title,omitempty"`
	WarningMessage   string         `json:"warning_message,omitempty"`
	LocationOverride *Location      `json:"location_override,omitempty"`
	Enabled          bool           `json:"enabled"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	NextRunAt        string         `json:"next_run_at,omitempty"`
	LastRunAt        string         `json:"last_run_at,omitempty"`
	LastWarningAt    string         `json:"last_warning_at,omitempty"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
	ArchivedAt       string         `json:"archived_at,omitempty"`
}

type ScheduleFilter struct {
	Enabled         string
	IncludeArchived bool
	Limit           int
	Offset          int
}

type ScheduleEvent struct {
	ID         int64          `json:"id"`
	ScheduleID string         `json:"schedule_id"`
	EventType  string         `json:"event_type"`
	Status     string         `json:"status"`
	PlannedFor string         `json:"planned_for"`
	SummonID   string         `json:"summon_id,omitempty"`
	Actor      string         `json:"actor,omitempty"`
	Message    string         `json:"message,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
	CreatedAt  string         `json:"created_at"`
}

type ScheduleEventFilter struct {
	ScheduleID string
	EventType  string
	Status     string
	Limit      int
	Offset     int
}

type RunDueResult struct {
	Checked  int `json:"checked"`
	Warnings int `json:"warnings"`
	Summons  int `json:"summons"`
	Failed   int `json:"failed"`
	Skipped  int `json:"skipped"`
}

func (s *Service) ensureScheduleSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS boss_schedules (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			template_id TEXT NOT NULL,
			mode TEXT NOT NULL CHECK(mode IN ('daily','cron')),
			daily_time TEXT NOT NULL DEFAULT '',
			cron_expr TEXT NOT NULL DEFAULT '',
			timezone TEXT NOT NULL,
			warning_minutes INTEGER NOT NULL DEFAULT 0 CHECK(warning_minutes BETWEEN 0 AND 10080),
			warning_title TEXT NOT NULL DEFAULT '',
			warning_message TEXT NOT NULL DEFAULT '',
			location_override_json TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
			metadata_json TEXT NOT NULL DEFAULT '{}',
			next_run_at TEXT NOT NULL DEFAULT '',
			last_run_at TEXT NOT NULL DEFAULT '',
			last_warning_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			archived_at TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(template_id) REFERENCES boss_templates(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_schedules_due ON boss_schedules(enabled,archived_at,next_run_at)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_schedules_template ON boss_schedules(template_id,updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS boss_schedule_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			schedule_id TEXT NOT NULL,
			event_type TEXT NOT NULL CHECK(event_type IN ('warning','summon')),
			status TEXT NOT NULL CHECK(status IN ('success','failed','skipped')),
			planned_for TEXT NOT NULL,
			summon_id TEXT NOT NULL DEFAULT '',
			actor TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			details_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			UNIQUE(schedule_id,event_type,planned_for),
			FOREIGN KEY(schedule_id) REFERENCES boss_schedules(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_schedule_events_schedule ON boss_schedule_events(schedule_id,id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_schedule_events_status ON boss_schedule_events(event_type,status,id DESC)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure boss schedule schema: %w", err)
		}
	}
	return nil
}

func (s *Service) startScheduleWorker() {
	if s == nil || s.scheduleStop != nil {
		return
	}
	s.scheduleStop = make(chan struct{})
	s.scheduleDone = make(chan struct{})
	go func() {
		defer close(s.scheduleDone)
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			_, _ = s.RunDueSchedules(context.Background(), time.Now(), "boss-scheduler")
		case <-s.scheduleStop:
			return
		}
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				_, _ = s.RunDueSchedules(context.Background(), now, "boss-scheduler")
			case <-s.scheduleStop:
				return
			}
		}
	}()
}

func (s *Service) CreateSchedule(ctx context.Context, input ScheduleInput) (Schedule, error) {
	normalized, next, err := s.normalizeScheduleInput(ctx, input, s.now())
	if err != nil {
		return Schedule{}, err
	}
	now := s.timestamp()
	item := Schedule{
		ID: newID("schedule"), Name: normalized.Name, TemplateID: normalized.TemplateID,
		Mode: normalized.Mode, DailyTime: normalized.DailyTime, Cron: normalized.Cron,
		Timezone: normalized.Timezone, WarningMinutes: normalized.WarningMinutes,
		WarningTitle: normalized.WarningTitle, WarningMessage: normalized.WarningMessage,
		LocationOverride: normalized.LocationOverride, Enabled: normalized.Enabled,
		Metadata: normalized.Metadata, CreatedAt: now, UpdatedAt: now,
	}
	if normalized.Enabled {
		item.NextRunAt = next.UTC().Format(time.RFC3339Nano)
	}
	locationJSON, metadataJSON, err := encodeSchedulePayloads(item.LocationOverride, item.Metadata)
	if err != nil {
		return Schedule{}, ErrInvalidSchedule
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO boss_schedules(id,name,template_id,mode,daily_time,cron_expr,timezone,warning_minutes,warning_title,warning_message,location_override_json,enabled,metadata_json,next_run_at,last_run_at,last_warning_at,created_at,updated_at,archived_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.Name, item.TemplateID, item.Mode, item.DailyTime, item.Cron, item.Timezone,
		item.WarningMinutes, item.WarningTitle, item.WarningMessage, locationJSON, boolInt(item.Enabled), metadataJSON,
		item.NextRunAt, "", "", item.CreatedAt, item.UpdatedAt, "")
	if err != nil {
		return Schedule{}, err
	}
	return s.GetSchedule(ctx, item.ID)
}

func (s *Service) UpdateSchedule(ctx context.Context, id string, input ScheduleInput) (Schedule, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Schedule{}, ErrScheduleNotFound
	}
	current, err := s.GetSchedule(ctx, id)
	if err != nil {
		return Schedule{}, err
	}
	if current.ArchivedAt != "" {
		return Schedule{}, ErrScheduleNotFound
	}
	normalized, next, err := s.normalizeScheduleInput(ctx, input, s.now())
	if err != nil {
		return Schedule{}, err
	}
	locationJSON, metadataJSON, err := encodeSchedulePayloads(normalized.LocationOverride, normalized.Metadata)
	if err != nil {
		return Schedule{}, ErrInvalidSchedule
	}
	nextRun := ""
	if normalized.Enabled {
		nextRun = next.UTC().Format(time.RFC3339Nano)
	}
	now := s.timestamp()
	result, err := s.db.ExecContext(ctx, `UPDATE boss_schedules SET name=?,template_id=?,mode=?,daily_time=?,cron_expr=?,timezone=?,warning_minutes=?,warning_title=?,warning_message=?,location_override_json=?,enabled=?,metadata_json=?,next_run_at=?,last_warning_at='',updated_at=? WHERE id=? AND archived_at=''`,
		normalized.Name, normalized.TemplateID, normalized.Mode, normalized.DailyTime, normalized.Cron, normalized.Timezone,
		normalized.WarningMinutes, normalized.WarningTitle, normalized.WarningMessage, locationJSON, boolInt(normalized.Enabled),
		metadataJSON, nextRun, now, id)
	if err != nil {
		return Schedule{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return Schedule{}, ErrScheduleNotFound
	}
	return s.GetSchedule(ctx, id)
}

func (s *Service) ArchiveSchedule(ctx context.Context, id string) (Schedule, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Schedule{}, ErrScheduleNotFound
	}
	now := s.timestamp()
	result, err := s.db.ExecContext(ctx, `UPDATE boss_schedules SET enabled=0,next_run_at='',updated_at=?,archived_at=? WHERE id=? AND archived_at=''`, now, now, id)
	if err != nil {
		return Schedule{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return Schedule{}, ErrScheduleNotFound
	}
	return s.GetSchedule(ctx, id)
}

func (s *Service) GetSchedule(ctx context.Context, id string) (Schedule, error) {
	item, err := scanSchedule(s.db.QueryRowContext(ctx, scheduleSelect+` WHERE s.id=?`, strings.TrimSpace(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return Schedule{}, ErrScheduleNotFound
	}
	return item, err
}

func (s *Service) ListSchedules(ctx context.Context, filter ScheduleFilter) ([]Schedule, error) {
	filter.Enabled = strings.ToLower(strings.TrimSpace(filter.Enabled))
	if filter.Enabled != "" && filter.Enabled != "true" && filter.Enabled != "false" {
		return nil, ErrInvalidSchedule
	}
	filter.Limit, filter.Offset = normalizePage(filter.Limit, filter.Offset)
	query := scheduleSelect + ` WHERE 1=1`
	args := []any{}
	if !filter.IncludeArchived {
		query += ` AND s.archived_at=''`
	}
	if filter.Enabled != "" {
		query += ` AND s.enabled=?`
		args = append(args, map[bool]int{true: 1, false: 0}[filter.Enabled == "true"])
	}
	query += ` ORDER BY s.enabled DESC,s.next_run_at ASC,s.updated_at DESC LIMIT ? OFFSET ?`
	args = append(args, filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Schedule{}
	for rows.Next() {
		item, scanErr := scanSchedule(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) RunScheduleNow(ctx context.Context, id, actor string) (SummonResult, error) {
	schedule, err := s.GetSchedule(ctx, id)
	if err != nil {
		return SummonResult{}, err
	}
	if schedule.ArchivedAt != "" || !schedule.Enabled {
		return SummonResult{}, ErrScheduleDisabled
	}
	planned := s.now().UTC()
	request := scheduledSummonRequest(schedule, planned, "manual")
	request.RequestKey = "boss-schedule-now:" + schedule.ID + ":" + newID("run")
	result, err := s.CreateSummon(ctx, request, strings.TrimSpace(actor))
	status := ScheduleEventSuccess
	message := "manual schedule run created"
	if err != nil {
		status = ScheduleEventFailed
		message = err.Error()
	}
	summonID := ""
	if err == nil {
		summonID = result.Summon.ID
	}
	_ = s.insertScheduleEvent(ctx, schedule.ID, ScheduleEventSummon, status, planned, summonID, actor, message, map[string]any{"manual": true})
	if err != nil {
		return SummonResult{}, err
	}
	return result, nil
}

func (s *Service) RunDueSchedules(ctx context.Context, now time.Time, actor string) (RunDueResult, error) {
	now = now.UTC()
	items, err := s.ListSchedules(ctx, ScheduleFilter{Enabled: "true", Limit: maximumLimit})
	if err != nil {
		return RunDueResult{}, err
	}
	result := RunDueResult{Checked: len(items)}
	for _, schedule := range items {
		if schedule.NextRunAt == "" {
			result.Skipped++
			continue
		}
		planned, parseErr := time.Parse(time.RFC3339Nano, schedule.NextRunAt)
		if parseErr != nil {
			result.Failed++
			continue
		}
		if schedule.WarningMinutes > 0 {
			warningAt := planned.Add(-time.Duration(schedule.WarningMinutes) * time.Minute)
			if !now.Before(warningAt) && now.Before(planned) {
				inserted, insertErr := s.insertScheduleEventIfAbsent(ctx, schedule.ID, ScheduleEventWarning, ScheduleEventSuccess, planned, "", actor, scheduleWarningMessage(schedule), map[string]any{
					"title": schedule.WarningTitle, "message": schedule.WarningMessage,
					"warning_minutes": schedule.WarningMinutes,
				})
				if insertErr != nil {
					result.Failed++
				} else if inserted {
					result.Warnings++
					_, _ = s.db.ExecContext(ctx, `UPDATE boss_schedules SET last_warning_at=?,updated_at=? WHERE id=?`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), schedule.ID)
				}
			}
		}
		if now.Before(planned) {
			continue
		}
		request := scheduledSummonRequest(schedule, planned, "scheduled")
		summonResult, runErr := s.CreateSummon(ctx, request, actor)
		eventStatus := ScheduleEventSuccess
		message := "scheduled summon record created"
		summonID := ""
		if runErr != nil {
			eventStatus = ScheduleEventFailed
			message = runErr.Error()
			result.Failed++
		} else {
			summonID = summonResult.Summon.ID
			result.Summons++
		}
		_, eventErr := s.insertScheduleEventIfAbsent(ctx, schedule.ID, ScheduleEventSummon, eventStatus, planned, summonID, actor, message, map[string]any{
			"duplicate": runErr == nil && summonResult.Duplicate,
		})
		if eventErr != nil {
			result.Failed++
		}
		next, nextErr := nextScheduleTime(schedule, now)
		nextRun := ""
		if nextErr == nil {
			nextRun = next.UTC().Format(time.RFC3339Nano)
		} else {
			result.Failed++
		}
		_, updateErr := s.db.ExecContext(ctx, `UPDATE boss_schedules SET next_run_at=?,last_run_at=?,last_warning_at='',updated_at=? WHERE id=? AND next_run_at=?`,
			nextRun, planned.UTC().Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), schedule.ID, schedule.NextRunAt)
		if updateErr != nil {
			result.Failed++
		}
	}
	return result, nil
}

func (s *Service) ListScheduleEvents(ctx context.Context, filter ScheduleEventFilter) ([]ScheduleEvent, error) {
	filter.ScheduleID = strings.TrimSpace(filter.ScheduleID)
	filter.EventType = strings.ToLower(strings.TrimSpace(filter.EventType))
	filter.Status = strings.ToLower(strings.TrimSpace(filter.Status))
	if filter.EventType != "" && filter.EventType != ScheduleEventWarning && filter.EventType != ScheduleEventSummon {
		return nil, ErrInvalidSchedule
	}
	if filter.Status != "" && filter.Status != ScheduleEventSuccess && filter.Status != ScheduleEventFailed && filter.Status != ScheduleEventSkipped {
		return nil, ErrInvalidSchedule
	}
	filter.Limit, filter.Offset = normalizePage(filter.Limit, filter.Offset)
	query := `SELECT id,schedule_id,event_type,status,planned_for,summon_id,actor,message,details_json,created_at FROM boss_schedule_events WHERE 1=1`
	args := []any{}
	if filter.ScheduleID != "" {
		query += ` AND schedule_id=?`
		args = append(args, filter.ScheduleID)
	}
	if filter.EventType != "" {
		query += ` AND event_type=?`
		args = append(args, filter.EventType)
	}
	if filter.Status != "" {
		query += ` AND status=?`
		args = append(args, filter.Status)
	}
	query += ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ScheduleEvent{}
	for rows.Next() {
		var item ScheduleEvent
		var details string
		if err := rows.Scan(&item.ID, &item.ScheduleID, &item.EventType, &item.Status, &item.PlannedFor, &item.SummonID, &item.Actor, &item.Message, &details, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Details = decodeObject(details)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) normalizeScheduleInput(ctx context.Context, input ScheduleInput, after time.Time) (ScheduleInput, time.Time, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.TemplateID = strings.TrimSpace(input.TemplateID)
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	input.DailyTime = strings.TrimSpace(input.DailyTime)
	input.Cron = strings.TrimSpace(input.Cron)
	input.Timezone = strings.TrimSpace(input.Timezone)
	input.WarningTitle = strings.TrimSpace(input.WarningTitle)
	input.WarningMessage = strings.TrimSpace(input.WarningMessage)
	if input.Name == "" || len(input.Name) > 128 || input.TemplateID == "" || len(input.WarningTitle) > 128 || len(input.WarningMessage) > 4096 {
		return ScheduleInput{}, time.Time{}, ErrInvalidSchedule
	}
	if input.WarningMinutes < 0 || input.WarningMinutes > maximumWarningMinutes {
		return ScheduleInput{}, time.Time{}, ErrInvalidSchedule
	}
	if input.Timezone == "" {
		input.Timezone = "Asia/Shanghai"
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return ScheduleInput{}, time.Time{}, ErrInvalidSchedule
	}
	template, err := s.GetTemplate(ctx, input.TemplateID)
	if err != nil {
		return ScheduleInput{}, time.Time{}, err
	}
	if template.ArchivedAt != "" {
		return ScheduleInput{}, time.Time{}, ErrTemplateNotFound
	}
	if input.Mode == ScheduleModeDaily {
		if _, err := time.Parse("15:04", input.DailyTime); err != nil {
			return ScheduleInput{}, time.Time{}, ErrInvalidSchedule
		}
		input.Cron = ""
	} else if input.Mode == ScheduleModeCron {
		if _, err := parseCron(input.Cron); err != nil {
			return ScheduleInput{}, time.Time{}, ErrInvalidSchedule
		}
		input.DailyTime = ""
	} else {
		return ScheduleInput{}, time.Time{}, ErrInvalidSchedule
	}
	if input.WarningMinutes > 0 {
		if input.WarningTitle == "" {
			input.WarningTitle = "Boss活动即将开始"
		}
		if input.WarningMessage == "" {
			input.WarningMessage = "Boss活动将在{{minutes}}分钟后开始，请提前前往活动区域。"
		}
	}
	if input.LocationOverride != nil {
		input.LocationOverride.Label = strings.TrimSpace(input.LocationOverride.Label)
		if len(input.LocationOverride.Label) > 128 || validateLocation(*input.LocationOverride) != nil {
			return ScheduleInput{}, time.Time{}, ErrInvalidSchedule
		}
	}
	if _, err := marshalBounded(input.Metadata); err != nil {
		return ScheduleInput{}, time.Time{}, ErrInvalidSchedule
	}
	input.Metadata = normalizedMap(input.Metadata)
	probe := Schedule{Mode: input.Mode, DailyTime: input.DailyTime, Cron: input.Cron, Timezone: input.Timezone}
	next, err := nextScheduleTime(probe, after)
	if err != nil {
		return ScheduleInput{}, time.Time{}, ErrInvalidSchedule
	}
	return input, next, nil
}

func scheduledSummonRequest(schedule Schedule, planned time.Time, source string) CreateSummonRequest {
	metadata := map[string]any{
		"schedule_id":   schedule.ID,
		"schedule_name": schedule.Name,
		"planned_for":   planned.UTC().Format(time.RFC3339Nano),
		"source":        source,
	}
	for key, value := range schedule.Metadata {
		metadata[key] = value
	}
	return CreateSummonRequest{
		TemplateID:       schedule.TemplateID,
		RequestKey:       fmt.Sprintf("boss-schedule:%s:%d", schedule.ID, planned.UTC().Unix()),
		LocationOverride: schedule.LocationOverride,
		Notes:            fmt.Sprintf("定时计划：%s；计划时间：%s", schedule.Name, planned.UTC().Format(time.RFC3339)),
		Metadata:         metadata,
	}
}

func scheduleWarningMessage(schedule Schedule) string {
	message := schedule.WarningMessage
	message = strings.ReplaceAll(message, "{{minutes}}", strconv.Itoa(schedule.WarningMinutes))
	message = strings.ReplaceAll(message, "{{schedule}}", schedule.Name)
	message = strings.ReplaceAll(message, "{{boss}}", schedule.TemplateName)
	if schedule.WarningTitle == "" {
		return message
	}
	return schedule.WarningTitle + "：" + message
}

func (s *Service) insertScheduleEvent(ctx context.Context, scheduleID, eventType, status string, planned time.Time, summonID, actor, message string, details map[string]any) error {
	body, _ := json.Marshal(normalizedMap(details))
	_, err := s.db.ExecContext(ctx, `INSERT INTO boss_schedule_events(schedule_id,event_type,status,planned_for,summon_id,actor,message,details_json,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		scheduleID, eventType, status, planned.UTC().Format(time.RFC3339Nano), summonID, strings.TrimSpace(actor), strings.TrimSpace(message), string(body), s.timestamp())
	return err
}

func (s *Service) insertScheduleEventIfAbsent(ctx context.Context, scheduleID, eventType, status string, planned time.Time, summonID, actor, message string, details map[string]any) (bool, error) {
	body, _ := json.Marshal(normalizedMap(details))
	result, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO boss_schedule_events(schedule_id,event_type,status,planned_for,summon_id,actor,message,details_json,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		scheduleID, eventType, status, planned.UTC().Format(time.RFC3339Nano), summonID, strings.TrimSpace(actor), strings.TrimSpace(message), string(body), s.timestamp())
	if err != nil {
		return false, err
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

func encodeSchedulePayloads(location *Location, metadata map[string]any) (string, string, error) {
	locationJSON := ""
	if location != nil {
		body, err := json.Marshal(location)
		if err != nil || len(body) > maximumJSON {
			return "", "", ErrInvalidSchedule
		}
		locationJSON = string(body)
	}
	metadataBody, err := marshalBounded(metadata)
	if err != nil {
		return "", "", err
	}
	return locationJSON, string(metadataBody), nil
}

const scheduleSelect = `SELECT s.id,s.name,s.template_id,t.name,s.mode,s.daily_time,s.cron_expr,s.timezone,s.warning_minutes,s.warning_title,s.warning_message,s.location_override_json,s.enabled,s.metadata_json,s.next_run_at,s.last_run_at,s.last_warning_at,s.created_at,s.updated_at,s.archived_at FROM boss_schedules s LEFT JOIN boss_templates t ON t.id=s.template_id`

func scanSchedule(scanner interface{ Scan(...any) error }) (Schedule, error) {
	var item Schedule
	var templateName sql.NullString
	var locationJSON, metadataJSON string
	var enabled int
	if err := scanner.Scan(&item.ID, &item.Name, &item.TemplateID, &templateName, &item.Mode, &item.DailyTime,
		&item.Cron, &item.Timezone, &item.WarningMinutes, &item.WarningTitle, &item.WarningMessage,
		&locationJSON, &enabled, &metadataJSON, &item.NextRunAt, &item.LastRunAt, &item.LastWarningAt,
		&item.CreatedAt, &item.UpdatedAt, &item.ArchivedAt); err != nil {
		return Schedule{}, err
	}
	item.TemplateName = templateName.String
	item.Enabled = enabled == 1
	item.Metadata = decodeObject(metadataJSON)
	if strings.TrimSpace(locationJSON) != "" {
		var location Location
		if json.Unmarshal([]byte(locationJSON), &location) == nil {
			item.LocationOverride = &location
		}
	}
	return item, nil
}

func nextScheduleTime(schedule Schedule, after time.Time) (time.Time, error) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	localAfter := after.In(location)
	if schedule.Mode == ScheduleModeDaily {
		parsed, err := time.Parse("15:04", schedule.DailyTime)
		if err != nil {
			return time.Time{}, err
		}
		candidate := time.Date(localAfter.Year(), localAfter.Month(), localAfter.Day(), parsed.Hour(), parsed.Minute(), 0, 0, location)
		if !candidate.After(localAfter) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		return candidate.UTC(), nil
	}
	if schedule.Mode != ScheduleModeCron {
		return time.Time{}, ErrInvalidSchedule
	}
	expression, err := parseCron(schedule.Cron)
	if err != nil {
		return time.Time{}, err
	}
	candidate := localAfter.Truncate(time.Minute).Add(time.Minute)
	limit := candidate.AddDate(1, 0, 7)
	for candidate.Before(limit) {
		if expression.matches(candidate) {
			return candidate.UTC(), nil
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, ErrInvalidSchedule
}

type cronExpression struct {
	minute  map[int]bool
	hour    map[int]bool
	day     map[int]bool
	month   map[int]bool
	weekday map[int]bool
	dayAny  bool
	weekAny bool
}

func parseCron(value string) (cronExpression, error) {
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) != 5 {
		return cronExpression{}, ErrInvalidSchedule
	}
	minute, minuteAny, err := parseCronField(parts[0], 0, 59, false)
	if err != nil {
		return cronExpression{}, err
	}
	hour, hourAny, err := parseCronField(parts[1], 0, 23, false)
	if err != nil {
		return cronExpression{}, err
	}
	day, dayAny, err := parseCronField(parts[2], 1, 31, false)
	if err != nil {
		return cronExpression{}, err
	}
	month, monthAny, err := parseCronField(parts[3], 1, 12, false)
	if err != nil {
		return cronExpression{}, err
	}
	weekday, weekAny, err := parseCronField(parts[4], 0, 7, true)
	if err != nil {
		return cronExpression{}, err
	}
	_ = minuteAny
	_ = hourAny
	_ = monthAny
	return cronExpression{minute: minute, hour: hour, day: day, month: month, weekday: weekday, dayAny: dayAny, weekAny: weekAny}, nil
}

func parseCronField(value string, minimum, maximum int, normalizeSunday bool) (map[int]bool, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false, ErrInvalidSchedule
	}
	result := map[int]bool{}
	isAny := value == "*"
	for _, token := range strings.Split(value, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			return nil, false, ErrInvalidSchedule
		}
		step := 1
		base := token
		if strings.Contains(token, "/") {
			pieces := strings.Split(token, "/")
			if len(pieces) != 2 {
				return nil, false, ErrInvalidSchedule
			}
			base = pieces[0]
			parsed, err := strconv.Atoi(pieces[1])
			if err != nil || parsed < 1 || parsed > maximum-minimum+1 {
				return nil, false, ErrInvalidSchedule
			}
			step = parsed
		}
		start, end := minimum, maximum
		if base != "*" {
			if strings.Contains(base, "-") {
				pieces := strings.Split(base, "-")
				if len(pieces) != 2 {
					return nil, false, ErrInvalidSchedule
				}
				var err error
				start, err = strconv.Atoi(pieces[0])
				if err != nil {
					return nil, false, ErrInvalidSchedule
				}
				end, err = strconv.Atoi(pieces[1])
				if err != nil {
					return nil, false, ErrInvalidSchedule
				}
			} else {
				parsed, err := strconv.Atoi(base)
				if err != nil {
					return nil, false, ErrInvalidSchedule
				}
				start, end = parsed, parsed
			}
		}
		if start < minimum || end > maximum || start > end {
			return nil, false, ErrInvalidSchedule
		}
		for number := start; number <= end; number += step {
			normalized := number
			if normalizeSunday && normalized == 7 {
				normalized = 0
			}
			result[normalized] = true
		}
	}
	if len(result) == 0 {
		return nil, false, ErrInvalidSchedule
	}
	return result, isAny, nil
}

func (c cronExpression) matches(value time.Time) bool {
	if !c.minute[value.Minute()] || !c.hour[value.Hour()] || !c.month[int(value.Month())] {
		return false
	}
	dayMatch := c.day[value.Day()]
	weekMatch := c.weekday[int(value.Weekday())]
	if c.dayAny && c.weekAny {
		return true
	}
	if c.dayAny {
		return weekMatch
	}
	if c.weekAny {
		return dayMatch
	}
	return dayMatch || weekMatch
}

func sortSchedulesByNext(items []Schedule) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Enabled != items[j].Enabled {
			return items[i].Enabled
		}
		return items[i].NextRunAt < items[j].NextRunAt
	})
}
