package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"palpanel/internal/boss"
	"palpanel/internal/paldefender"
	"palpanel/internal/playeridentity"
)

const maximumBossTransportParticipants = 64

type bossTransportBatchInput struct {
	PlayerUIDs   []string `json:"player_uids,omitempty"`
	SpreadRadius *float64 `json:"spread_radius,omitempty"`
}

type bossTransportItemResult struct {
	PlayerUID string                     `json:"player_uid"`
	Nickname  string                     `json:"nickname,omitempty"`
	Status    string                     `json:"status"`
	Error     string                     `json:"error,omitempty"`
	Lookup    *bossBridgeLocationLookup  `json:"location_lookup,omitempty"`
	Transport *boss.ParticipantTransport `json:"transport,omitempty"`
}

type bossTransportBatchResult struct {
	SummonID  string                    `json:"summon_id"`
	Action    string                    `json:"action"`
	Requested int                       `json:"requested"`
	Selected  int                       `json:"selected"`
	Succeeded int                       `json:"succeeded"`
	Failed    int                       `json:"failed"`
	Skipped   int                       `json:"skipped"`
	Items     []bossTransportItemResult `json:"items"`
	Snapshot  boss.TransportSnapshot    `json:"snapshot"`
}

func init() {
	patchFeatures = append(patchFeatures,
		"boss-participant-batch-teleport",
		"boss-participant-return-checkpoint",
		"boss-participant-transport-ledger",
		"boss-participant-transport-recovery",
		"boss-participant-transport-ui",
	)
}

func isBossParticipantTransportControl(action string) bool {
	switch normalizeBossRegistrationAction(action) {
	case "transport_snapshot", "participants_teleport", "participants_return":
		return true
	default:
		return false
	}
}

func (s Server) teleportBossParticipants(ctx context.Context, service *boss.Service, summonID string, input bossTransportBatchInput, actor string) (bossTransportBatchResult, error) {
	spreadRadius := 200.0
	if input.SpreadRadius != nil {
		spreadRadius = *input.SpreadRadius
	}
	if math.IsNaN(spreadRadius) || math.IsInf(spreadRadius, 0) || spreadRadius < 0 || spreadRadius > 5000 {
		return bossTransportBatchResult{}, boss.ErrInvalidTransport
	}
	holder, err := service.AcquireTransportOperation(ctx, summonID)
	if err != nil {
		return bossTransportBatchResult{}, err
	}
	defer func() { _ = service.ReleaseTransportOperation(context.Background(), summonID, holder) }()

	registration, err := service.RegistrationSnapshot(ctx, summonID, false)
	if err != nil {
		return bossTransportBatchResult{}, err
	}
	participants := selectBossTransportParticipants(registration.Participants, input.PlayerUIDs)
	if len(participants) > maximumBossTransportParticipants {
		return bossTransportBatchResult{}, fmt.Errorf("%w: at most %d participants can be transported in one batch", boss.ErrInvalidTransport, maximumBossTransportParticipants)
	}
	result := bossTransportBatchResult{
		SummonID:  summonID,
		Action:    "teleport",
		Requested: len(input.PlayerUIDs),
		Selected:  len(participants),
		Items:     make([]bossTransportItemResult, 0, len(participants)),
	}
	if len(participants) == 0 {
		result.Snapshot, err = service.TransportSnapshot(ctx, summonID)
		return result, err
	}

	players, observedAt, err := s.queryBossBridgeOnlinePlayers(ctx)
	if err != nil {
		return bossTransportBatchResult{}, err
	}
	for index, participant := range participants {
		itemResult := bossTransportItemResult{PlayerUID: participant.PlayerUID, Nickname: participant.Nickname}
		lookup, lookupErr := matchBossBridgePlayerLocation(players, observedAt, participant.PlayerUID, participant.SteamID, participant.Nickname)
		itemResult.Lookup = &lookup
		if lookupErr != nil || lookup.Location == nil {
			itemResult.Status = "location_unavailable"
			itemResult.Error = lookup.Error
			result.Skipped++
			result.Items = append(result.Items, itemResult)
			continue
		}
		destination := spreadBossTransportDestination(registration.Policy.Center, index, len(participants), spreadRadius)
		identifier := lookup.Identifier
		if identifier == "" {
			identifier = participant.PlayerUID
		}
		transport, duplicate, prepareErr := service.PrepareParticipantTransport(ctx, summonID, participant.PlayerUID, identifier, *lookup.Location, destination, actor, map[string]any{
			"location_source":      lookup.Source,
			"location_matched_by":  lookup.MatchedBy,
			"location_observed_at": lookup.ObservedAt,
			"spread_radius":        spreadRadius,
		})
		if prepareErr != nil {
			itemResult.Status = "prepare_failed"
			itemResult.Error = prepareErr.Error()
			result.Failed++
			result.Items = append(result.Items, itemResult)
			continue
		}
		itemResult.Transport = &transport
		if duplicate {
			switch transport.State {
			case boss.TransportStatePrepared:
				itemResult.Status = "transport_reconcile_required"
				itemResult.Error = "a saved checkpoint already exists without a final teleport result; run safe return before retrying"
			case boss.TransportStateTeleportFailed:
				itemResult.Status = "teleport_failed_return_required"
				itemResult.Error = "the previous teleport failed or was uncertain; run safe return before retrying"
			case boss.TransportStateReturnFailed:
				itemResult.Status = "return_required"
				itemResult.Error = "the participant still has a failed safe-return operation"
			default:
				itemResult.Status = "already_teleported"
			}
			result.Skipped++
			result.Items = append(result.Items, itemResult)
			continue
		}
		request, requestErr := bossCoordinateTeleportRequest(destination)
		if requestErr == nil {
			_, requestErr = s.defender.RCONTeleport(ctx, participant.PlayerUID, request)
		}
		recorded, recordErr := completeBossParticipantTeleportLedger(service, summonID, participant.PlayerUID, actor, requestErr)
		if recordErr == nil {
			transport = recorded
		}
		if recordErr != nil {
			if requestErr == nil {
				requestErr = fmt.Errorf("teleport command succeeded but ledger update failed: %w", recordErr)
			} else {
				requestErr = errors.Join(requestErr, recordErr)
			}
		}
		itemResult.Transport = &transport
		if requestErr != nil {
			itemResult.Status = "teleport_failed"
			itemResult.Error = requestErr.Error()
			result.Failed++
		} else {
			itemResult.Status = "teleported"
			result.Succeeded++
		}
		result.Items = append(result.Items, itemResult)
	}
	result.Snapshot, err = service.TransportSnapshot(ctx, summonID)
	return result, err
}

func (s Server) returnBossParticipants(ctx context.Context, service *boss.Service, summonID string, input bossTransportBatchInput, actor string) (bossTransportBatchResult, error) {
	holder, err := service.AcquireTransportOperation(ctx, summonID)
	if err != nil {
		return bossTransportBatchResult{}, err
	}
	defer func() { _ = service.ReleaseTransportOperation(context.Background(), summonID, holder) }()

	snapshot, err := service.TransportSnapshot(ctx, summonID)
	if err != nil {
		return bossTransportBatchResult{}, err
	}
	selected := normalizeBossTransportSelection(input.PlayerUIDs)
	items := make([]boss.ParticipantTransport, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		if item.State != boss.TransportStatePrepared && item.State != boss.TransportStateTeleported && item.State != boss.TransportStateTeleportFailed && item.State != boss.TransportStateReturnFailed {
			continue
		}
		if len(selected) > 0 {
			if _, ok := selected[playeridentity.Normalize(item.PlayerUID)]; !ok {
				continue
			}
		}
		items = append(items, item)
	}
	if len(items) > maximumBossTransportParticipants {
		return bossTransportBatchResult{}, fmt.Errorf("%w: at most %d participants can be returned in one batch", boss.ErrInvalidTransport, maximumBossTransportParticipants)
	}
	result := bossTransportBatchResult{
		SummonID:  summonID,
		Action:    "return",
		Requested: len(input.PlayerUIDs),
		Selected:  len(items),
		Items:     make([]bossTransportItemResult, 0, len(items)),
	}
	for _, item := range items {
		itemResult := bossTransportItemResult{PlayerUID: item.PlayerUID, Nickname: item.Nickname, Transport: &item}
		request, requestErr := bossCoordinateTeleportRequest(item.Origin)
		if requestErr == nil {
			_, requestErr = s.defender.RCONTeleport(ctx, item.Identifier, request)
		}
		transport := item
		recorded, recordErr := completeBossParticipantReturnLedger(service, summonID, item.PlayerUID, actor, requestErr)
		if recordErr == nil {
			transport = recorded
		}
		if recordErr != nil {
			if requestErr == nil {
				requestErr = fmt.Errorf("return command succeeded but ledger update failed: %w", recordErr)
			} else {
				requestErr = errors.Join(requestErr, recordErr)
			}
		}
		itemResult.Transport = &transport
		if requestErr != nil {
			itemResult.Status = "return_failed"
			itemResult.Error = requestErr.Error()
			result.Failed++
		} else {
			itemResult.Status = "returned"
			result.Succeeded++
		}
		result.Items = append(result.Items, itemResult)
	}
	result.Snapshot, err = service.TransportSnapshot(ctx, summonID)
	return result, err
}

func completeBossParticipantTeleportLedger(service *boss.Service, summonID, playerUID, actor string, operationErr error) (boss.ParticipantTransport, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return service.CompleteParticipantTeleport(ctx, summonID, playerUID, actor, operationErr)
}

func completeBossParticipantReturnLedger(service *boss.Service, summonID, playerUID, actor string, operationErr error) (boss.ParticipantTransport, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return service.CompleteParticipantReturn(ctx, summonID, playerUID, actor, operationErr)
}

func selectBossTransportParticipants(participants []boss.Participant, playerUIDs []string) []boss.Participant {
	selected := normalizeBossTransportSelection(playerUIDs)
	result := make([]boss.Participant, 0, len(participants))
	for _, participant := range participants {
		if participant.Status == boss.ParticipantStatusCancelled || !participant.Eligible {
			continue
		}
		if len(selected) > 0 {
			if _, ok := selected[playeridentity.Normalize(participant.PlayerUID)]; !ok {
				continue
			}
		}
		result = append(result, participant)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].RegisteredAt != result[j].RegisteredAt {
			return result[i].RegisteredAt < result[j].RegisteredAt
		}
		return result[i].PlayerUID < result[j].PlayerUID
	})
	return result
}

func normalizeBossTransportSelection(values []string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, value := range values {
		if normalized := playeridentity.Normalize(value); normalized != "" {
			result[normalized] = struct{}{}
		}
	}
	return result
}

func spreadBossTransportDestination(center boss.Location, index, count int, radius float64) boss.Location {
	if count <= 1 || radius <= 0 {
		return center
	}
	angle := 2 * math.Pi * float64(index) / float64(count)
	result := center
	result.X += radius * math.Cos(angle)
	result.Y += radius * math.Sin(angle)
	return result
}

func bossCoordinateTeleportRequest(location boss.Location) (paldefender.TeleportRequest, error) {
	var request paldefender.TeleportRequest
	body, err := json.Marshal(map[string]any{
		"Mode": "coordinates",
		"X":    location.X,
		"Y":    location.Y,
		"Z":    location.Z,
	})
	if err != nil {
		return request, err
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return request, fmt.Errorf("build PalDefender teleport request: %w", err)
	}
	return request, nil
}
