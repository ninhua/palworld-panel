package api

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/id"
	"palpanel/internal/saveindex"
)

const defaultSaveImportInspectionTTL = 15 * time.Minute

var (
	errSaveImportInspectionNotFound = errors.New("save import inspection not found")
	errSaveImportInspectionExpired  = errors.New("save import inspection expired")
	errSaveImportSelectionRequired  = errors.New("save import candidate selection required")
	errSaveImportCandidateInvalid   = errors.New("save import candidate is invalid")
	errSaveImportAlreadyClaimed     = errors.New("save import inspection already claimed")
)

type saveImportCandidate struct {
	ID           string   `json:"id"`
	RelativePath string   `json:"relative_path"`
	WorldID      string   `json:"world_id,omitempty"`
	PlayerCount  int      `json:"player_count"`
	LevelSHA256  string   `json:"level_sha256"`
	LevelSize    int64    `json:"level_size"`
	Valid        bool     `json:"valid"`
	Warnings     []string `json:"warnings"`
	Errors       []string `json:"errors"`
}

type saveImportInspection struct {
	ID                  string                `json:"id"`
	FileName            string                `json:"file_name"`
	Name                string                `json:"name,omitempty"`
	Candidates          []saveImportCandidate `json:"candidates"`
	SelectedCandidateID string                `json:"selected_candidate_id"`
	RequiresSelection   bool                  `json:"requires_selection"`
	ExpiresAt           time.Time             `json:"expires_at"`
	Root                string                `json:"-"`
	Claimed             bool                  `json:"-"`
	expired             bool
	purgeScheduled      bool
	expiryTimer         *time.Timer
}

func (inspection saveImportInspection) requiresSelection() bool {
	valid := 0
	for _, candidate := range inspection.Candidates {
		if candidate.Valid {
			valid++
		}
	}
	return valid > 1 && inspection.SelectedCandidateID == ""
}

type saveImportInspectionStore struct {
	mu    sync.Mutex
	ttl   time.Duration
	now   func() time.Time
	items map[string]*saveImportInspection
}

func newSaveImportInspectionStore(ttl time.Duration) *saveImportInspectionStore {
	if ttl <= 0 {
		ttl = defaultSaveImportInspectionTTL
	}
	return &saveImportInspectionStore{ttl: ttl, now: time.Now, items: make(map[string]*saveImportInspection)}
}

func (store *saveImportInspectionStore) put(inspection *saveImportInspection) {
	store.mu.Lock()
	inspection.ExpiresAt = store.now().Add(store.ttl)
	inspection.RequiresSelection = inspection.requiresSelection()
	store.items[inspection.ID] = inspection
	inspection.expiryTimer = time.AfterFunc(store.ttl, func() { store.expire(inspection.ID) })
	store.mu.Unlock()
}

func (store *saveImportInspectionStore) get(id string) (*saveImportInspection, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	inspection, err := store.getLocked(id)
	if err != nil {
		return nil, err
	}
	copy := *inspection
	copy.Candidates = append([]saveImportCandidate(nil), inspection.Candidates...)
	return &copy, nil
}

func (store *saveImportInspectionStore) selectCandidate(id, candidateID string) (*saveImportInspection, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	inspection, err := store.getLocked(id)
	if err != nil {
		return nil, err
	}
	if inspection.Claimed {
		return nil, errSaveImportAlreadyClaimed
	}
	for _, candidate := range inspection.Candidates {
		if candidate.ID == candidateID {
			if !candidate.Valid {
				return nil, errSaveImportCandidateInvalid
			}
			inspection.SelectedCandidateID = candidateID
			inspection.RequiresSelection = false
			copy := *inspection
			copy.Candidates = append([]saveImportCandidate(nil), inspection.Candidates...)
			return &copy, nil
		}
	}
	return nil, errSaveImportCandidateInvalid
}

func (store *saveImportInspectionStore) claim(id string) (*saveImportInspection, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	inspection, err := store.getLocked(id)
	if err != nil {
		return nil, err
	}
	if inspection.Claimed {
		return nil, errSaveImportAlreadyClaimed
	}
	if inspection.SelectedCandidateID == "" {
		return nil, errSaveImportSelectionRequired
	}
	inspection.Claimed = true
	if inspection.expiryTimer != nil {
		inspection.expiryTimer.Stop()
		inspection.expiryTimer = nil
	}
	copy := *inspection
	copy.Candidates = append([]saveImportCandidate(nil), inspection.Candidates...)
	return &copy, nil
}

func (store *saveImportInspectionStore) finalize(id string) {
	store.mu.Lock()
	inspection := store.items[id]
	if inspection != nil {
		inspection.Claimed = true
		inspection.expired = false
		if inspection.expiryTimer != nil {
			inspection.expiryTimer.Stop()
			inspection.expiryTimer = nil
		}
		if !inspection.purgeScheduled {
			inspection.purgeScheduled = true
			current := inspection
			time.AfterFunc(store.ttl, func() {
				store.mu.Lock()
				if store.items[id] == current {
					delete(store.items, id)
				}
				store.mu.Unlock()
			})
		}
	}
	store.mu.Unlock()
	if inspection != nil && inspection.Root != "" {
		_ = os.RemoveAll(inspection.Root)
		store.mu.Lock()
		inspection.Root = ""
		store.mu.Unlock()
	}
}

func (store *saveImportInspectionStore) discard(id string) {
	store.mu.Lock()
	inspection := store.items[id]
	if inspection != nil && inspection.expiryTimer != nil {
		inspection.expiryTimer.Stop()
	}
	delete(store.items, id)
	store.mu.Unlock()
	if inspection != nil && inspection.Root != "" {
		_ = os.RemoveAll(inspection.Root)
	}
}

func (store *saveImportInspectionStore) getLocked(id string) (*saveImportInspection, error) {
	inspection := store.items[id]
	if inspection == nil {
		return nil, errSaveImportInspectionNotFound
	}
	if inspection.expired {
		return nil, errSaveImportInspectionExpired
	}
	if !inspection.Claimed && !store.now().Before(inspection.ExpiresAt) {
		store.expireLocked(inspection)
		return nil, errSaveImportInspectionExpired
	}
	return inspection, nil
}

func (store *saveImportInspectionStore) expire(id string) {
	store.mu.Lock()
	inspection := store.items[id]
	if inspection != nil && !inspection.Claimed && !store.now().Before(inspection.ExpiresAt) {
		store.expireLocked(inspection)
	}
	store.mu.Unlock()
}

func (store *saveImportInspectionStore) expireLocked(inspection *saveImportInspection) {
	if inspection.expired {
		return
	}
	inspection.expired = true
	root := inspection.Root
	inspection.Root = ""
	if root != "" {
		_ = os.RemoveAll(root)
	}
	if !inspection.purgeScheduled {
		inspection.purgeScheduled = true
		id := inspection.ID
		time.AfterFunc(store.ttl, func() {
			store.mu.Lock()
			if current := store.items[id]; current == inspection && current.expired {
				delete(store.items, id)
			}
			store.mu.Unlock()
		})
	}
}

func inspectSaveImportCandidates(ctx context.Context, cfg appconfig.Config, root string) ([]saveImportCandidate, error) {
	var worlds []string
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if current != root && saveArchivePathExcluded(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(entry.Name(), "Level.sav") {
			worlds = append(worlds, filepath.Dir(current))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(worlds, func(i, j int) bool {
		iPath, _ := filepath.Rel(root, worlds[i])
		jPath, _ := filepath.Rel(root, worlds[j])
		return filepath.ToSlash(iPath) < filepath.ToSlash(jPath)
	})
	candidates := make([]saveImportCandidate, 0, len(worlds))
	for _, world := range worlds {
		relative, err := filepath.Rel(root, world)
		if err != nil || !pathWithin(root, world) {
			return nil, errors.New("unsafe save world path")
		}
		relative = filepath.ToSlash(relative)
		candidate := saveImportCandidate{
			ID:           stableSaveImportCandidateID(relative),
			RelativePath: relative,
			Warnings:     []string{},
			Errors:       []string{},
		}
		if relative != "." {
			base := filepath.Base(world)
			candidate.WorldID = base
		}
		levelPath := filepath.Join(world, "Level.sav")
		level, err := os.Open(levelPath)
		if err != nil {
			candidate.Errors = append(candidate.Errors, "Level.sav could not be read")
			candidates = append(candidates, candidate)
			continue
		}
		hash := sha256.New()
		candidate.LevelSize, err = io.Copy(hash, level)
		closeErr := level.Close()
		if err != nil || closeErr != nil {
			candidate.Errors = append(candidate.Errors, "Level.sav could not be read")
			candidates = append(candidates, candidate)
			continue
		}
		candidate.LevelSHA256 = fmt.Sprintf("%x", hash.Sum(nil))
		if candidate.LevelSize == 0 {
			candidate.Errors = append(candidate.Errors, "Level.sav is empty")
			candidates = append(candidates, candidate)
			continue
		}
		candidateConfig := cfg
		candidateConfig.SaveIndexCacheDir = filepath.Join(cfg.SaveIndexCacheDir, candidate.ID)
		if err := os.MkdirAll(candidateConfig.SaveIndexCacheDir, 0o700); err != nil {
			candidate.Errors = append(candidate.Errors, "save index parser could not be prepared")
			candidates = append(candidates, candidate)
			continue
		}
		manager := saveindex.NewManager(candidateConfig)
		manager.SetSourcePath(world)
		index, status, err := manager.Rebuild(ctx)
		if err != nil {
			message := "save index parser rejected Level.sav"
			if status.ErrorCode != "" {
				message += " (" + status.ErrorCode + ")"
			}
			candidate.Errors = append(candidate.Errors, message)
			candidates = append(candidates, candidate)
			continue
		}
		candidate.Valid = true
		candidate.PlayerCount = len(index.Players)
		candidate.Warnings = redactSaveImportMessages(index.Warnings, root, world)
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func stableSaveImportCandidateID(relative string) string {
	hash := sha256.Sum256([]byte(filepath.ToSlash(relative)))
	return fmt.Sprintf("candidate_%x", hash[:12])
}

func redactSaveImportMessages(messages []string, privatePaths ...string) []string {
	redacted := make([]string, 0, len(messages))
	for _, message := range messages {
		for _, privatePath := range privatePaths {
			if privatePath == "" {
				continue
			}
			message = strings.ReplaceAll(message, privatePath, "<redacted>")
			message = strings.ReplaceAll(message, filepath.ToSlash(privatePath), "<redacted>")
		}
		redacted = append(redacted, message)
	}
	return redacted
}

func (s Server) inspectSaveSourceImport(c *gin.Context) {
	inspection, status, code, err := s.prepareSaveSourceImportInspection(c)
	if err != nil {
		fail(c, status, code, err.Error())
		return
	}
	ok(c, inspection)
}

func (s Server) selectSaveSourceImportCandidate(c *gin.Context) {
	var input struct {
		CandidateID string `json:"candidate_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "save_world_selection_invalid", "candidate_id is required")
		return
	}
	inspection, err := s.saveImports.selectCandidate(c.Param("id"), strings.TrimSpace(input.CandidateID))
	if err != nil {
		status, code, message := saveImportInspectionError(err)
		fail(c, status, code, message)
		return
	}
	ok(c, inspection)
}

func (s Server) handleSaveSourceImport(c *gin.Context) {
	mediaType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if mediaType == "multipart/form-data" {
		inspection, status, code, err := s.prepareSaveSourceImportInspection(c)
		if err != nil {
			fail(c, status, code, err.Error())
			return
		}
		valid := validSaveImportCandidateCount(inspection.Candidates)
		if valid == 0 {
			s.saveImports.discard(inspection.ID)
			fail(c, http.StatusBadRequest, "save_world_invalid", "the archive does not contain a parseable non-empty Level.sav")
			return
		}
		if inspection.RequiresSelection {
			c.JSON(http.StatusConflict, gin.H{"ok": false, "error": gin.H{
				"code": "save_world_selection_required", "message": "multiple valid save worlds require explicit selection",
				"inspection_id": inspection.ID, "candidates": inspection.Candidates, "expires_at": inspection.ExpiresAt,
			}})
			return
		}
		s.importClaimedSaveSource(c, inspection.ID, inspection.Name, "")
		return
	}
	var input struct {
		InspectionID string `json:"inspection_id" binding:"required"`
		CandidateID  string `json:"candidate_id"`
		Name         string `json:"name"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.InspectionID) == "" {
		fail(c, http.StatusBadRequest, "save_import_request_invalid", "inspection_id is required")
		return
	}
	if strings.TrimSpace(input.CandidateID) != "" {
		if _, err := s.saveImports.selectCandidate(input.InspectionID, input.CandidateID); err != nil {
			status, code, message := saveImportInspectionError(err)
			fail(c, status, code, message)
			return
		}
	}
	s.importClaimedSaveSource(c, input.InspectionID, input.Name, "")
}

func (s Server) prepareSaveSourceImportInspection(c *gin.Context) (*saveImportInspection, int, string, error) {
	if s.saveImports == nil {
		return nil, http.StatusInternalServerError, "save_import_unavailable", errors.New("save import inspection is unavailable")
	}
	limit := s.cfg.MaxUploadBytes
	if limit <= 0 {
		limit = 256 << 20
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, multipartRequestLimit(limit))
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		return nil, http.StatusBadRequest, "save_archive_required", errors.New("a ZIP or TAR archive is required")
	}
	defer file.Close()
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	format, err := saveArchiveFormatForName(header.Filename)
	if err != nil {
		return nil, http.StatusBadRequest, "save_archive_invalid", err
	}
	inspectionID := id.New("inspect")
	inspectionRoot := filepath.Join(s.cfg.SaveSourcesDir, ".inspections", inspectionID)
	if !pathWithin(s.cfg.SaveSourcesDir, inspectionRoot) {
		return nil, http.StatusInternalServerError, "save_import_prepare_failed", errors.New("save inspection path is invalid")
	}
	if err := os.MkdirAll(inspectionRoot, 0o700); err != nil {
		return nil, http.StatusInternalServerError, "save_import_prepare_failed", errors.New("save inspection could not be prepared")
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(inspectionRoot)
		}
	}()
	uploadPath := filepath.Join(inspectionRoot, "upload.archive")
	upload, err := os.OpenFile(uploadPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, http.StatusInternalServerError, "save_import_prepare_failed", errors.New("save upload could not be prepared")
	}
	written, copyErr := io.CopyN(upload, file, limit+1)
	closeErr := upload.Close()
	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		return nil, http.StatusBadRequest, "save_archive_read_failed", errors.New("save archive could not be read")
	}
	if closeErr != nil {
		return nil, http.StatusInternalServerError, "save_import_prepare_failed", errors.New("save upload could not be stored")
	}
	if written > limit {
		return nil, http.StatusRequestEntityTooLarge, "save_archive_too_large", errors.New("save archive exceeds upload limit")
	}
	extracted := filepath.Join(inspectionRoot, "files")
	if err := extractSaveArchive(uploadPath, extracted, limit, format); err != nil {
		return nil, http.StatusBadRequest, "save_archive_invalid", errors.New("save archive could not be extracted safely")
	}
	inspectConfig := s.cfg
	inspectConfig.SaveIndexCacheDir = filepath.Join(inspectionRoot, "index-cache")
	candidates, err := inspectSaveImportCandidates(c.Request.Context(), inspectConfig, extracted)
	if err != nil {
		return nil, http.StatusBadRequest, "save_archive_invalid", errors.New("save archive candidates could not be inspected")
	}
	if len(candidates) == 0 {
		return nil, http.StatusBadRequest, "save_world_missing", errors.New("Level.sav was not found in the archive")
	}
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		name = defaultSaveSourceName(header.Filename)
	}
	inspection := &saveImportInspection{
		ID: inspectionID, FileName: filepath.Base(header.Filename), Name: name, Root: inspectionRoot, Candidates: candidates,
	}
	if validSaveImportCandidateCount(candidates) == 1 {
		for _, candidate := range candidates {
			if candidate.Valid {
				inspection.SelectedCandidateID = candidate.ID
				break
			}
		}
	}
	s.saveImports.put(inspection)
	keep = true
	return inspection, http.StatusOK, "", nil
}

func (s Server) importClaimedSaveSource(c *gin.Context, inspectionID, requestedName, _ string) {
	inspection, err := s.saveImports.claim(inspectionID)
	if err != nil {
		status, code, message := saveImportInspectionError(err)
		fail(c, status, code, message)
		return
	}
	defer s.saveImports.finalize(inspectionID)
	var selected *saveImportCandidate
	for i := range inspection.Candidates {
		if inspection.Candidates[i].ID == inspection.SelectedCandidateID && inspection.Candidates[i].Valid {
			selected = &inspection.Candidates[i]
			break
		}
	}
	if selected == nil {
		fail(c, http.StatusConflict, "save_world_selection_invalid", "the selected save world is invalid")
		return
	}
	extracted := filepath.Join(inspection.Root, "files")
	worldPath, err := safeSaveImportCandidatePath(extracted, selected.RelativePath)
	if err != nil || verifySaveImportCandidate(worldPath, *selected) != nil {
		fail(c, http.StatusBadRequest, "save_world_selection_invalid", "the selected save world failed integrity validation")
		return
	}
	sourceID := id.New("save")
	destination := filepath.Join(s.cfg.SaveSourcesDir, sourceID)
	if !pathWithin(s.cfg.SaveSourcesDir, destination) {
		fail(c, http.StatusInternalServerError, "save_import_store_failed", "save source path is invalid")
		return
	}
	if err := os.Rename(worldPath, destination); err != nil {
		fail(c, http.StatusInternalServerError, "save_import_store_failed", "save source could not be stored")
		return
	}
	if err := removeNestedSaveImportCandidates(destination, selected.RelativePath, inspection.Candidates); err != nil {
		_ = os.RemoveAll(destination)
		fail(c, http.StatusInternalServerError, "save_import_store_failed", "unselected save worlds could not be removed")
		return
	}
	if err := removeImportedWorldOption(destination); err != nil {
		_ = os.RemoveAll(destination)
		fail(c, http.StatusInternalServerError, "save_import_store_failed", "WorldOption.sav could not be removed from the imported world")
		return
	}
	name := strings.TrimSpace(requestedName)
	if name == "" {
		name = inspection.Name
	}
	if name == "" {
		name = defaultSaveSourceName(inspection.FileName)
	}
	source := db.SaveSource{ID: sourceID, Name: name, Kind: "import", Path: destination}
	if err := s.store.UpsertSaveSource(c.Request.Context(), source); err != nil {
		_ = os.RemoveAll(destination)
		fail(c, http.StatusInternalServerError, "save_source_store_failed", "save source metadata could not be stored")
		return
	}
	ok(c, source)
}

func removeImportedWorldOption(worldPath string) error {
	entries, err := os.ReadDir(worldPath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(entry.Name(), "WorldOption.sav") {
			continue
		}
		target := filepath.Join(worldPath, entry.Name())
		if !pathWithin(worldPath, target) {
			return errors.New("unsafe WorldOption.sav path")
		}
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func safeSaveImportCandidatePath(root, relative string) (string, error) {
	relative = filepath.ToSlash(strings.TrimSpace(relative))
	if relative == "." {
		return root, nil
	}
	clean, err := cleanSaveArchivePath(relative)
	if err != nil || filepath.ToSlash(clean) != relative {
		return "", errors.New("unsafe save candidate path")
	}
	target := filepath.Join(root, clean)
	if !pathWithin(root, target) {
		return "", errors.New("unsafe save candidate path")
	}
	return target, nil
}

func verifySaveImportCandidate(worldPath string, candidate saveImportCandidate) error {
	level, err := os.Open(filepath.Join(worldPath, "Level.sav"))
	if err != nil {
		return err
	}
	defer level.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, level)
	if err != nil || size != candidate.LevelSize || fmt.Sprintf("%x", hash.Sum(nil)) != candidate.LevelSHA256 {
		return errors.New("save candidate changed after inspection")
	}
	return nil
}

func removeNestedSaveImportCandidates(destination, selectedRelative string, candidates []saveImportCandidate) error {
	selectedClean, err := cleanSaveArchivePath(selectedRelative)
	if selectedRelative == "." {
		selectedClean = "."
		err = nil
	}
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		if candidate.RelativePath == selectedRelative {
			continue
		}
		candidateClean, err := cleanSaveArchivePath(candidate.RelativePath)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(selectedClean, candidateClean)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		target := filepath.Join(destination, relative)
		if !pathWithin(destination, target) {
			return errors.New("unsafe nested save candidate path")
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
	}
	return nil
}

func cleanupSaveImportInspections(cfg appconfig.Config) error {
	root := filepath.Join(cfg.SaveSourcesDir, ".inspections")
	if cfg.SaveSourcesDir == "" || !pathWithin(cfg.SaveSourcesDir, root) {
		return errors.New("save inspection cleanup path is invalid")
	}
	return os.RemoveAll(root)
}

func validSaveImportCandidateCount(candidates []saveImportCandidate) int {
	count := 0
	for _, candidate := range candidates {
		if candidate.Valid {
			count++
		}
	}
	return count
}

func saveImportInspectionError(err error) (int, string, string) {
	switch {
	case errors.Is(err, errSaveImportInspectionExpired):
		return http.StatusGone, "save_import_inspection_expired", "save import inspection expired"
	case errors.Is(err, errSaveImportInspectionNotFound):
		return http.StatusNotFound, "save_import_inspection_not_found", "save import inspection was not found"
	case errors.Is(err, errSaveImportAlreadyClaimed):
		return http.StatusConflict, "save_import_already_claimed", "save import inspection was already claimed"
	case errors.Is(err, errSaveImportSelectionRequired):
		return http.StatusConflict, "save_world_selection_required", "select a valid save world before importing"
	case errors.Is(err, errSaveImportCandidateInvalid):
		return http.StatusConflict, "save_world_selection_invalid", "the selected save world is invalid"
	default:
		return http.StatusBadRequest, "save_import_invalid", "save import request is invalid"
	}
}
