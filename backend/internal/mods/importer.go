package mods

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/id"
	"palpanel/internal/jobs"
	"palpanel/internal/networkproxy"
	"palpanel/internal/steamcmd"
)

const (
	inspectionLifetime  = 30 * time.Minute
	githubMetadataLimit = 4 << 20
)

type ImportCandidate struct {
	ID            string   `json:"id"`
	SourceType    string   `json:"source_type"`
	FileName      string   `json:"file_name,omitempty"`
	FileSize      int64    `json:"file_size,omitempty"`
	Name          string   `json:"name,omitempty"`
	PackageName   string   `json:"package_name,omitempty"`
	Version       string   `json:"version,omitempty"`
	Action        string   `json:"action"`
	ExistingModID string   `json:"existing_mod_id,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
	Ready         bool     `json:"ready"`
}

type ImportInspection struct {
	ID                  string            `json:"id"`
	SourceType          string            `json:"source_type"`
	Source              string            `json:"source"`
	Candidates          []ImportCandidate `json:"candidates"`
	SelectedCandidateID string            `json:"selected_candidate_id,omitempty"`
	ExpiresAt           string            `json:"expires_at"`
}

type ImportFailure struct {
	Code string
	Err  error
}

func (e ImportFailure) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Err.Error()
}

func (e ImportFailure) Unwrap() error { return e.Err }

type importCandidateRecord struct {
	public       ImportCandidate
	downloadURL  string
	archivePath  string
	modRoot      string
	workshopID   string
	workshopMeta WorkshopItem
}

type importRecord struct {
	inspection ImportInspection
	directory  string
	candidates map[string]*importCandidateRecord
	expiresAt  time.Time
	claimed    bool
	preparing  bool
}

type importRegistry struct {
	mu            sync.Mutex
	records       map[string]*importRecord
	root          string
	now           func() time.Time
	downloader    *safeDownloader
	githubAPIBase string
	maxBytes      int64
	cfg           appconfig.Config
}

func newImportRegistry(cfg appconfig.Config) *importRegistry {
	root := ""
	if strings.TrimSpace(cfg.ServerDirectory()) != "" {
		root = filepath.Join(cfg.WorkshopModsDir(), ".palpanel-imports")
		if err := cfg.ValidateManagedPath(root, false); err != nil {
			root = ""
		} else if err := os.RemoveAll(root); err != nil {
			root = ""
		}
	}
	limit := cfg.MaxUploadBytes
	if limit <= 0 {
		limit = 256 << 20
	}
	proxyService := networkproxy.New(cfg)
	return &importRegistry{
		records:       map[string]*importRecord{},
		root:          root,
		now:           time.Now,
		downloader:    newSafeDownloader(proxyService.InstallProxyURL),
		githubAPIBase: "https://api.github.com",
		maxBytes:      limit,
		cfg:           cfg,
	}
}

func (m Manager) InspectSource(ctx context.Context, source string) (ImportInspection, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return ImportInspection{}, ImportFailure{Code: "source_required", Err: errors.New("a mod source is required")}
	}
	record, err := m.imports.newRecord(source)
	if err != nil {
		return ImportInspection{}, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = m.removeManagedDirectory(record.directory)
		}
	}()

	if workshopID, ok := workshopIDFromSource(source); ok {
		candidateID, err := newImportIdentifier("candidate")
		if err != nil {
			return ImportInspection{}, err
		}
		candidate := &importCandidateRecord{
			public: ImportCandidate{
				ID: candidateID, SourceType: "workshop", FileName: workshopID,
				Action: "unknown", Ready: true,
				Warnings: []string{"PackageName will be verified after the Workshop item is downloaded."},
			},
			workshopID: workshopID,
		}
		record.inspection.SourceType = "workshop"
		record.inspection.SelectedCandidateID = candidate.public.ID
		record.candidates[candidate.public.ID] = candidate
		m.imports.finishRecord(record)
		keep = true
		return record.inspection, nil
	}

	parsed, parseErr := url.Parse(source)
	if parseErr != nil || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return ImportInspection{}, ImportFailure{Code: "invalid_source", Err: errors.New("source must be a Workshop ID/URL, public GitHub release, or public HTTP(S) ZIP")}
	}
	if parsed.User != nil {
		return ImportInspection{}, ImportFailure{Code: "invalid_source", Err: errors.New("URLs containing credentials are not allowed")}
	}
	if strings.EqualFold(parsed.Hostname(), "github.com") || strings.EqualFold(parsed.Hostname(), "www.github.com") {
		if err := m.inspectGitHub(ctx, record, parsed); err != nil {
			return ImportInspection{}, err
		}
	} else {
		candidateID, err := newImportIdentifier("candidate")
		if err != nil {
			return ImportInspection{}, err
		}
		candidate := &importCandidateRecord{public: ImportCandidate{ID: candidateID, SourceType: "https_zip", FileName: cleanFilename(filepath.Base(parsed.Path)), Action: "unknown"}, downloadURL: parsed.String()}
		record.inspection.SourceType = "https_zip"
		record.candidates[candidate.public.ID] = candidate
		if err := m.prepareArchiveCandidate(ctx, record, candidate); err != nil {
			return ImportInspection{}, err
		}
		record.inspection.SelectedCandidateID = candidate.public.ID
	}
	m.imports.finishRecord(record)
	keep = true
	return record.inspection, nil
}

func (m Manager) InspectUpload(ctx context.Context, reader io.Reader, filename string) (ImportInspection, error) {
	record, err := m.imports.newRecord(filename)
	if err != nil {
		return ImportInspection{}, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = m.removeManagedDirectory(record.directory)
		}
	}()
	candidateID, err := newImportIdentifier("candidate")
	if err != nil {
		return ImportInspection{}, err
	}
	candidate := &importCandidateRecord{public: ImportCandidate{ID: candidateID, SourceType: "local_zip", FileName: cleanFilename(filename), Action: "unknown"}}
	record.inspection.SourceType = "local_zip"
	record.candidates[candidate.public.ID] = candidate
	archivePath := filepath.Join(record.directory, "upload.zip")
	size, err := writeLimitedFile(reader, archivePath, m.imports.maxBytes)
	if err != nil {
		return ImportInspection{}, ImportFailure{Code: "archive_too_large", Err: err}
	}
	candidate.archivePath = archivePath
	candidate.public.FileSize = size
	if err := m.analyzeArchiveCandidate(ctx, record, candidate); err != nil {
		return ImportInspection{}, err
	}
	record.inspection.SelectedCandidateID = candidate.public.ID
	m.imports.finishRecord(record)
	keep = true
	return record.inspection, nil
}

func (m Manager) SelectImportCandidate(ctx context.Context, inspectionID, candidateID string) (ImportInspection, error) {
	record, candidate, staged, inspection, ready, err := m.imports.beginSelection(inspectionID, candidateID)
	if err != nil {
		return ImportInspection{}, err
	}
	if ready {
		return inspection, nil
	}
	if err := m.prepareArchiveCandidate(ctx, record, staged); err != nil {
		m.imports.cancelSelection(record)
		return ImportInspection{}, err
	}
	return m.imports.finishSelection(record, candidate, staged)
}

func (m Manager) Import(ctx context.Context, inspectionID, candidateID string) (db.Job, error) {
	record, candidate, err := m.imports.claim(inspectionID, candidateID)
	if err != nil {
		return db.Job{}, err
	}
	if candidate.workshopID != "" {
		if _, err := m.RequireWorkshopLogin(ctx); err != nil {
			m.imports.release(record)
			switch {
			case errors.Is(err, steamcmd.ErrLoginRequired):
				return db.Job{}, ImportFailure{Code: "steam_login_required", Err: steamcmd.ErrLoginRequired}
			case errors.Is(err, steamcmd.ErrInteractiveLogin):
				return db.Job{}, ImportFailure{Code: "steam_login_unsupported", Err: steamcmd.ErrInteractiveLogin}
			case errors.Is(err, steamcmd.ErrInvalidAccountName):
				return db.Job{}, ImportFailure{Code: "invalid_steam_account", Err: errors.New("saved Steam account name is invalid")}
			default:
				return db.Job{}, ImportFailure{Code: "steam_login_verify_failed", Err: errors.New("Steam login verification failed")}
			}
		}
	}
	job, err := m.jobs.Submit(ctx, jobs.ClassLifecycle, "mod_import", "queued mod import", func(jobCtx context.Context, jobID string) {
		defer m.imports.complete(record)
		if candidate.workshopID != "" {
			m.runWorkshopImport(jobCtx, jobID, candidate.workshopID, false, candidate.workshopMeta, record.directory)
			return
		}
		m.update(jobID, "running", 45, "installing validated mod", "")
		if _, err := m.installPrepared(jobCtx, candidate.modRoot, candidate.public.SourceType, "", WorkshopItem{}, false); err != nil {
			m.update(jobID, "failed", 70, "mod installation failed", err.Error())
			return
		}
		m.update(jobID, "completed", 100, "mod import completed", "")
	})
	if err != nil {
		m.imports.release(record)
		return db.Job{}, err
	}
	return job, nil
}

func (m Manager) inspectGitHub(ctx context.Context, record *importRecord, parsed *url.URL) error {
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return ImportFailure{Code: "github_source_invalid", Err: errors.New("GitHub source must identify an owner and repository")}
	}
	owner := parts[0]
	repository := strings.TrimSuffix(parts[1], ".git")
	if owner == "" || repository == "" {
		return ImportFailure{Code: "github_source_invalid", Err: errors.New("GitHub source must identify an owner and repository")}
	}
	record.inspection.SourceType = "github_release"
	if len(parts) >= 6 && parts[2] == "releases" && parts[3] == "download" {
		candidateID, err := newImportIdentifier("candidate")
		if err != nil {
			return err
		}
		candidate := &importCandidateRecord{public: ImportCandidate{ID: candidateID, SourceType: "github_asset", FileName: cleanFilename(parts[len(parts)-1]), Action: "unknown"}, downloadURL: parsed.String()}
		record.candidates[candidate.public.ID] = candidate
		if err := m.prepareArchiveCandidate(ctx, record, candidate); err != nil {
			return err
		}
		record.inspection.SelectedCandidateID = candidate.public.ID
		return nil
	}

	releaseEndpoint := strings.TrimRight(m.imports.githubAPIBase, "/") + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repository) + "/releases/latest"
	if len(parts) >= 5 && parts[2] == "releases" && parts[3] == "tag" {
		tag := strings.Join(parts[4:], "/")
		releaseEndpoint = strings.TrimRight(m.imports.githubAPIBase, "/") + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repository) + "/releases/tags/" + url.PathEscape(tag)
	}
	metadataPath := filepath.Join(record.directory, "release.json")
	if _, err := m.imports.downloader.Download(ctx, releaseEndpoint, metadataPath, githubMetadataLimit); err != nil {
		return ImportFailure{Code: "github_release_failed", Err: err}
	}
	body, err := os.ReadFile(metadataPath)
	if err != nil {
		return err
	}
	var release struct {
		Assets []struct {
			Name               string `json:"name"`
			Size               int64  `json:"size"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return ImportFailure{Code: "github_release_invalid", Err: fmt.Errorf("decode GitHub release: %w", err)}
	}
	for _, asset := range release.Assets {
		if !strings.HasSuffix(strings.ToLower(asset.Name), ".zip") || strings.TrimSpace(asset.BrowserDownloadURL) == "" {
			continue
		}
		candidateID, err := newImportIdentifier("candidate")
		if err != nil {
			return err
		}
		candidate := &importCandidateRecord{
			public:      ImportCandidate{ID: candidateID, SourceType: "github_asset", FileName: asset.Name, FileSize: asset.Size, Action: "unknown", Ready: false},
			downloadURL: asset.BrowserDownloadURL,
		}
		record.candidates[candidate.public.ID] = candidate
	}
	if len(record.candidates) == 0 {
		return ImportFailure{Code: "github_no_zip_assets", Err: errors.New("GitHub release has no ZIP assets")}
	}
	if len(record.candidates) == 1 {
		for _, candidate := range record.candidates {
			if err := m.prepareArchiveCandidate(ctx, record, candidate); err != nil {
				return err
			}
			record.inspection.SelectedCandidateID = candidate.public.ID
		}
	}
	return nil
}

func (m Manager) prepareArchiveCandidate(ctx context.Context, record *importRecord, candidate *importCandidateRecord) error {
	directory := filepath.Join(record.directory, candidate.public.ID)
	if err := m.removeManagedDirectory(directory); err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	archivePath := filepath.Join(directory, "archive.zip")
	size, err := m.imports.downloader.Download(ctx, candidate.downloadURL, archivePath, m.imports.maxBytes)
	if err != nil {
		return ImportFailure{Code: "download_failed", Err: err}
	}
	candidate.archivePath = archivePath
	candidate.public.FileSize = size
	return m.analyzeArchiveCandidate(ctx, record, candidate)
}

func (m Manager) analyzeArchiveCandidate(ctx context.Context, record *importRecord, candidate *importCandidateRecord) error {
	extractDirectory := filepath.Join(filepath.Dir(candidate.archivePath), "extracted")
	if err := m.removeManagedDirectory(extractDirectory); err != nil {
		return err
	}
	if err := extractArchive(candidate.archivePath, extractDirectory); err != nil {
		return ImportFailure{Code: "archive_invalid", Err: err}
	}
	modRoot, metadata, err := inspectModDirectory(extractDirectory)
	if err != nil {
		return ImportFailure{Code: "mod_metadata_invalid", Err: err}
	}
	candidate.modRoot = modRoot
	candidate.public.Name = metadata.Name
	candidate.public.PackageName = metadata.PackageName
	candidate.public.Version = metadata.Version
	candidate.public.Ready = true
	existing, err := m.findModByPackage(ctx, metadata.PackageName)
	if err != nil {
		return err
	}
	if existing != nil {
		candidate.public.Action = "update"
		candidate.public.ExistingModID = existing.ID
		candidate.public.Warnings = []string{"The existing enabled state and record identity will be preserved."}
	} else {
		candidate.public.Action = "new"
		candidate.public.Warnings = []string{"The new mod will be installed disabled."}
	}
	return nil
}

func (m Manager) findModByPackage(ctx context.Context, packageName string) (*db.Mod, error) {
	items, err := m.store.ListMods(ctx)
	if err != nil {
		return nil, err
	}
	var found *db.Mod
	for i := range items {
		if strings.EqualFold(strings.TrimSpace(items[i].PackageName), strings.TrimSpace(packageName)) {
			item := items[i]
			if found != nil {
				return nil, fmt.Errorf("multiple installed mods use PackageName %q", packageName)
			}
			found = &item
		}
	}
	return found, nil
}

func (m Manager) installPrepared(ctx context.Context, sourceRoot, source, workshopID string, meta WorkshopItem, enableNew bool) (db.Mod, error) {
	validatedRoot, metadata, err := inspectModDirectory(sourceRoot)
	if err != nil {
		return db.Mod{}, err
	}
	existing, err := m.findModByPackage(ctx, metadata.PackageName)
	if err != nil {
		return db.Mod{}, err
	}
	modID := id.New("mod")
	enabled := enableNew
	target, err := safeModTarget(m.cfg.WorkshopModsDir(), modID, "")
	if err != nil {
		return db.Mod{}, err
	}
	var previous db.Mod
	if existing != nil {
		previous = *existing
		modID = previous.ID
		enabled = previous.Enabled
		target, err = safeModTarget(m.cfg.WorkshopModsDir(), modID, previous.Path)
		if err != nil {
			return db.Mod{}, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return db.Mod{}, err
	}
	backup := target + ".palpanel-backup-" + id.New("swap")
	targetExisted := false
	if info, statErr := os.Stat(target); statErr == nil {
		if !info.IsDir() {
			return db.Mod{}, fmt.Errorf("mod target is not a directory: %s", target)
		}
		targetExisted = true
		if err := os.Rename(target, backup); err != nil {
			return db.Mod{}, fmt.Errorf("backup existing mod: %w", err)
		}
	} else if !os.IsNotExist(statErr) {
		return db.Mod{}, statErr
	}
	rollbackFiles := func() error {
		_ = m.removeManagedDirectory(target)
		if targetExisted {
			return os.Rename(backup, target)
		}
		return nil
	}
	if err := os.Rename(validatedRoot, target); err != nil {
		_ = rollbackFiles()
		return db.Mod{}, fmt.Errorf("activate staged mod: %w", err)
	}

	mod := db.Mod{
		ID: modID, Name: metadata.Name, Source: source, PackageName: metadata.PackageName,
		Path: target, Version: metadata.Version, Enabled: enabled, WorkshopID: workshopID,
	}
	if existing != nil {
		mod.CreatedAt = previous.CreatedAt
	}
	if workshopID != "" {
		mod.SteamURL = steamWorkshopPageURL + workshopID
	}
	applyWorkshopMetadata(&mod, meta)
	settingsPath := m.cfg.PalModSettingsPath()
	settingsBody, settingsErr := os.ReadFile(settingsPath)
	settingsExisted := settingsErr == nil
	if settingsErr != nil && !os.IsNotExist(settingsErr) {
		_ = rollbackFiles()
		return db.Mod{}, settingsErr
	}
	rollbackDatabase := func() {
		if existing != nil {
			_ = m.store.UpsertMod(context.Background(), previous)
		} else {
			_ = m.store.DeleteMod(context.Background(), mod.ID)
		}
		if settingsExisted {
			_ = os.WriteFile(settingsPath, settingsBody, 0o644)
		} else {
			_ = os.Remove(settingsPath)
		}
	}
	if err := m.store.UpsertMod(ctx, mod); err != nil {
		_ = rollbackFiles()
		return db.Mod{}, err
	}
	if enabled {
		if err := m.rewriteActiveMods(ctx); err != nil {
			rollbackDatabase()
			_ = rollbackFiles()
			return db.Mod{}, err
		}
		if err := m.store.SetKV(ctx, "pending_restart", "true"); err != nil {
			rollbackDatabase()
			_ = rollbackFiles()
			return db.Mod{}, err
		}
	}
	if targetExisted {
		if err := m.removeManagedDirectory(backup); err != nil {
			// The new directory and database row are already committed. Leaving a
			// backup is safer than reporting a failed installation at this point.
			return mod, nil
		}
	}
	return mod, nil
}

func (m Manager) runWorkshopImport(ctx context.Context, jobID, itemID string, enableNew bool, meta WorkshopItem, directory string) {
	downloadRoot := filepath.Join(directory, "workshop")
	if err := m.cfg.ValidateManagedPath(downloadRoot, false); err != nil {
		m.update(jobID, "failed", 20, "Workshop staging directory is unsafe", err.Error())
		return
	}
	if err := os.MkdirAll(downloadRoot, 0o700); err != nil {
		m.update(jobID, "failed", 20, "create Workshop staging directory failed", err.Error())
		return
	}
	if err := m.cfg.ValidateManagedPath(downloadRoot, false); err != nil {
		m.update(jobID, "failed", 20, "Workshop staging directory is unsafe", err.Error())
		return
	}
	if err := m.downloadWorkshopTo(ctx, jobID, itemID, downloadRoot); err != nil {
		errorCode := ""
		message := "Workshop download failed"
		switch {
		case errors.Is(err, steamcmd.ErrSteamMobileConfirmationRequired):
			errorCode = "steam_mobile_confirmation_required"
			message = "Steam Mobile login confirmation is required"
		case errors.Is(err, steamcmd.ErrSteamGuardRequired):
			errorCode = "steam_guard_required"
			message = "Steam Guard verification is required"
		case errors.Is(err, steamcmd.ErrInvalidCredentials):
			errorCode = "invalid_steam_credentials"
			message = "Saved Steam credentials were rejected"
		case errors.Is(err, steamcmd.ErrLoginRequired):
			errorCode = "steam_login_required"
			message = "Steam credentials are required"
		}
		m.updateWithCode(jobID, "failed", 50, message, err.Error(), errorCode)
		return
	}
	m.update(jobID, "running", 80, "validating and installing mod", "")
	if _, err := m.installPrepared(ctx, filepath.Join(downloadRoot, itemID), "workshop", itemID, meta, enableNew); err != nil {
		m.update(jobID, "failed", 80, "Workshop mod validation or installation failed", err.Error())
		return
	}
	m.update(jobID, "completed", 100, "Workshop download completed", "")
}

func (r *importRegistry) newRecord(source string) (*importRecord, error) {
	if strings.TrimSpace(r.root) == "" {
		return nil, ImportFailure{Code: "import_staging_unavailable", Err: errors.New("mod import staging requires PALPANEL_SERVER_DIR")}
	}
	if err := os.MkdirAll(r.root, 0o700); err != nil {
		return nil, err
	}
	inspectionID, err := newImportIdentifier("inspection")
	if err != nil {
		return nil, err
	}
	now := r.now().UTC()
	record := &importRecord{
		directory:  filepath.Join(r.root, inspectionID),
		candidates: map[string]*importCandidateRecord{},
		expiresAt:  now.Add(inspectionLifetime),
	}
	record.inspection = ImportInspection{ID: filepath.Base(record.directory), Source: source, ExpiresAt: record.expiresAt.Format(time.RFC3339Nano)}
	if err := os.MkdirAll(record.directory, 0o700); err != nil {
		return nil, err
	}
	return record, nil
}

func (r *importRegistry) finishRecord(record *importRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleanupExpiredLocked()
	record.expiresAt = r.now().UTC().Add(inspectionLifetime)
	record.inspection.ExpiresAt = record.expiresAt.Format(time.RFC3339Nano)
	r.refreshInspection(record)
	r.records[record.inspection.ID] = record
}

func (r *importRegistry) refreshInspection(record *importRecord) {
	record.inspection.Candidates = make([]ImportCandidate, 0, len(record.candidates))
	for _, candidate := range record.candidates {
		record.inspection.Candidates = append(record.inspection.Candidates, candidate.public)
	}
	sort.Slice(record.inspection.Candidates, func(i, j int) bool {
		return record.inspection.Candidates[i].FileName < record.inspection.Candidates[j].FileName
	})
}

func (r *importRegistry) beginSelection(inspectionID, candidateID string) (*importRecord, *importCandidateRecord, *importCandidateRecord, ImportInspection, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, err := r.recordLocked(inspectionID)
	if err != nil {
		return nil, nil, nil, ImportInspection{}, false, err
	}
	if record.claimed {
		return nil, nil, nil, ImportInspection{}, false, ImportFailure{Code: "inspection_claimed", Err: errors.New("import inspection has already been consumed")}
	}
	if record.preparing {
		return nil, nil, nil, ImportInspection{}, false, ImportFailure{Code: "inspection_unavailable", Err: errors.New("another candidate is currently being inspected")}
	}
	candidate, ok := record.candidates[strings.TrimSpace(candidateID)]
	if !ok {
		return nil, nil, nil, ImportInspection{}, false, ImportFailure{Code: "candidate_not_found", Err: errors.New("import candidate was not found")}
	}
	if candidate.public.Ready {
		record.inspection.SelectedCandidateID = candidate.public.ID
		r.refreshInspection(record)
		return record, candidate, nil, record.inspection, true, nil
	}
	if candidate.downloadURL == "" {
		return nil, nil, nil, ImportInspection{}, false, ImportFailure{Code: "candidate_unavailable", Err: errors.New("candidate has no downloadable ZIP")}
	}
	record.preparing = true
	staged := *candidate
	return record, candidate, &staged, ImportInspection{}, false, nil
}

func (r *importRegistry) cancelSelection(record *importRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current := r.records[record.inspection.ID]; current == record {
		current.preparing = false
		if !r.now().UTC().Before(current.expiresAt) {
			delete(r.records, current.inspection.ID)
			_ = r.removeManagedDirectory(current.directory)
		}
	}
}

func (r *importRegistry) finishSelection(record *importRecord, candidate, staged *importCandidateRecord) (ImportInspection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.records[record.inspection.ID]
	if current != record || !current.preparing || current.claimed {
		return ImportInspection{}, ImportFailure{Code: "inspection_unavailable", Err: errors.New("inspection is no longer available")}
	}
	current.preparing = false
	if !r.now().UTC().Before(current.expiresAt) {
		delete(r.records, current.inspection.ID)
		_ = r.removeManagedDirectory(current.directory)
		return ImportInspection{}, ImportFailure{Code: "inspection_expired", Err: errors.New("import inspection has expired")}
	}
	*candidate = *staged
	current.inspection.SelectedCandidateID = candidate.public.ID
	r.refreshInspection(current)
	return current.inspection, nil
}

func (r *importRegistry) claim(inspectionID, candidateID string) (*importRecord, *importCandidateRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, err := r.recordLocked(inspectionID)
	if err != nil {
		return nil, nil, err
	}
	if record.claimed {
		return nil, nil, ImportFailure{Code: "inspection_claimed", Err: errors.New("import inspection has already been consumed")}
	}
	if record.preparing {
		return nil, nil, ImportFailure{Code: "inspection_unavailable", Err: errors.New("a candidate inspection is still in progress")}
	}
	if strings.TrimSpace(candidateID) == "" {
		candidateID = record.inspection.SelectedCandidateID
	}
	candidate, ok := record.candidates[candidateID]
	if !ok {
		return nil, nil, ImportFailure{Code: "candidate_not_found", Err: errors.New("select an import candidate first")}
	}
	if !candidate.public.Ready {
		return nil, nil, ImportFailure{Code: "candidate_not_ready", Err: errors.New("selected candidate has not been inspected")}
	}
	record.claimed = true
	return record, candidate, nil
}

func (r *importRegistry) release(record *importRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current := r.records[record.inspection.ID]; current == record {
		current.claimed = false
	}
}

func (r *importRegistry) complete(record *importRecord) {
	r.mu.Lock()
	if current := r.records[record.inspection.ID]; current == record {
		delete(r.records, record.inspection.ID)
	}
	r.mu.Unlock()
	_ = r.removeManagedDirectory(record.directory)
}

func (r *importRegistry) cleanupExpiredLocked() {
	now := r.now().UTC()
	for inspectionID, record := range r.records {
		if !record.claimed && !record.preparing && !now.Before(record.expiresAt) {
			delete(r.records, inspectionID)
			_ = r.removeManagedDirectory(record.directory)
		}
	}
}

func (r *importRegistry) recordLocked(inspectionID string) (*importRecord, error) {
	inspectionID = strings.TrimSpace(inspectionID)
	record, ok := r.records[inspectionID]
	if ok && !record.claimed && !record.preparing && !r.now().UTC().Before(record.expiresAt) {
		delete(r.records, inspectionID)
		_ = r.removeManagedDirectory(record.directory)
		r.cleanupExpiredLocked()
		return nil, ImportFailure{Code: "inspection_expired", Err: errors.New("import inspection has expired")}
	}
	r.cleanupExpiredLocked()
	if !ok {
		return nil, ImportFailure{Code: "inspection_not_found", Err: errors.New("import inspection was not found")}
	}
	return record, nil
}

func (r *importRegistry) removeManagedDirectory(path string) error {
	if err := r.cfg.ValidateManagedPath(path, false); err != nil {
		return err
	}
	if err := r.cfg.ValidateManagedPath(path, false); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func newImportIdentifier(prefix string) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate import identifier: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(random[:]), nil
}

func safeModTarget(root, modID, storedPath string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := strings.TrimSpace(storedPath)
	if target == "" {
		target = filepath.Join(root, modID)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("mod target must be inside %s", root)
	}
	return target, nil
}

func workshopIDFromSource(source string) (string, bool) {
	source = strings.TrimSpace(source)
	if workshopIDPattern.MatchString(source) {
		return source, true
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.User != nil || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) ||
		(!strings.EqualFold(parsed.Hostname(), "steamcommunity.com") && !strings.EqualFold(parsed.Hostname(), "www.steamcommunity.com")) ||
		!strings.EqualFold(strings.TrimSuffix(parsed.Path, "/"), "/sharedfiles/filedetails") {
		return "", false
	}
	itemID := strings.TrimSpace(parsed.Query().Get("id"))
	return itemID, workshopIDPattern.MatchString(itemID)
}

func writeLimitedFile(reader io.Reader, destination string, limit int64) (int64, error) {
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(file, io.LimitReader(reader, limit+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return 0, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destination)
		return 0, closeErr
	}
	if written > limit {
		_ = os.Remove(destination)
		return 0, fmt.Errorf("archive exceeds the %d byte limit", limit)
	}
	return written, nil
}
