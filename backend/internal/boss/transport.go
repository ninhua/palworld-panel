package boss

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"palpanel/internal/playeridentity"
)

var (
	ErrInvalidTransport       = errors.New("boss participant transport request is invalid")
	ErrTransportBusy          = errors.New("boss participant transport is busy")
	ErrTransportStateConflict = errors.New("boss participant transport state conflicts with the requested operation")
)

const (
	TransportStatePrepared       = "prepared"
	TransportStateTeleported     = "teleported"
	TransportStateTeleportFailed = "teleport_failed"
	TransportStateReturned       = "returned"
	TransportStateReturnFailed   = "return_failed"

	transportLeaseDuration = 10 * time.Minute
)

type ParticipantTransport struct {
	ID               int64          `json:"id"`
	SummonID         string         `json:"summon_id"`
	PlayerUID        string         `json:"player_uid"`
	Nickname         string         `json:"nickname,omitempty"`
	Identifier       string         `json:"identifier"`
	State            string         `json:"state"`
	Origin           Location       `json:"origin"`
	Destination      Location       `json:"destination"`
	TeleportAttempts int            `json:"teleport_attempts"`
	ReturnAttempts   int            `json:"return_attempts"`
	LastError        string         `json:"last_error,omitempty"`
	Actor            string         `json:"actor,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	PreparedAt       string         `json:"prepared_at"`
	TeleportedAt     string         `json:"teleported_at,omitempty"`
	ReturnedAt       string         `json:"returned_at,omitempty"`
	UpdatedAt        string         `json:"updated_at"`
}

type TransportSnapshot struct {
	SummonID       string                 `json:"summon_id"`
	Items          []ParticipantTransport `json:"items"`
	Count          int                    `json:"count"`
	Prepared       int                    `json:"prepared"`
	Teleported     int                    `json:"teleported"`
	TeleportFailed int                    `json:"teleport_failed"`
	Returned       int                    `json:"returned"`
	ReturnFailed   int                    `json:"return_failed"`
}

func (s *Service) ensureTransportSchema(ctx context.Context) error {
	if err := s.ensureRegistrationSchema(ctx); err != nil {
		return err
	}
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS boss_transport_leases (
			summon_id TEXT PRIMARY KEY,
			holder TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS boss_participant_transports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			summon_id TEXT NOT NULL,
			player_uid TEXT NOT NULL COLLATE PALPLAYERUID,
			nickname TEXT NOT NULL DEFAULT '',
			identifier TEXT NOT NULL,
			state TEXT NOT NULL CHECK(state IN ('prepared','teleported','teleport_failed','returned','return_failed')),
			origin_json TEXT NOT NULL,
			destination_json TEXT NOT NULL,
			teleport_attempts INTEGER NOT NULL DEFAULT 0,
			return_attempts INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			actor TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			prepared_at TEXT NOT NULL,
			teleported_at TEXT NOT NULL DEFAULT '',
			returned_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			UNIQUE(summon_id,player_uid)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_transport_summon_state ON boss_participant_transports(summon_id,state,updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_transport_player ON boss_participant_transports(player_uid,updated_at DESC)`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure boss participant transport schema: %w", err)
		}
	}
	return nil
}

func (s *Service) AcquireTransportOperation(ctx context.Context, summonID string) (string, error) {
	summonID = strings.TrimSpace(summonID)
	if summonID == "" {
		return "", ErrInvalidTransport
	}
	if err := s.ensureTransportSchema(ctx); err != nil {
		return "", err
	}
	holder := newID("boss_transport")
	now := s.now().UTC()
	updatedAt := now.Format(time.RFC3339Nano)
	expiresAt := now.Add(transportLeaseDuration).Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `INSERT INTO boss_transport_leases(summon_id,holder,expires_at,updated_at)
		VALUES(?,?,?,?)
		ON CONFLICT(summon_id) DO UPDATE SET holder=excluded.holder,expires_at=excluded.expires_at,updated_at=excluded.updated_at
		WHERE boss_transport_leases.expires_at<=excluded.updated_at`, summonID, holder, expiresAt, updatedAt)
	if err != nil {
		return "", err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if rows != 1 {
		return "", ErrTransportBusy
	}
	return holder, nil
}

func (s *Service) ReleaseTransportOperation(ctx context.Context, summonID, holder string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM boss_transport_leases WHERE summon_id=? AND holder=?`, strings.TrimSpace(summonID), strings.TrimSpace(holder))
	return err
}

func (s *Service) PrepareParticipantTransport(ctx context.Context, summonID, playerUID, identifier string, origin, destination Location, actor string, metadata map[string]any) (ParticipantTransport, bool, error) {
	if err := s.ensureTransportSchema(ctx); err != nil {
		return ParticipantTransport{}, false, err
	}
	summonID = strings.TrimSpace(summonID)
	playerUID = playeridentity.Normalize(playerUID)
	identifier = strings.TrimSpace(identifier)
	actor = strings.TrimSpace(actor)
	if summonID == "" || playerUID == "" || identifier == "" || len(identifier) > 128 {
		return ParticipantTransport{}, false, ErrInvalidTransport
	}
	if err := validateRegistrationLocation(origin); err != nil {
		return ParticipantTransport{}, false, ErrInvalidTransport
	}
	if err := validateRegistrationLocation(destination); err != nil {
		return ParticipantTransport{}, false, ErrInvalidTransport
	}
	originJSON, err := json.Marshal(origin)
	if err != nil {
		return ParticipantTransport{}, false, ErrInvalidTransport
	}
	destinationJSON, err := json.Marshal(destination)
	if err != nil {
		return ParticipantTransport{}, false, ErrInvalidTransport
	}
	metadataJSON, err := marshalBounded(normalizedMap(metadata))
	if err != nil {
		return ParticipantTransport{}, false, ErrInvalidTransport
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ParticipantTransport{}, false, err
	}
	defer rollback(tx)
	summon, err := getSummonTx(ctx, tx, summonID)
	if errors.Is(err, sql.ErrNoRows) {
		return ParticipantTransport{}, false, ErrSummonNotFound
	}
	if err != nil {
		return ParticipantTransport{}, false, err
	}
	if terminalStatus(summon.Status) {
		return ParticipantTransport{}, false, ErrTransportStateConflict
	}
	participant, err := participantTx(ctx, tx, summonID, playerUID)
	if errors.Is(err, sql.ErrNoRows) {
		return ParticipantTransport{}, false, ErrParticipantNotFound
	}
	if err != nil {
		return ParticipantTransport{}, false, err
	}
	if participant.Status == ParticipantStatusCancelled || !participant.Eligible {
		return ParticipantTransport{}, false, ErrTransportStateConflict
	}

	existing, existingErr := participantTransportTx(ctx, tx, summonID, playerUID)
	if existingErr == nil && existing.State != TransportStateReturned {
		// Any non-returned checkpoint may represent a command whose outcome was not
		// durably recorded. Never send a second outward teleport until an explicit
		// safe return has reconciled the saved origin.
		return existing, true, nil
	}
	if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
		return ParticipantTransport{}, false, existingErr
	}
	now := s.timestamp()
	if existingErr == nil {
		_, err = tx.ExecContext(ctx, `UPDATE boss_participant_transports SET nickname=?,identifier=?,state=?,origin_json=?,destination_json=?,last_error='',actor=?,metadata_json=?,prepared_at=?,teleported_at='',returned_at='',updated_at=? WHERE summon_id=? AND player_uid=?`,
			participant.Nickname, identifier, TransportStatePrepared, string(originJSON), string(destinationJSON), actor, string(metadataJSON), now, now, summonID, playerUID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO boss_participant_transports(summon_id,player_uid,nickname,identifier,state,origin_json,destination_json,actor,metadata_json,prepared_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			summonID, playerUID, participant.Nickname, identifier, TransportStatePrepared, string(originJSON), string(destinationJSON), actor, string(metadataJSON), now, now)
	}
	if err != nil {
		return ParticipantTransport{}, false, err
	}
	if err := insertParticipantEvent(ctx, tx, summonID, playerUID, "transport_prepared", actor, map[string]any{
		"identifier": identifier, "origin": origin, "destination": destination,
	}, now); err != nil {
		return ParticipantTransport{}, false, err
	}
	item, err := participantTransportTx(ctx, tx, summonID, playerUID)
	if err != nil {
		return ParticipantTransport{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ParticipantTransport{}, false, err
	}
	return item, false, nil
}

func (s *Service) CompleteParticipantTeleport(ctx context.Context, summonID, playerUID, actor string, operationErr error) (ParticipantTransport, error) {
	return s.completeParticipantTransportStage(ctx, summonID, playerUID, actor, false, operationErr)
}

func (s *Service) CompleteParticipantReturn(ctx context.Context, summonID, playerUID, actor string, operationErr error) (ParticipantTransport, error) {
	return s.completeParticipantTransportStage(ctx, summonID, playerUID, actor, true, operationErr)
}

func (s *Service) completeParticipantTransportStage(ctx context.Context, summonID, playerUID, actor string, returning bool, operationErr error) (ParticipantTransport, error) {
	if err := s.ensureTransportSchema(ctx); err != nil {
		return ParticipantTransport{}, err
	}
	summonID = strings.TrimSpace(summonID)
	playerUID = playeridentity.Normalize(playerUID)
	actor = strings.TrimSpace(actor)
	if summonID == "" || playerUID == "" {
		return ParticipantTransport{}, ErrInvalidTransport
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ParticipantTransport{}, err
	}
	defer rollback(tx)
	item, err := participantTransportTx(ctx, tx, summonID, playerUID)
	if errors.Is(err, sql.ErrNoRows) {
		return ParticipantTransport{}, ErrParticipantNotFound
	}
	if err != nil {
		return ParticipantTransport{}, err
	}
	if returning {
		if item.State != TransportStatePrepared && item.State != TransportStateTeleported && item.State != TransportStateTeleportFailed && item.State != TransportStateReturnFailed {
			return ParticipantTransport{}, ErrTransportStateConflict
		}
	} else if item.State != TransportStatePrepared && item.State != TransportStateTeleportFailed {
		return ParticipantTransport{}, ErrTransportStateConflict
	}
	now := s.timestamp()
	state := TransportStateTeleported
	eventType := "transport_succeeded"
	lastError := ""
	teleportedAt := item.TeleportedAt
	returnedAt := item.ReturnedAt
	if returning {
		state = TransportStateReturned
		eventType = "return_succeeded"
		returnedAt = now
	} else {
		teleportedAt = now
	}
	if operationErr != nil {
		lastError = strings.TrimSpace(operationErr.Error())
		if len(lastError) > 4096 {
			lastError = lastError[:4096]
		}
		if returning {
			state = TransportStateReturnFailed
			eventType = "return_failed"
		} else {
			state = TransportStateTeleportFailed
			eventType = "transport_failed"
			teleportedAt = ""
		}
	}
	if returning {
		_, err = tx.ExecContext(ctx, `UPDATE boss_participant_transports SET state=?,return_attempts=return_attempts+1,last_error=?,actor=?,returned_at=?,updated_at=? WHERE summon_id=? AND player_uid=?`,
			state, lastError, actor, returnedAt, now, summonID, playerUID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE boss_participant_transports SET state=?,teleport_attempts=teleport_attempts+1,last_error=?,actor=?,teleported_at=?,updated_at=? WHERE summon_id=? AND player_uid=?`,
			state, lastError, actor, teleportedAt, now, summonID, playerUID)
	}
	if err != nil {
		return ParticipantTransport{}, err
	}
	if err := insertParticipantEvent(ctx, tx, summonID, playerUID, eventType, actor, map[string]any{
		"state": state, "error": lastError,
	}, now); err != nil {
		return ParticipantTransport{}, err
	}
	item, err = participantTransportTx(ctx, tx, summonID, playerUID)
	if err != nil {
		return ParticipantTransport{}, err
	}
	if err := tx.Commit(); err != nil {
		return ParticipantTransport{}, err
	}
	return item, nil
}

func (s *Service) TransportSnapshot(ctx context.Context, summonID string) (TransportSnapshot, error) {
	if err := s.ensureTransportSchema(ctx); err != nil {
		return TransportSnapshot{}, err
	}
	summonID = strings.TrimSpace(summonID)
	if summonID == "" {
		return TransportSnapshot{}, ErrInvalidTransport
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,summon_id,player_uid,nickname,identifier,state,origin_json,destination_json,teleport_attempts,return_attempts,last_error,actor,metadata_json,prepared_at,teleported_at,returned_at,updated_at FROM boss_participant_transports WHERE summon_id=? ORDER BY updated_at DESC,id DESC`, summonID)
	if err != nil {
		return TransportSnapshot{}, err
	}
	defer rows.Close()
	result := TransportSnapshot{SummonID: summonID, Items: []ParticipantTransport{}}
	for rows.Next() {
		item, scanErr := scanParticipantTransport(rows)
		if scanErr != nil {
			return TransportSnapshot{}, scanErr
		}
		result.Items = append(result.Items, item)
		switch item.State {
		case TransportStatePrepared:
			result.Prepared++
		case TransportStateTeleported:
			result.Teleported++
		case TransportStateTeleportFailed:
			result.TeleportFailed++
		case TransportStateReturned:
			result.Returned++
		case TransportStateReturnFailed:
			result.ReturnFailed++
		}
	}
	if err := rows.Err(); err != nil {
		return TransportSnapshot{}, err
	}
	result.Count = len(result.Items)
	return result, nil
}

type transportScanner interface {
	Scan(dest ...any) error
}

func participantTransportTx(ctx context.Context, tx *sql.Tx, summonID, playerUID string) (ParticipantTransport, error) {
	return scanParticipantTransport(tx.QueryRowContext(ctx, `SELECT id,summon_id,player_uid,nickname,identifier,state,origin_json,destination_json,teleport_attempts,return_attempts,last_error,actor,metadata_json,prepared_at,teleported_at,returned_at,updated_at FROM boss_participant_transports WHERE summon_id=? AND player_uid=?`, summonID, playerUID))
}

func scanParticipantTransport(scanner transportScanner) (ParticipantTransport, error) {
	var item ParticipantTransport
	var originJSON, destinationJSON, metadataJSON string
	if err := scanner.Scan(&item.ID, &item.SummonID, &item.PlayerUID, &item.Nickname, &item.Identifier, &item.State, &originJSON, &destinationJSON, &item.TeleportAttempts, &item.ReturnAttempts, &item.LastError, &item.Actor, &metadataJSON, &item.PreparedAt, &item.TeleportedAt, &item.ReturnedAt, &item.UpdatedAt); err != nil {
		return ParticipantTransport{}, err
	}
	if err := json.Unmarshal([]byte(originJSON), &item.Origin); err != nil {
		return ParticipantTransport{}, err
	}
	if err := json.Unmarshal([]byte(destinationJSON), &item.Destination); err != nil {
		return ParticipantTransport{}, err
	}
	item.Metadata = decodeObject(metadataJSON)
	return item, nil
}
