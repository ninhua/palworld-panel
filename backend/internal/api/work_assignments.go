package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const workAssignmentTokenTTL = 5 * time.Minute

var workAssignmentGUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func init() {
	patchFeatures = append(patchFeatures, "offline-existing-work-assignment-fix")
}

type workAssignmentRequest struct {
	WorkerInstanceID              string `json:"worker_instance_id"`
	WorkBaseID                    string `json:"work_base_id"`
	OwnerMapObjectConcreteModelID string `json:"owner_map_object_concrete_model_id"`
	AssignDefineDataID            string `json:"assign_define_data_id"`
	LocationIndex                 int    `json:"location_index"`
}

type workAssignmentHelperRequest struct {
	BaseCampID                    string `json:"base_camp_id"`
	WorkerInstanceID              string `json:"worker_instance_id"`
	WorkBaseID                    string `json:"work_base_id"`
	OwnerMapObjectConcreteModelID string `json:"owner_map_object_concrete_model_id"`
	AssignDefineDataID            string `json:"assign_define_data_id"`
	LocationIndex                 int    `json:"location_index"`
	ExpectedLevelSHA256           string `json:"expected_level_sha256"`
}

type workAssignmentCommitRequest struct {
	Token          string `json:"token"`
	Confirm        bool   `json:"confirm"`
	IdempotencyKey string `json:"idempotency_key"`
}

type workAssignmentPlan struct {
	Fixed int `json:"fixed"`
}

type workAssignmentListItem struct {
	WorkerInstanceID              string `json:"worker_instance_id"`
	WorkBaseID                    string `json:"work_base_id"`
	OwnerMapObjectConcreteModelID string `json:"owner_map_object_concrete_model_id"`
	AssignDefineDataID            string `json:"assign_define_data_id"`
	LocationIndex                 int    `json:"location_index"`
	AssignmentID                  string `json:"assignment_id"`
	WorkerGUID                    string `json:"worker_guid"`
	AssignType                    int    `json:"assign_type"`
	State                         int    `json:"state"`
	Fixed                         int    `json:"fixed"`
}

type workAssignmentListBase struct {
	WorkType                      string  `json:"work_type"`
	BaseCampID                    string  `json:"base_camp_id"`
	WorkBaseID                    string  `json:"work_base_id"`
	OwnerMapObjectModelID         string  `json:"owner_map_object_model_id"`
	OwnerMapObjectConcreteModelID string  `json:"owner_map_object_concrete_model_id"`
	MapObjectInstanceID           *string `json:"map_object_instance_id"`
	CurrentState                  int     `json:"current_state"`
	AssignLocationCount           int     `json:"assign_location_count"`
	BehaviourType                 int     `json:"behaviour_type"`
	AssignDefineDataID            string  `json:"assign_define_data_id"`
	OverrideWorkType              int     `json:"override_work_type"`
	AssignableFixedType           int     `json:"assignable_fixed_type"`
	AssignableOtomo               int     `json:"assignable_otomo"`
	CanTriggerWorkerEvent         int     `json:"can_trigger_worker_event"`
	CanStealAssign                int     `json:"can_steal_assign"`
	AssignmentCount               int     `json:"assignment_count"`
}

type workAssignmentList struct {
	LevelSHA256 string                   `json:"level_sha256"`
	BaseCampID  string                   `json:"base_camp_id"`
	WorkBases   []workAssignmentListBase `json:"work_bases"`
	Assignments []workAssignmentListItem `json:"assignments"`
}

type workAssignmentFixResult struct {
	Plan    workAssignmentPlan `json:"plan"`
	Changed bool               `json:"changed"`
}

type workAssignmentToken struct {
	Principal string
	WorldID   string
	StableKey string
	ExpiresAt time.Time
	Request   workAssignmentHelperRequest
	Plan      json.RawMessage
}

type workAssignmentTokenStore struct {
	mu      sync.Mutex
	tokens  map[string]workAssignmentToken
	commits map[string]json.RawMessage
}

func newWorkAssignmentTokenStore() *workAssignmentTokenStore {
	return &workAssignmentTokenStore{tokens: map[string]workAssignmentToken{}, commits: map[string]json.RawMessage{}}
}

func (s *workAssignmentTokenStore) put(token string, value workAssignmentToken) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = value
}

func (s *workAssignmentTokenStore) take(token string, now time.Time) (workAssignmentToken, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.tokens[token]
	if !ok || !now.Before(value.ExpiresAt) {
		delete(s.tokens, token)
		return workAssignmentToken{}, false
	}
	delete(s.tokens, token)
	return value, true
}

func (s *workAssignmentTokenStore) commit(key string) (json.RawMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.commits[key]
	return append(json.RawMessage(nil), value...), ok
}

func (s *workAssignmentTokenStore) rememberCommit(key string, value json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits[key] = append(json.RawMessage(nil), value...)
}

func (s Server) prepareWorkAssignment(c *gin.Context) {
	var input workAssignmentRequest
	if err := decodeStrictJSON(c, &input); err != nil {
		fail(c, http.StatusBadRequest, "work_assignment_request_invalid", err.Error())
		return
	}
	baseID := strings.ToLower(strings.TrimSpace(c.Param("id")))
	normalizeWorkAssignmentRequest(&input)
	if err := validateWorkAssignmentRequest(baseID, input); err != nil {
		fail(c, http.StatusBadRequest, "work_assignment_request_invalid", err.Error())
		return
	}
	worldID, worldDir, levelHash, err := s.currentWorkAssignmentWorld()
	if err != nil {
		fail(c, http.StatusConflict, "work_assignment_world_unavailable", err.Error())
		return
	}
	helperRequest := buildWorkAssignmentHelperRequest(baseID, input, levelHash)
	raw, err := runWorkAssignmentHelper(c.Request.Context(), "work-plan", worldDir, "", helperRequest)
	if err != nil {
		fail(c, http.StatusBadGateway, "work_assignment_plan_failed", err.Error())
		return
	}
	plan, err := decodeWorkAssignmentPlan(raw)
	if err != nil || (plan.Fixed != 0 && plan.Fixed != 1) {
		if err == nil {
			err = fmt.Errorf("unexpected fixed value %d", plan.Fixed)
		}
		fail(c, http.StatusBadGateway, "work_assignment_plan_invalid", err.Error())
		return
	}
	token, err := newOpaqueToken()
	if err != nil {
		fail(c, http.StatusInternalServerError, "work_assignment_token_failed", err.Error())
		return
	}
	expiresAt := time.Now().Add(workAssignmentTokenTTL)
	principal := CurrentPrincipal(c).UserID
	s.workAssignments.put(token, workAssignmentToken{
		Principal: principal,
		WorldID:   worldID,
		StableKey: workAssignmentStableKey(helperRequest),
		ExpiresAt: expiresAt,
		Request:   helperRequest,
		Plan:      append(json.RawMessage(nil), raw...),
	})
	ok(c, gin.H{"token": token, "expires_at": expiresAt.UTC().Format(time.RFC3339), "world_id": worldID, "expected_level_sha256": levelHash, "plan": json.RawMessage(raw)})
}

func (s Server) listWorkAssignments(c *gin.Context) {
	baseID := strings.ToLower(strings.TrimSpace(c.Param("id")))
	if !workAssignmentGUID.MatchString(baseID) {
		fail(c, http.StatusBadRequest, "work_assignment_base_invalid", "base id must be a canonical lowercase GUID")
		return
	}
	worldID, worldDir, levelHash, err := s.currentWorkAssignmentWorld()
	if err != nil {
		fail(c, http.StatusConflict, "work_assignment_world_unavailable", err.Error())
		return
	}
	raw, err := runWorkAssignmentListHelper(c.Request.Context(), worldDir, baseID)
	if err != nil {
		fail(c, http.StatusBadGateway, "work_assignment_list_failed", err.Error())
		return
	}
	result, err := decodeWorkAssignmentList(raw)
	if err != nil || result.BaseCampID != baseID || result.LevelSHA256 != levelHash {
		if err == nil {
			err = errors.New("helper result does not match the current base or Level.sav SHA256")
		}
		fail(c, http.StatusConflict, "work_assignment_list_invalid", err.Error())
		return
	}
	if result.Assignments == nil {
		result.Assignments = []workAssignmentListItem{}
	}
	if result.WorkBases == nil {
		result.WorkBases = []workAssignmentListBase{}
	}
	ok(c, gin.H{"world_id": worldID, "level_sha256": result.LevelSHA256, "base_camp_id": result.BaseCampID, "work_bases": result.WorkBases, "assignments": result.Assignments})
}

func (s Server) commitWorkAssignment(c *gin.Context) {
	var input workAssignmentCommitRequest
	if err := decodeStrictJSON(c, &input); err != nil {
		fail(c, http.StatusBadRequest, "work_assignment_request_invalid", err.Error())
		return
	}
	input.Token = strings.TrimSpace(input.Token)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.Token == "" || len(input.Token) > 256 || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 {
		fail(c, http.StatusBadRequest, "work_assignment_request_invalid", "token and idempotency_key are required and bounded")
		return
	}
	if !input.Confirm {
		fail(c, http.StatusBadRequest, "work_assignment_confirmation_required", "confirm must be true")
		return
	}
	principal := CurrentPrincipal(c).UserID
	commitKey := principal + ":" + input.IdempotencyKey
	if prior, found := s.workAssignments.commit(commitKey); found {
		ok(c, prior)
		return
	}
	pending, found := s.workAssignments.take(input.Token, time.Now())
	if !found || (pending.Principal != "" && pending.Principal != principal) {
		fail(c, http.StatusConflict, "work_assignment_token_invalid", "token is expired, already used, or belongs to another principal")
		return
	}
	if pending.StableKey != workAssignmentStableKey(pending.Request) {
		fail(c, http.StatusConflict, "work_assignment_token_invalid", "token payload failed its integrity check")
		return
	}
	var result json.RawMessage
	err := s.server.WithStoppedOperation(c.Request.Context(), func() error {
		worldID, worldDir, currentHash, err := s.currentWorkAssignmentWorld()
		if err != nil {
			return err
		}
		if worldID != pending.WorldID {
			return fmt.Errorf("current world changed from %s to %s", pending.WorldID, worldID)
		}
		if currentHash != pending.Request.ExpectedLevelSHA256 {
			return errors.New("Level.sav changed after prepare")
		}
		backup, created, err := createHostMigrationPreSwitchBackup(s, c.Request.Context(), "stopped", worldID)
		if err != nil {
			return err
		}
		if !created {
			return errors.New("verified world backup was not created")
		}
		backupLevel := filepath.Join(backup.Path, "Level.sav")
		backupHash, err := hostMigrationFileSHA256(backupLevel)
		if err != nil {
			return fmt.Errorf("verify backup Level.sav: %w", err)
		}
		if backupHash != currentHash {
			return errors.New("backup Level.sav hash does not match the active save")
		}
		stageRoot, err := os.MkdirTemp(filepath.Dir(worldDir), ".palpanel-work-assignment-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stageRoot)
		outputDir := filepath.Join(stageRoot, "output")
		raw, err := runWorkAssignmentHelper(c.Request.Context(), "work-fix-existing", worldDir, outputDir, pending.Request)
		if err != nil {
			return err
		}
		fixResult, err := decodeWorkAssignmentFixResult(raw)
		if err != nil || fixResult.Plan.Fixed != 1 {
			if err == nil {
				err = errors.New("helper did not verify fixed=1")
			}
			return err
		}
		stagedLevel := filepath.Join(outputDir, "Level.sav")
		if info, statErr := os.Stat(stagedLevel); statErr != nil || !info.Mode().IsRegular() {
			return errors.New("helper output does not contain a regular Level.sav")
		}
		levelPath := filepath.Join(worldDir, "Level.sav")
		prePublishHash, err := hostMigrationFileSHA256(levelPath)
		if err != nil {
			return err
		}
		if prePublishHash != currentHash {
			return errors.New("Level.sav changed while preparing the staged output")
		}
		publishedHash, err := publishLevelFile(stagedLevel, levelPath)
		if err != nil {
			return err
		}
		verifyRequest := pending.Request
		verifyRequest.ExpectedLevelSHA256 = publishedHash
		verifyRaw, verifyErr := runWorkAssignmentHelper(c.Request.Context(), "work-plan", worldDir, "", verifyRequest)
		if verifyErr == nil {
			var verified workAssignmentPlan
			verified, verifyErr = decodeWorkAssignmentPlan(verifyRaw)
			if verifyErr == nil && verified.Fixed != 1 {
				verifyErr = errors.New("post-publish plan did not report fixed=1")
			}
		}
		if verifyErr != nil {
			if rollbackErr := rollbackLevelFile(levelPath, backupLevel, publishedHash); rollbackErr != nil {
				return fmt.Errorf("post-publish verification failed: %v; rollback failed: %w", verifyErr, rollbackErr)
			}
			return fmt.Errorf("post-publish verification failed and original Level.sav was restored: %w", verifyErr)
		}
		result, err = json.Marshal(gin.H{"committed": true, "changed": fixResult.Changed, "world_id": worldID, "previous_level_sha256": currentHash, "level_sha256": publishedHash, "backup_id": backup.ID, "server_remains_stopped": true, "plan": json.RawMessage(verifyRaw)})
		return err
	})
	if err != nil {
		fail(c, http.StatusConflict, "work_assignment_commit_failed", err.Error())
		return
	}
	s.workAssignments.rememberCommit(commitKey, result)
	ok(c, result)
}

func (s Server) currentWorkAssignmentWorld() (string, string, string, error) {
	worldID, err := readHostMigrationCurrentWorldID(s)
	if err != nil {
		return "", "", "", err
	}
	worldDir := filepath.Join(s.cfg.ServerDirectory(), "Pal", "Saved", "SaveGames", "0", worldID)
	saveRoot := filepath.Dir(worldDir)
	if !pathWithin(saveRoot, worldDir) {
		return "", "", "", errors.New("current world is outside the managed save root")
	}
	levelPath := filepath.Join(worldDir, "Level.sav")
	info, err := os.Lstat(levelPath)
	if err != nil || !info.Mode().IsRegular() {
		return "", "", "", errors.New("current Level.sav is not a regular file")
	}
	levelHash, err := hostMigrationFileSHA256(levelPath)
	return worldID, worldDir, levelHash, err
}

func runWorkAssignmentHelper(ctx context.Context, command, inputDir, outputDir string, request workAssignmentHelperRequest) ([]byte, error) {
	requestFile, err := os.CreateTemp("", ".palpanel-work-request-*.json")
	if err != nil {
		return nil, err
	}
	requestPath := requestFile.Name()
	defer os.Remove(requestPath)
	encoder := json.NewEncoder(requestFile)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(request); err != nil {
		requestFile.Close()
		return nil, err
	}
	if err := requestFile.Sync(); err != nil {
		requestFile.Close()
		return nil, err
	}
	if err := requestFile.Close(); err != nil {
		return nil, err
	}
	args := []string{command, "--input", inputDir}
	if outputDir != "" {
		args = append(args, "--output", outputDir)
	}
	args = append(args, "--request", requestPath)
	return runHostMigrationHelper(ctx, args...)
}

func runWorkAssignmentListHelper(ctx context.Context, inputDir, baseID string) ([]byte, error) {
	return runHostMigrationHelper(ctx, "work-list", "--input", inputDir, "--base-camp-id", baseID)
}

func buildWorkAssignmentHelperRequest(baseID string, input workAssignmentRequest, levelHash string) workAssignmentHelperRequest {
	return workAssignmentHelperRequest{BaseCampID: baseID, WorkerInstanceID: input.WorkerInstanceID, WorkBaseID: input.WorkBaseID, OwnerMapObjectConcreteModelID: input.OwnerMapObjectConcreteModelID, AssignDefineDataID: input.AssignDefineDataID, LocationIndex: input.LocationIndex, ExpectedLevelSHA256: levelHash}
}

func normalizeWorkAssignmentRequest(input *workAssignmentRequest) {
	input.WorkerInstanceID = strings.ToLower(strings.TrimSpace(input.WorkerInstanceID))
	input.WorkBaseID = strings.ToLower(strings.TrimSpace(input.WorkBaseID))
	input.OwnerMapObjectConcreteModelID = strings.ToLower(strings.TrimSpace(input.OwnerMapObjectConcreteModelID))
	input.AssignDefineDataID = strings.TrimSpace(input.AssignDefineDataID)
}

func validateWorkAssignmentRequest(baseID string, input workAssignmentRequest) error {
	for name, value := range map[string]string{"base_id": baseID, "worker_instance_id": input.WorkerInstanceID, "work_base_id": input.WorkBaseID, "owner_map_object_concrete_model_id": input.OwnerMapObjectConcreteModelID} {
		if !workAssignmentGUID.MatchString(value) {
			return fmt.Errorf("%s must be a canonical lowercase GUID", name)
		}
	}
	if input.AssignDefineDataID == "" || len(input.AssignDefineDataID) > 256 {
		return errors.New("assign_define_data_id is required and must not exceed 256 bytes")
	}
	if input.LocationIndex < 0 || input.LocationIndex > 255 {
		return errors.New("location_index must be between 0 and 255")
	}
	return nil
}

func decodeWorkAssignmentPlan(raw []byte) (workAssignmentPlan, error) {
	var plan workAssignmentPlan
	err := json.Unmarshal(raw, &plan)
	return plan, err
}

func decodeWorkAssignmentFixResult(raw []byte) (workAssignmentFixResult, error) {
	var result workAssignmentFixResult
	err := json.Unmarshal(raw, &result)
	return result, err
}

func decodeWorkAssignmentList(raw []byte) (workAssignmentList, error) {
	var result workAssignmentList
	err := json.Unmarshal(raw, &result)
	return result, err
}

func workAssignmentStableKey(request workAssignmentHelperRequest) string {
	raw, _ := json.Marshal(request)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func newOpaqueToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func decodeStrictJSON(c *gin.Context, target any) error {
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("request must contain exactly one JSON object")
	}
	return nil
}
