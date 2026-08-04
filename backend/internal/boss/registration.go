package boss

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"palpanel/internal/playeridentity"
)

var (
	ErrInvalidRegistration      = errors.New("boss registration request is invalid")
	ErrRegistrationClosed       = errors.New("boss registration is closed")
	ErrRegistrationFull         = errors.New("boss registration has reached its player limit")
	ErrRegistrationPolicy       = errors.New("boss registration policy conflicts with current participants")
	ErrRegistrationBusy         = errors.New("boss registration is busy")
	ErrParticipantNotFound      = errors.New("boss participant not found")
	ErrParticipantStateConflict = errors.New("boss participant state conflicts with the requested operation")
)

const (
	ParticipantStatusRegistered = "registered"
	ParticipantStatusCheckedIn  = "checked_in"
	ParticipantStatusCancelled  = "cancelled"

	ParticipantAreaUnknown  = "unknown"
	ParticipantAreaEligible = "eligible"
	ParticipantAreaOutside  = "outside"
	ParticipantAreaDisabled = "disabled"

	registrationActionConfigure = "registration_configure"
	registrationActionSnapshot  = "registration_snapshot"
	registrationActionRegister  = "participant_register"
	registrationActionCheckArea = "participant_check_area"
	registrationActionCancel    = "participant_cancel"

	registrationLeaseDuration = 15 * time.Second
)

type RegistrationPolicyUpdate struct {
	Enabled     *bool    `json:"enabled,omitempty"`
	MaxPlayers  *int     `json:"max_players,omitempty"`
	Radius      *float64 `json:"radius,omitempty"`
	UseZ        *bool    `json:"use_z,omitempty"`
	AllowActive *bool    `json:"allow_active,omitempty"`
}

type RegistrationPolicy struct {
	SummonID      string   `json:"summon_id"`
	Enabled       bool     `json:"enabled"`
	Open          bool     `json:"open"`
	AllowActive   bool     `json:"allow_active"`
	MaxPlayers    int      `json:"max_players"`
	Registered    int      `json:"registered"`
	CheckedIn     int      `json:"checked_in"`
	Outside       int      `json:"outside"`
	Available     int      `json:"available"`
	Radius        float64  `json:"radius"`
	UseZ          bool     `json:"use_z"`
	AreaRequired  bool     `json:"area_required"`
	Center        Location `json:"center"`
	SummonStatus  string   `json:"summon_status"`
	Configuration string   `json:"configuration"`
}

type ParticipantInput struct {
	PlayerUID string         `json:"player_uid"`
	Nickname  string         `json:"nickname,omitempty"`
	SteamID   string         `json:"steam_id,omitempty"`
	Location  *Location      `json:"location,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type ParticipantAreaInput struct {
	PlayerUID string         `json:"player_uid"`
	Location  *Location      `json:"location"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type ParticipantCancelInput struct {
	PlayerUID string `json:"player_uid"`
	Reason    string `json:"reason,omitempty"`
}

type Participant struct {
	ID           int64          `json:"id"`
	SummonID     string         `json:"summon_id"`
	PlayerUID    string         `json:"player_uid"`
	Nickname     string         `json:"nickname,omitempty"`
	SteamID      string         `json:"steam_id,omitempty"`
	Status       string         `json:"status"`
	AreaStatus   string         `json:"area_status"`
	Eligible     bool           `json:"eligible"`
	Location     *Location      `json:"location,omitempty"`
	Distance     float64        `json:"distance"`
	Actor        string         `json:"actor,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	RegisteredAt string         `json:"registered_at"`
	CheckedAt    string         `json:"checked_at,omitempty"`
	CancelledAt  string         `json:"cancelled_at,omitempty"`
	UpdatedAt    string         `json:"updated_at"`
}

type ParticipantResult struct {
	Participant Participant        `json:"participant"`
	Policy      RegistrationPolicy `json:"policy"`
	Duplicate   bool               `json:"duplicate"`
}

type ParticipantEvent struct {
	ID        int64          `json:"id"`
	SummonID  string         `json:"summon_id"`
	PlayerUID string         `json:"player_uid"`
	EventType string         `json:"event_type"`
	Actor     string         `json:"actor,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
	CreatedAt string         `json:"created_at"`
}

type RegistrationSnapshot struct {
	Policy       RegistrationPolicy `json:"policy"`
	Participants []Participant      `json:"participants"`
	Events       []ParticipantEvent `json:"events"`
	Count        int                `json:"count"`
	EventCount   int                `json:"event_count"`
}

func IsRegistrationControlAction(action string) bool {
	switch normalizeRegistrationAction(action) {
	case registrationActionConfigure, registrationActionSnapshot, registrationActionRegister, registrationActionCheckArea, registrationActionCancel:
		return true
	default:
		return false
	}
}

func normalizeRegistrationAction(action string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(action)), "-", "_")
}

func (s *Service) ensureRegistrationSchema(ctx context.Context) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS boss_registration_leases (
			summon_id TEXT PRIMARY KEY,
			holder TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS boss_summon_participants (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			summon_id TEXT NOT NULL,
			player_uid TEXT NOT NULL COLLATE PALPLAYERUID,
			nickname TEXT NOT NULL DEFAULT '',
			steam_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL CHECK(status IN ('registered','checked_in','cancelled')),
			area_status TEXT NOT NULL CHECK(area_status IN ('unknown','eligible','outside','disabled')),
			location_json TEXT NOT NULL DEFAULT '{}',
			distance REAL NOT NULL DEFAULT 0,
			actor TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			registered_at TEXT NOT NULL,
			checked_at TEXT NOT NULL DEFAULT '',
			cancelled_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			UNIQUE(summon_id,player_uid),
			FOREIGN KEY(summon_id) REFERENCES boss_summons(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_participants_summon_status ON boss_summon_participants(summon_id,status,registered_at)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_participants_player ON boss_summon_participants(player_uid,updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS boss_participant_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			summon_id TEXT NOT NULL,
			player_uid TEXT NOT NULL COLLATE PALPLAYERUID,
			event_type TEXT NOT NULL,
			actor TEXT NOT NULL DEFAULT '',
			details_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			FOREIGN KEY(summon_id) REFERENCES boss_summons(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_participant_events_summon ON boss_participant_events(summon_id,id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_participant_events_player ON boss_participant_events(player_uid,id DESC)`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure boss registration schema: %w", err)
		}
	}
	return nil
}

func (s *Service) acquireRegistrationLease(ctx context.Context, summonID, holder string) (bool, error) {
	summonID = strings.TrimSpace(summonID)
	holder = strings.TrimSpace(holder)
	if summonID == "" || holder == "" {
		return false, ErrInvalidRegistration
	}
	if err := s.ensureRegistrationSchema(ctx); err != nil {
		return false, err
	}
	now := s.now().UTC()
	updatedAt := now.Format(time.RFC3339Nano)
	expiresAt := now.Add(registrationLeaseDuration).Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `INSERT INTO boss_registration_leases(summon_id,holder,expires_at,updated_at)
		VALUES(?,?,?,?)
		ON CONFLICT(summon_id) DO UPDATE SET
		 holder=excluded.holder,expires_at=excluded.expires_at,updated_at=excluded.updated_at
		WHERE boss_registration_leases.holder=excluded.holder
		 OR boss_registration_leases.expires_at<=excluded.updated_at`, summonID, holder, expiresAt, updatedAt)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func (s *Service) releaseRegistrationLease(ctx context.Context, summonID, holder string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM boss_registration_leases WHERE summon_id=? AND holder=?`, strings.TrimSpace(summonID), strings.TrimSpace(holder))
	return err
}

func (s *Service) ConfigureRegistration(ctx context.Context, summonID string, update RegistrationPolicyUpdate, actor string) (RegistrationSnapshot, error) {
	if err := s.ensureRegistrationSchema(ctx); err != nil {
		return RegistrationSnapshot{}, err
	}
	summonID = strings.TrimSpace(summonID)
	actor = strings.TrimSpace(actor)
	if summonID == "" {
		return RegistrationSnapshot{}, ErrInvalidRegistration
	}
	holder := newID("boss_registration")
	acquired, leaseErr := s.acquireRegistrationLease(ctx, strings.TrimSpace(summonID), holder)
	if leaseErr != nil {
		return RegistrationSnapshot{}, leaseErr
	}
	if !acquired {
		return RegistrationSnapshot{}, ErrRegistrationBusy
	}
	defer func() { _ = s.releaseRegistrationLease(context.Background(), strings.TrimSpace(summonID), holder) }()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RegistrationSnapshot{}, err
	}
	defer rollback(tx)
	summon, err := getSummonTx(ctx, tx, summonID)
	if errors.Is(err, sql.ErrNoRows) {
		return RegistrationSnapshot{}, ErrSummonNotFound
	}
	if err != nil {
		return RegistrationSnapshot{}, err
	}
	if terminalStatus(summon.Status) {
		return RegistrationSnapshot{}, ErrRegistrationClosed
	}
	registered, checkedIn, outside, err := participantCountsTx(ctx, tx, summonID)
	if err != nil {
		return RegistrationSnapshot{}, err
	}
	policy := registrationPolicy(summon, registered, checkedIn, outside)
	if update.Enabled != nil {
		policy.Enabled = *update.Enabled
	}
	if update.MaxPlayers != nil {
		policy.MaxPlayers = *update.MaxPlayers
	}
	if update.Radius != nil {
		policy.Radius = *update.Radius
	}
	if update.UseZ != nil {
		policy.UseZ = *update.UseZ
	}
	if update.AllowActive != nil {
		policy.AllowActive = *update.AllowActive
	}
	if policy.MaxPlayers < 0 || policy.MaxPlayers > 1000 || math.IsNaN(policy.Radius) || math.IsInf(policy.Radius, 0) || policy.Radius < 0 || policy.Radius > 10_000_000 {
		return RegistrationSnapshot{}, ErrInvalidRegistration
	}
	if policy.MaxPlayers > 0 && registered > policy.MaxPlayers {
		return RegistrationSnapshot{}, ErrRegistrationPolicy
	}
	metadata := normalizedMap(summon.Metadata)
	metadata["registration_enabled"] = policy.Enabled
	metadata["registration_max_players"] = policy.MaxPlayers
	metadata["registration_radius"] = policy.Radius
	metadata["registration_use_z"] = policy.UseZ
	metadata["registration_allow_active"] = policy.AllowActive
	metadataJSON, err := marshalBounded(metadata)
	if err != nil {
		return RegistrationSnapshot{}, ErrInvalidRegistration
	}
	now := s.timestamp()
	if _, err := tx.ExecContext(ctx, `UPDATE boss_summons SET metadata_json=?,updated_at=? WHERE id=?`, string(metadataJSON), now, summonID); err != nil {
		return RegistrationSnapshot{}, err
	}
	if err := reconcileParticipantAreasTx(ctx, tx, policy, actor, now); err != nil {
		return RegistrationSnapshot{}, err
	}
	if err := insertSummonEvent(ctx, tx, summonID, summon.Status, summon.Status, actor, "boss registration policy updated", map[string]any{
		"registration_enabled": policy.Enabled, "registration_max_players": policy.MaxPlayers,
		"registration_radius": policy.Radius, "registration_use_z": policy.UseZ,
		"registration_allow_active": policy.AllowActive,
	}, now); err != nil {
		return RegistrationSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return RegistrationSnapshot{}, err
	}
	return s.RegistrationSnapshot(ctx, summonID, false)
}

func (s *Service) RegisterParticipant(ctx context.Context, summonID string, input ParticipantInput, actor string) (ParticipantResult, error) {
	if err := s.ensureRegistrationSchema(ctx); err != nil {
		return ParticipantResult{}, err
	}
	input.PlayerUID = playeridentity.Normalize(input.PlayerUID)
	input.Nickname = strings.TrimSpace(input.Nickname)
	input.SteamID = playeridentity.NormalizeSteamID(input.SteamID)
	actor = strings.TrimSpace(actor)
	if strings.TrimSpace(summonID) == "" || input.PlayerUID == "" || len(input.PlayerUID) > 128 || len(input.Nickname) > 128 || len(input.SteamID) > 128 {
		return ParticipantResult{}, ErrInvalidRegistration
	}
	if _, err := marshalBounded(input.Metadata); err != nil {
		return ParticipantResult{}, ErrInvalidRegistration
	}
	if input.Location != nil {
		if err := validateRegistrationLocation(*input.Location); err != nil {
			return ParticipantResult{}, err
		}
	}
	holder := newID("boss_registration")
	acquired, leaseErr := s.acquireRegistrationLease(ctx, strings.TrimSpace(summonID), holder)
	if leaseErr != nil {
		return ParticipantResult{}, leaseErr
	}
	if !acquired {
		return ParticipantResult{}, ErrRegistrationBusy
	}
	defer func() { _ = s.releaseRegistrationLease(context.Background(), strings.TrimSpace(summonID), holder) }()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ParticipantResult{}, err
	}
	defer rollback(tx)
	summon, policy, err := registrationContextTx(ctx, tx, strings.TrimSpace(summonID))
	if err != nil {
		return ParticipantResult{}, err
	}
	if !policy.Open {
		return ParticipantResult{}, ErrRegistrationClosed
	}
	existing, existingErr := participantTx(ctx, tx, summon.ID, input.PlayerUID)
	duplicate := existingErr == nil && existing.Status != ParticipantStatusCancelled
	if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
		return ParticipantResult{}, existingErr
	}
	if !duplicate && policy.MaxPlayers > 0 && policy.Registered >= policy.MaxPlayers {
		return ParticipantResult{}, ErrRegistrationFull
	}
	areaStatus, distance, eligible := participantArea(policy, input.Location)
	status := ParticipantStatusRegistered
	checkedAt := ""
	if eligible {
		status = ParticipantStatusCheckedIn
		checkedAt = s.timestamp()
	}
	now := s.timestamp()
	registeredAt := now
	locationJSON := "{}"
	metadata := normalizedMap(input.Metadata)
	if existingErr == nil {
		if existing.RegisteredAt != "" {
			registeredAt = existing.RegisteredAt
		}
		merged := normalizedMap(existing.Metadata)
		for key, value := range input.Metadata {
			merged[key] = value
		}
		metadata = merged
		if duplicate && input.Location == nil {
			areaStatus = existing.AreaStatus
			distance = existing.Distance
			eligible = existing.Eligible
			status = existing.Status
			checkedAt = existing.CheckedAt
			if existing.Location != nil {
				body, _ := json.Marshal(existing.Location)
				locationJSON = string(body)
			}
		}
	}
	if input.Location != nil {
		body, _ := json.Marshal(input.Location)
		locationJSON = string(body)
	}
	metadataJSON, _ := json.Marshal(metadata)
	_, err = tx.ExecContext(ctx, `INSERT INTO boss_summon_participants(summon_id,player_uid,nickname,steam_id,status,area_status,location_json,distance,actor,metadata_json,registered_at,checked_at,cancelled_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(summon_id,player_uid) DO UPDATE SET
		 nickname=CASE WHEN excluded.nickname<>'' THEN excluded.nickname ELSE boss_summon_participants.nickname END,
		 steam_id=CASE WHEN excluded.steam_id<>'' THEN excluded.steam_id ELSE boss_summon_participants.steam_id END,
		 status=excluded.status,area_status=excluded.area_status,location_json=excluded.location_json,distance=excluded.distance,
		 actor=excluded.actor,metadata_json=excluded.metadata_json,checked_at=excluded.checked_at,cancelled_at='',updated_at=excluded.updated_at`,
		summon.ID, input.PlayerUID, input.Nickname, input.SteamID, status, areaStatus, locationJSON, distance,
		actor, string(metadataJSON), registeredAt, checkedAt, "", now)
	if err != nil {
		return ParticipantResult{}, err
	}
	if err := insertParticipantEvent(ctx, tx, summon.ID, input.PlayerUID, "registered", actor, map[string]any{
		"duplicate": duplicate, "status": status, "area_status": areaStatus, "eligible": eligible,
		"distance": distance, "max_players": policy.MaxPlayers,
	}, now); err != nil {
		return ParticipantResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ParticipantResult{}, err
	}
	participant, err := s.GetParticipant(ctx, summon.ID, input.PlayerUID)
	if err != nil {
		return ParticipantResult{}, err
	}
	snapshot, err := s.RegistrationSnapshot(ctx, summon.ID, false)
	if err != nil {
		return ParticipantResult{}, err
	}
	return ParticipantResult{Participant: participant, Policy: snapshot.Policy, Duplicate: duplicate}, nil
}

func (s *Service) CheckParticipantArea(ctx context.Context, summonID string, input ParticipantAreaInput, actor string) (ParticipantResult, error) {
	if err := s.ensureRegistrationSchema(ctx); err != nil {
		return ParticipantResult{}, err
	}
	input.PlayerUID = playeridentity.Normalize(input.PlayerUID)
	actor = strings.TrimSpace(actor)
	if strings.TrimSpace(summonID) == "" || input.PlayerUID == "" {
		return ParticipantResult{}, ErrInvalidRegistration
	}
	if input.Location == nil {
		return ParticipantResult{}, ErrInvalidRegistration
	}
	if err := validateRegistrationLocation(*input.Location); err != nil {
		return ParticipantResult{}, err
	}
	if _, err := marshalBounded(input.Metadata); err != nil {
		return ParticipantResult{}, ErrInvalidRegistration
	}
	holder := newID("boss_registration")
	acquired, leaseErr := s.acquireRegistrationLease(ctx, strings.TrimSpace(summonID), holder)
	if leaseErr != nil {
		return ParticipantResult{}, leaseErr
	}
	if !acquired {
		return ParticipantResult{}, ErrRegistrationBusy
	}
	defer func() { _ = s.releaseRegistrationLease(context.Background(), strings.TrimSpace(summonID), holder) }()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ParticipantResult{}, err
	}
	defer rollback(tx)
	summon, policy, err := registrationContextTx(ctx, tx, strings.TrimSpace(summonID))
	if err != nil {
		return ParticipantResult{}, err
	}
	participant, err := participantTx(ctx, tx, summon.ID, input.PlayerUID)
	if errors.Is(err, sql.ErrNoRows) {
		return ParticipantResult{}, ErrParticipantNotFound
	}
	if err != nil {
		return ParticipantResult{}, err
	}
	if participant.Status == ParticipantStatusCancelled {
		return ParticipantResult{}, ErrParticipantStateConflict
	}
	areaStatus, distance, eligible := participantArea(policy, input.Location)
	status := ParticipantStatusRegistered
	checkedAt := ""
	if eligible {
		status = ParticipantStatusCheckedIn
		checkedAt = s.timestamp()
	}
	locationJSON, _ := json.Marshal(input.Location)
	metadata := participant.Metadata
	for key, value := range input.Metadata {
		metadata[key] = value
	}
	metadataJSON, _ := json.Marshal(metadata)
	now := s.timestamp()
	if _, err := tx.ExecContext(ctx, `UPDATE boss_summon_participants SET status=?,area_status=?,location_json=?,distance=?,actor=?,metadata_json=?,checked_at=?,updated_at=? WHERE summon_id=? AND player_uid=?`,
		status, areaStatus, string(locationJSON), distance, actor, string(metadataJSON), checkedAt, now, summon.ID, input.PlayerUID); err != nil {
		return ParticipantResult{}, err
	}
	if err := insertParticipantEvent(ctx, tx, summon.ID, input.PlayerUID, "area_checked", actor, map[string]any{
		"status": status, "area_status": areaStatus, "eligible": eligible, "distance": distance,
		"radius": policy.Radius, "use_z": policy.UseZ,
	}, now); err != nil {
		return ParticipantResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ParticipantResult{}, err
	}
	updated, err := s.GetParticipant(ctx, summon.ID, input.PlayerUID)
	if err != nil {
		return ParticipantResult{}, err
	}
	snapshot, err := s.RegistrationSnapshot(ctx, summon.ID, false)
	if err != nil {
		return ParticipantResult{}, err
	}
	return ParticipantResult{Participant: updated, Policy: snapshot.Policy}, nil
}

func (s *Service) CancelParticipant(ctx context.Context, summonID string, input ParticipantCancelInput, actor string) (ParticipantResult, error) {
	if err := s.ensureRegistrationSchema(ctx); err != nil {
		return ParticipantResult{}, err
	}
	input.PlayerUID = playeridentity.Normalize(input.PlayerUID)
	input.Reason = strings.TrimSpace(input.Reason)
	actor = strings.TrimSpace(actor)
	if strings.TrimSpace(summonID) == "" || input.PlayerUID == "" || len(input.Reason) > 4096 {
		return ParticipantResult{}, ErrInvalidRegistration
	}
	holder := newID("boss_registration")
	acquired, leaseErr := s.acquireRegistrationLease(ctx, strings.TrimSpace(summonID), holder)
	if leaseErr != nil {
		return ParticipantResult{}, leaseErr
	}
	if !acquired {
		return ParticipantResult{}, ErrRegistrationBusy
	}
	defer func() { _ = s.releaseRegistrationLease(context.Background(), strings.TrimSpace(summonID), holder) }()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ParticipantResult{}, err
	}
	defer rollback(tx)
	summon, _, err := registrationContextTx(ctx, tx, strings.TrimSpace(summonID))
	if err != nil {
		return ParticipantResult{}, err
	}
	participant, err := participantTx(ctx, tx, summon.ID, input.PlayerUID)
	if errors.Is(err, sql.ErrNoRows) {
		return ParticipantResult{}, ErrParticipantNotFound
	}
	if err != nil {
		return ParticipantResult{}, err
	}
	duplicate := participant.Status == ParticipantStatusCancelled
	now := s.timestamp()
	if !duplicate {
		if _, err := tx.ExecContext(ctx, `UPDATE boss_summon_participants SET status='cancelled',actor=?,cancelled_at=?,updated_at=? WHERE summon_id=? AND player_uid=?`, actor, now, now, summon.ID, input.PlayerUID); err != nil {
			return ParticipantResult{}, err
		}
		if err := insertParticipantEvent(ctx, tx, summon.ID, input.PlayerUID, "cancelled", actor, map[string]any{"reason": input.Reason}, now); err != nil {
			return ParticipantResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ParticipantResult{}, err
	}
	updated, err := s.GetParticipant(ctx, summon.ID, input.PlayerUID)
	if err != nil {
		return ParticipantResult{}, err
	}
	snapshot, err := s.RegistrationSnapshot(ctx, summon.ID, false)
	if err != nil {
		return ParticipantResult{}, err
	}
	return ParticipantResult{Participant: updated, Policy: snapshot.Policy, Duplicate: duplicate}, nil
}

func (s *Service) RegistrationSnapshot(ctx context.Context, summonID string, includeCancelled bool) (RegistrationSnapshot, error) {
	if err := s.ensureRegistrationSchema(ctx); err != nil {
		return RegistrationSnapshot{}, err
	}
	summon, err := s.GetSummon(ctx, strings.TrimSpace(summonID))
	if err != nil {
		return RegistrationSnapshot{}, err
	}
	registered, checkedIn, outside, err := participantCounts(ctx, s.db, summon.ID)
	if err != nil {
		return RegistrationSnapshot{}, err
	}
	policy := registrationPolicy(summon, registered, checkedIn, outside)
	query := participantSelect + ` WHERE summon_id=?`
	args := []any{summon.ID}
	if !includeCancelled {
		query += ` AND status<>'cancelled'`
	}
	query += ` ORDER BY CASE status WHEN 'checked_in' THEN 0 WHEN 'registered' THEN 1 ELSE 2 END,registered_at,id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return RegistrationSnapshot{}, err
	}
	items := []Participant{}
	for rows.Next() {
		participant, scanErr := scanParticipant(rows)
		if scanErr != nil {
			_ = rows.Close()
			return RegistrationSnapshot{}, scanErr
		}
		items = append(items, participant)
	}
	if err := rows.Close(); err != nil {
		return RegistrationSnapshot{}, err
	}
	if err := rows.Err(); err != nil {
		return RegistrationSnapshot{}, err
	}
	eventRows, err := s.db.QueryContext(ctx, `SELECT id,summon_id,player_uid,event_type,actor,details_json,created_at FROM boss_participant_events WHERE summon_id=? ORDER BY id DESC LIMIT 200`, summon.ID)
	if err != nil {
		return RegistrationSnapshot{}, err
	}
	events := []ParticipantEvent{}
	for eventRows.Next() {
		var event ParticipantEvent
		var detailsJSON string
		if err := eventRows.Scan(&event.ID, &event.SummonID, &event.PlayerUID, &event.EventType, &event.Actor, &detailsJSON, &event.CreatedAt); err != nil {
			_ = eventRows.Close()
			return RegistrationSnapshot{}, err
		}
		event.Details = decodeObject(detailsJSON)
		events = append(events, event)
	}
	if err := eventRows.Close(); err != nil {
		return RegistrationSnapshot{}, err
	}
	if err := eventRows.Err(); err != nil {
		return RegistrationSnapshot{}, err
	}
	return RegistrationSnapshot{Policy: policy, Participants: items, Events: events, Count: len(items), EventCount: len(events)}, nil
}

func (s *Service) GetParticipant(ctx context.Context, summonID, playerUID string) (Participant, error) {
	if err := s.ensureRegistrationSchema(ctx); err != nil {
		return Participant{}, err
	}
	participant, err := scanParticipant(s.db.QueryRowContext(ctx, participantSelect+` WHERE summon_id=? AND player_uid=?`, strings.TrimSpace(summonID), playeridentity.Normalize(playerUID)))
	if errors.Is(err, sql.ErrNoRows) {
		return Participant{}, ErrParticipantNotFound
	}
	return participant, err
}

func reconcileParticipantAreasTx(ctx context.Context, tx *sql.Tx, policy RegistrationPolicy, actor, now string) error {
	rows, err := tx.QueryContext(ctx, participantSelect+` WHERE summon_id=? AND status<>'cancelled' ORDER BY id`, policy.SummonID)
	if err != nil {
		return err
	}
	participants := []Participant{}
	for rows.Next() {
		participant, scanErr := scanParticipant(rows)
		if scanErr != nil {
			_ = rows.Close()
			return scanErr
		}
		participants = append(participants, participant)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, participant := range participants {
		areaStatus, distance, eligible := participantArea(policy, participant.Location)
		status := ParticipantStatusRegistered
		checkedAt := ""
		if eligible {
			status = ParticipantStatusCheckedIn
			checkedAt = participant.CheckedAt
			if checkedAt == "" {
				checkedAt = now
			}
		}
		if participant.Status == status && participant.AreaStatus == areaStatus && math.Abs(participant.Distance-distance) < 0.000001 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE boss_summon_participants SET status=?,area_status=?,distance=?,actor=?,checked_at=?,updated_at=? WHERE id=?`,
			status, areaStatus, distance, actor, checkedAt, now, participant.ID); err != nil {
			return err
		}
		if err := insertParticipantEvent(ctx, tx, policy.SummonID, participant.PlayerUID, "policy_rechecked", actor, map[string]any{
			"status": status, "area_status": areaStatus, "eligible": eligible, "distance": distance,
			"radius": policy.Radius, "use_z": policy.UseZ,
		}, now); err != nil {
			return err
		}
	}
	return nil
}

func registrationContextTx(ctx context.Context, tx *sql.Tx, summonID string) (Summon, RegistrationPolicy, error) {
	summon, err := getSummonTx(ctx, tx, summonID)
	if errors.Is(err, sql.ErrNoRows) {
		return Summon{}, RegistrationPolicy{}, ErrSummonNotFound
	}
	if err != nil {
		return Summon{}, RegistrationPolicy{}, err
	}
	registered, checkedIn, outside, err := participantCountsTx(ctx, tx, summonID)
	if err != nil {
		return Summon{}, RegistrationPolicy{}, err
	}
	return summon, registrationPolicy(summon, registered, checkedIn, outside), nil
}

func registrationPolicy(summon Summon, registered, checkedIn, outside int) RegistrationPolicy {
	enabled := registrationMetadataBool(summon.Metadata, "registration_enabled", false)
	maxPlayers := registrationMetadataInt(summon.Metadata, "registration_max_players", 0)
	radius := registrationMetadataFloat(summon.Metadata, "registration_radius", 0)
	useZ := registrationMetadataBool(summon.Metadata, "registration_use_z", false)
	allowActive := registrationMetadataBool(summon.Metadata, "registration_allow_active", false)
	open := enabled && (summon.Status == SummonStatusPending || (summon.Status == SummonStatusActive && allowActive))
	available := -1
	if maxPlayers > 0 {
		available = max(maxPlayers-registered, 0)
	}
	configuration := "disabled"
	if enabled {
		configuration = "configured"
	}
	return RegistrationPolicy{
		SummonID: summon.ID, Enabled: enabled, Open: open, AllowActive: allowActive,
		MaxPlayers: maxPlayers, Registered: registered, CheckedIn: checkedIn, Outside: outside,
		Available: available, Radius: radius, UseZ: useZ, AreaRequired: radius > 0,
		Center: summon.Location, SummonStatus: summon.Status, Configuration: configuration,
	}
}

func participantArea(policy RegistrationPolicy, location *Location) (string, float64, bool) {
	if policy.Radius <= 0 {
		return ParticipantAreaDisabled, 0, true
	}
	if location == nil {
		return ParticipantAreaUnknown, 0, false
	}
	dx := location.X - policy.Center.X
	dy := location.Y - policy.Center.Y
	distanceSquared := dx*dx + dy*dy
	if policy.UseZ {
		dz := location.Z - policy.Center.Z
		distanceSquared += dz * dz
	}
	distance := math.Sqrt(distanceSquared)
	if distance <= policy.Radius {
		return ParticipantAreaEligible, distance, true
	}
	return ParticipantAreaOutside, distance, false
}

func validateRegistrationLocation(location Location) error {
	for _, value := range []float64{location.X, location.Y, location.Z} {
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 10_000_000 {
			return ErrInvalidRegistration
		}
	}
	if len(strings.TrimSpace(location.Label)) > 128 {
		return ErrInvalidRegistration
	}
	return nil
}

func participantCountsTx(ctx context.Context, tx *sql.Tx, summonID string) (int, int, int, error) {
	return participantCounts(ctx, tx, summonID)
}

func participantCounts(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, summonID string) (int, int, int, error) {
	var registered, checkedIn, outside int
	err := queryer.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN status IN ('registered','checked_in') THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='checked_in' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status IN ('registered','checked_in') AND area_status='outside' THEN 1 ELSE 0 END),0)
		FROM boss_summon_participants WHERE summon_id=?`, summonID).Scan(&registered, &checkedIn, &outside)
	return registered, checkedIn, outside, err
}

const participantSelect = `SELECT id,summon_id,player_uid,nickname,steam_id,status,area_status,location_json,distance,actor,metadata_json,registered_at,checked_at,cancelled_at,updated_at FROM boss_summon_participants`

func participantTx(ctx context.Context, tx *sql.Tx, summonID, playerUID string) (Participant, error) {
	return scanParticipant(tx.QueryRowContext(ctx, participantSelect+` WHERE summon_id=? AND player_uid=?`, summonID, playerUID))
}

func scanParticipant(scanner interface{ Scan(...any) error }) (Participant, error) {
	var item Participant
	var locationJSON, metadataJSON string
	if err := scanner.Scan(&item.ID, &item.SummonID, &item.PlayerUID, &item.Nickname, &item.SteamID,
		&item.Status, &item.AreaStatus, &locationJSON, &item.Distance, &item.Actor, &metadataJSON,
		&item.RegisteredAt, &item.CheckedAt, &item.CancelledAt, &item.UpdatedAt); err != nil {
		return Participant{}, err
	}
	if strings.TrimSpace(locationJSON) != "" && strings.TrimSpace(locationJSON) != "{}" {
		var location Location
		if json.Unmarshal([]byte(locationJSON), &location) == nil {
			item.Location = &location
		}
	}
	item.Metadata = decodeObject(metadataJSON)
	item.Eligible = item.AreaStatus == ParticipantAreaEligible || item.AreaStatus == ParticipantAreaDisabled
	return item, nil
}

func insertParticipantEvent(ctx context.Context, tx *sql.Tx, summonID, playerUID, eventType, actor string, details map[string]any, createdAt string) error {
	body, _ := json.Marshal(normalizedMap(details))
	_, err := tx.ExecContext(ctx, `INSERT INTO boss_participant_events(summon_id,player_uid,event_type,actor,details_json,created_at) VALUES(?,?,?,?,?,?)`,
		summonID, playerUID, eventType, actor, string(body), createdAt)
	return err
}

func registrationMetadataBool(metadata map[string]any, key string, fallback bool) bool {
	value, exists := metadata[key]
	if !exists {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true") || strings.TrimSpace(typed) == "1"
	case float64:
		return typed != 0
	case int:
		return typed != 0
	default:
		return fallback
	}
}

func registrationMetadataInt(metadata map[string]any, key string, fallback int) int {
	value, exists := metadata[key]
	if !exists {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return int(parsed)
		}
	}
	return fallback
}

func registrationMetadataFloat(metadata map[string]any, key string, fallback float64) float64 {
	value, exists := metadata[key]
	if !exists {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err == nil {
			return parsed
		}
	}
	return fallback
}
