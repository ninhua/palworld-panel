package breeding

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/id"
	"palpanel/internal/pallocalize"
	"palpanel/internal/saveindex"
)

type Service struct {
	cfg       appconfig.Config
	store     *db.Store
	saveIndex *saveindex.Manager
	client    *http.Client
	statusMu  sync.RWMutex
	lastError string
	checkedAt time.Time
}

type Status struct {
	Configured      bool   `json:"configured"`
	Available       bool   `json:"available"`
	UpstreamVersion string `json:"upstream_version,omitempty"`
	DatabaseVersion string `json:"database_version,omitempty"`
	LatencyMS       int64  `json:"latency_ms"`
	LastError       string `json:"last_error,omitempty"`
	CheckedAt       string `json:"checked_at,omitempty"`
}

type Target struct {
	PalID            string   `json:"pal_id"`
	Gender           string   `json:"gender,omitempty"`
	RequiredPassives []string `json:"required_passives"`
	OptionalPassives []string `json:"optional_passives"`
	IVHP             int      `json:"iv_hp"`
	IVAttack         int      `json:"iv_attack"`
	IVDefense        int      `json:"iv_defense"`
}

type Settings struct {
	MaxBreedingSteps           int      `json:"max_breeding_steps"`
	MaxSolverIterations        int      `json:"max_solver_iterations"`
	MaxWildPals                int      `json:"max_wild_pals"`
	MaxInputIrrelevantPassives int      `json:"max_input_irrelevant_passives"`
	MaxBredIrrelevantPassives  int      `json:"max_bred_irrelevant_passives"`
	MaxThreads                 int      `json:"max_threads"`
	MaxGoldCost                int      `json:"max_gold_cost"`
	UseGenderReversers         bool     `json:"use_gender_reversers"`
	AllowedWildPals            []string `json:"allowed_wild_pals,omitempty"`
	BannedWildPals             []string `json:"banned_wild_pals,omitempty"`
	BannedBredPals             []string `json:"banned_bred_pals,omitempty"`
	AllowedSurgeryPassives     []string `json:"allowed_surgery_passives,omitempty"`
	BannedSurgeryPassives      []string `json:"banned_surgery_passives,omitempty"`
}

type GameSettings struct {
	BreedingTimeSeconds         int  `json:"breeding_time_seconds"`
	MassiveEggIncubationMinutes int  `json:"massive_egg_incubation_minutes"`
	MultipleBreedingFarms       bool `json:"multiple_breeding_farms"`
	MultipleIncubators          bool `json:"multiple_incubators"`
}

type SubmitInput struct {
	Target             Target       `json:"target"`
	Settings           Settings     `json:"settings"`
	GameSettings       GameSettings `json:"game_settings"`
	OwnerPlayerUID     string       `json:"owner_player_uid,omitempty"`
	ResultLimit        int          `json:"result_limit"`
	CustomContainerIDs []string     `json:"custom_container_ids,omitempty"`
}

type Billing struct {
	ReservationID string
	Settle        func(context.Context, string, bool) error
}

type bridgeJob struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Phase         string `json:"phase"`
	Step          int    `json:"step"`
	TargetSteps   int    `json:"target_steps"`
	WorkProcessed int64  `json:"work_processed"`
	WorkTotal     int64  `json:"work_total"`
	ErrorCode     string `json:"error_code"`
	Error         string `json:"error"`
}

func New(cfg appconfig.Config, store *db.Store, index *saveindex.Manager) *Service {
	timeout := time.Duration(cfg.PalCalcTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &Service{cfg: cfg, store: store, saveIndex: index, client: &http.Client{Timeout: timeout}}
}

func (s *Service) Status(ctx context.Context) Status {
	status := Status{Configured: strings.TrimSpace(s.cfg.PalCalcBridgeURL) != ""}
	if !status.Configured {
		status.LastError = "PalCalc bridge URL is not configured"
		return status
	}
	started := time.Now()
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	raw, err := s.request(checkCtx, http.MethodGet, "/health", nil)
	status.LatencyMS = time.Since(started).Milliseconds()
	s.statusMu.RLock()
	status.LastError = s.lastError
	status.CheckedAt = s.checkedAt.UTC().Format(time.RFC3339Nano)
	s.statusMu.RUnlock()
	if err != nil {
		status.LastError = err.Error()
		return status
	}
	var payload struct {
		Status          string `json:"status"`
		UpstreamVersion string `json:"upstream_version"`
		DatabaseVersion string `json:"database_version"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		status.LastError = "PalCalc health response is invalid: " + err.Error()
		return status
	}
	status.Available = strings.EqualFold(payload.Status, "ok")
	status.UpstreamVersion = payload.UpstreamVersion
	status.DatabaseVersion = payload.DatabaseVersion
	if status.Available {
		status.LastError = ""
	}
	return status
}

func (s *Service) Catalog(ctx context.Context) (json.RawMessage, error) {
	raw, err := s.request(ctx, http.MethodGet, "/v1/catalog", nil)
	if err != nil {
		return nil, err
	}
	return localizeCatalog(raw)
}

func (s *Service) Submit(ctx context.Context, subject string, input SubmitInput, billing *Billing) (db.Job, error) {
	if strings.TrimSpace(input.Target.PalID) == "" {
		return db.Job{}, errors.New("target pal is required")
	}
	index, status, err := s.saveIndex.Current(ctx)
	if err != nil || status.State != "ready" {
		return db.Job{}, fmt.Errorf("save index is not ready: %w", err)
	}
	owned := make([]map[string]any, 0, len(index.Pals))
	for _, pal := range index.Pals {
		if input.OwnerPlayerUID != "" && !strings.EqualFold(pal.OwnerPlayerUID, input.OwnerPlayerUID) {
			continue
		}
		owned = append(owned, map[string]any{
			"instance_id": pal.InstanceID, "pal_id": pal.CharacterID, "nickname": pal.Nickname,
			"level": pal.Level, "owner_player_id": pal.OwnerPlayerUID, "gender": pal.Gender,
			"passives": pal.Passives, "rank": max(1, pal.Rank), "iv_hp": pal.IVHP,
			"iv_attack": pal.IVAttack, "iv_defense": pal.IVDefense, "container_id": pal.ContainerID,
			"slot_index": pal.SlotIndex, "location_type": pal.LocationType,
		})
	}
	for _, containerID := range input.CustomContainerIDs {
		container, err := s.store.GetCustomPalContainer(ctx, subject, containerID)
		if err != nil {
			return db.Job{}, fmt.Errorf("custom pal container %s is unavailable: %w", containerID, err)
		}
		var customPals []saveindex.Pal
		if err := json.Unmarshal([]byte(container.PalsJSON), &customPals); err != nil {
			return db.Job{}, fmt.Errorf("custom pal container %s is invalid: %w", containerID, err)
		}
		for position, pal := range customPals {
			owned = append(owned, map[string]any{
				"instance_id": pal.InstanceID, "pal_id": pal.CharacterID, "nickname": pal.Nickname,
				"level": pal.Level, "owner_player_id": input.OwnerPlayerUID, "gender": pal.Gender,
				"passives": pal.Passives, "rank": max(1, pal.Rank), "iv_hp": pal.IVHP,
				"iv_attack": pal.IVAttack, "iv_defense": pal.IVDefense, "container_id": container.ID,
				"slot_index": position, "location_type": "custom",
			})
		}
	}
	if len(owned) == 0 {
		return db.Job{}, errors.New("no owned pals matched the selected source")
	}

	sourceID := "server"
	if source, sourceErr := s.store.ActiveSaveSource(ctx); sourceErr == nil {
		sourceID = source.ID
	}
	requestJSON, _ := json.Marshal(input)
	if cached, cacheErr := s.store.FindCachedBreedingResult(ctx, sourceID, index.Snapshot.Fingerprint, string(requestJSON)); cacheErr == nil {
		job, err := s.store.CreateJob(ctx, id.New("breed"), "breeding", "已命中相同存档与设置的缓存结果")
		if err != nil {
			return db.Job{}, err
		}
		if err := s.store.CreateBreedingResult(ctx, db.BreedingResult{
			ID: id.New("result"), JobID: job.ID, Subject: subject, SourceID: sourceID,
			Fingerprint: index.Snapshot.Fingerprint, RequestJSON: string(requestJSON), ResultJSON: cached.ResultJSON, Status: "completed",
		}); err != nil {
			return db.Job{}, err
		}
		_ = s.store.UpdateJobWithCode(ctx, job.ID, "completed", 100, "已读取缓存配种结果", "", "")
		s.settleBilling(billing, true)
		return s.store.GetJob(ctx, job.ID)
	} else if !errors.Is(cacheErr, sql.ErrNoRows) {
		return db.Job{}, cacheErr
	}

	job, err := s.store.CreateJob(ctx, id.New("breed"), "breeding", "配种计算已排队")
	if err != nil {
		return db.Job{}, err
	}
	payload := map[string]any{
		"request_id": job.ID, "save_fingerprint": index.Snapshot.Fingerprint,
		"owned_pals": owned, "target": input.Target, "settings": withDefaults(input.Settings),
		"game_settings": gameDefaults(input.GameSettings), "result_limit": clamp(input.ResultLimit, 1, 100, 20),
	}
	if err := s.store.CreateBreedingResult(ctx, db.BreedingResult{
		ID: id.New("result"), JobID: job.ID, Subject: subject, SourceID: sourceID,
		Fingerprint: index.Snapshot.Fingerprint, RequestJSON: string(requestJSON), Status: "queued",
	}); err != nil {
		_ = s.store.UpdateJobWithCode(ctx, job.ID, "failed", 0, "保存配种任务失败", err.Error(), "breeding_store_failed")
		return db.Job{}, err
	}
	go s.run(job.ID, payload, billing)
	return job, nil
}

func (s *Service) run(jobID string, payload map[string]any, billing *Billing) {
	timeout := time.Duration(s.cfg.PalCalcTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if _, err := s.request(ctx, http.MethodPost, "/v1/jobs", payload); err != nil {
		s.fail(jobID, "palcalc_submit_failed", err)
		s.settleBilling(billing, false)
		return
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.fail(jobID, "palcalc_timeout", ctx.Err())
			s.settleBilling(billing, false)
			return
		case <-ticker.C:
			raw, err := s.request(ctx, http.MethodGet, "/v1/jobs/"+url.PathEscape(jobID), nil)
			if err != nil {
				continue
			}
			var state bridgeJob
			if err := json.Unmarshal(raw, &state); err != nil {
				continue
			}
			progress := 0
			if state.WorkTotal > 0 {
				progress = int(min(int64(99), state.WorkProcessed*100/state.WorkTotal))
			} else if state.TargetSteps > 0 {
				progress = min(90, state.Step*90/state.TargetSteps)
			}
			_ = s.store.UpdateJobWithCode(context.Background(), jobID, normalizeStatus(state.Status), progress, state.Phase, state.Error, state.ErrorCode)
			switch state.Status {
			case "completed":
				result, err := s.request(ctx, http.MethodGet, "/v1/jobs/"+url.PathEscape(jobID)+"/result", nil)
				if err != nil {
					s.fail(jobID, "palcalc_result_failed", err)
					s.settleBilling(billing, false)
					return
				}
				_ = s.store.CompleteBreedingResult(context.Background(), jobID, "completed", string(result))
				_ = s.store.UpdateJobWithCode(context.Background(), jobID, "completed", 100, "配种计算完成", "", "")
				s.settleBilling(billing, true)
				return
			case "failed", "canceled":
				_ = s.store.CompleteBreedingResult(context.Background(), jobID, state.Status, "")
				s.settleBilling(billing, false)
				return
			}
		}
	}
}

func (s *Service) settleBilling(billing *Billing, commit bool) {
	if billing == nil || billing.Settle == nil || billing.ReservationID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = billing.Settle(ctx, billing.ReservationID, commit)
}

func (s *Service) Control(ctx context.Context, jobID, action string) (json.RawMessage, error) {
	method, path := http.MethodPost, "/v1/jobs/"+url.PathEscape(jobID)+"/"+action
	if action == "cancel" {
		method, path = http.MethodDelete, "/v1/jobs/"+url.PathEscape(jobID)
	}
	return s.request(ctx, method, path, nil)
}

func (s *Service) Result(ctx context.Context, jobID string) (db.BreedingResult, json.RawMessage, error) {
	item, err := s.store.GetBreedingResultByJob(ctx, jobID)
	if err != nil {
		return item, nil, err
	}
	raw := json.RawMessage(item.ResultJSON)
	if len(raw) == 0 {
		return item, raw, nil
	}
	localized, err := localizeBreedingResult(raw)
	if err != nil {
		return item, nil, err
	}
	return item, localized, nil
}

func (s *Service) History(ctx context.Context, subject string, limit int) ([]db.BreedingResult, error) {
	return s.store.ListBreedingResults(ctx, subject, limit)
}

func (s *Service) fail(jobID, code string, err error) {
	_ = s.store.CompleteBreedingResult(context.Background(), jobID, "failed", "")
	_ = s.store.UpdateJobWithCode(context.Background(), jobID, "failed", 0, "配种计算失败", err.Error(), code)
}

func (s *Service) request(ctx context.Context, method, path string, payload any) (json.RawMessage, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.cfg.PalCalcBridgeURL+path, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.recordStatus(err)
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		s.recordStatus(err)
		return nil, err
	}
	if resp.StatusCode >= 300 {
		err := fmt.Errorf("palcalc bridge returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		s.recordStatus(err)
		return nil, err
	}
	s.recordStatus(nil)
	return json.RawMessage(raw), nil
}

func (s *Service) recordStatus(err error) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.checkedAt = time.Now().UTC()
	if err == nil {
		s.lastError = ""
		return
	}
	s.lastError = err.Error()
}

func withDefaults(input Settings) Settings {
	if input.MaxBreedingSteps <= 0 {
		input.MaxBreedingSteps = 6
	}
	if input.MaxSolverIterations <= 0 {
		input.MaxSolverIterations = 20
	}
	if input.MaxInputIrrelevantPassives == 0 {
		input.MaxInputIrrelevantPassives = 2
	}
	if input.MaxBredIrrelevantPassives == 0 {
		input.MaxBredIrrelevantPassives = 1
	}
	return input
}

func gameDefaults(input GameSettings) GameSettings {
	if input.BreedingTimeSeconds <= 0 {
		input.BreedingTimeSeconds = 300
	}
	if input.MassiveEggIncubationMinutes <= 0 {
		input.MassiveEggIncubationMinutes = 120
	}
	if !input.MultipleBreedingFarms && !input.MultipleIncubators {
		input.MultipleBreedingFarms, input.MultipleIncubators = true, true
	}
	return input
}

func normalizeStatus(status string) string {
	switch status {
	case "queued", "running", "paused", "completed", "failed", "canceled":
		return status
	default:
		return "running"
	}
}

func localizeCatalog(raw json.RawMessage) (json.RawMessage, error) {
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("PalCalc catalog response is invalid: %w", err)
	}
	localizeCatalogItems(document["pals"], pallocalize.BreedingPalName)
	localizeCatalogItems(document["passives"], pallocalize.PassiveName)
	return json.Marshal(document)
}

func localizeCatalogItems(raw any, lookup func(string) string) {
	items, _ := raw.([]any)
	for _, rawItem := range items {
		item, _ := rawItem.(map[string]any)
		id := breedingText(item["id"])
		if id == "" {
			continue
		}
		original := breedingText(item["name"])
		localized := lookup(id)
		if localized == "" || localized == id {
			localized = firstNonEmptyText(original, id)
		}
		if breedingText(item["raw_name"]) == "" && original != "" && original != localized {
			item["raw_name"] = original
		}
		item["name"] = localized
	}
}

func localizeBreedingResult(raw json.RawMessage) (json.RawMessage, error) {
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("PalCalc result is invalid: %w", err)
	}
	localizeBreedingValue(document)
	return json.Marshal(document)
}

func localizeBreedingValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		if palID := breedingText(typed["pal_id"]); palID != "" {
			original := breedingText(typed["pal_name"])
			localized := pallocalize.BreedingPalName(palID)
			if localized == "" || localized == palID {
				localized = firstNonEmptyText(original, palID)
			}
			if breedingText(typed["raw_pal_name"]) == "" && original != "" && original != localized {
				typed["raw_pal_name"] = original
			}
			typed["pal_name"] = localized
		}
		if rawPassives, ok := typed["passives"].([]any); ok {
			original := make([]any, 0, len(rawPassives))
			localized := make([]any, 0, len(rawPassives))
			changed := false
			for _, rawPassive := range rawPassives {
				passiveID := breedingText(rawPassive)
				original = append(original, passiveID)
				name := pallocalize.PassiveName(passiveID)
				localized = append(localized, name)
				changed = changed || name != passiveID
			}
			if changed {
				typed["raw_passives"] = original
			}
			typed["passives"] = localized
		}
		for _, child := range typed {
			localizeBreedingValue(child)
		}
	case []any:
		for _, child := range typed {
			localizeBreedingValue(child)
		}
	}
}

func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func breedingText(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func clamp(value, minimum, maximum, fallback int) int {
	if value == 0 {
		return fallback
	}
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
