package mods

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/docker"
	"palpanel/internal/jobs"
)

func TestLocalImportInstallsDisabledAndUpdatesByPackageName(t *testing.T) {
	manager, store := newImportTestManager(t)
	first := modArchive(t, "Example", "ExamplePackage", "1.0", "first")
	inspection, err := manager.InspectUpload(context.Background(), bytes.NewReader(first), "example.zip")
	if err != nil {
		t.Fatal(err)
	}
	candidate := onlyCandidate(t, inspection)
	if candidate.Action != "new" || !candidate.Ready || candidate.PackageName != "ExamplePackage" {
		t.Fatalf("new inspection candidate = %#v", candidate)
	}
	job, err := manager.Import(context.Background(), inspection.ID, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitForModJob(t, store, job.ID, "completed")
	installed, err := store.ListMods(context.Background())
	if err != nil || len(installed) != 1 {
		t.Fatalf("installed mods = %#v, %v", installed, err)
	}
	firstRecord := installed[0]
	if firstRecord.Enabled || firstRecord.PackageName != "ExamplePackage" {
		t.Fatalf("new import = %#v", firstRecord)
	}
	if _, configured, err := store.GetKV(context.Background(), "pending_restart"); err != nil || configured {
		t.Fatalf("disabled new import changed pending restart: configured=%v err=%v", configured, err)
	}

	if _, err := manager.SetEnabled(context.Background(), firstRecord.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetKV(context.Background(), "pending_restart", "false"); err != nil {
		t.Fatal(err)
	}
	second := modArchive(t, "Example Updated", "examplepackage", "2.0", "second")
	updatedInspection, err := manager.InspectUpload(context.Background(), bytes.NewReader(second), "update.zip")
	if err != nil {
		t.Fatal(err)
	}
	updatedCandidate := onlyCandidate(t, updatedInspection)
	if updatedCandidate.Action != "update" || updatedCandidate.ExistingModID != firstRecord.ID {
		t.Fatalf("update inspection candidate = %#v", updatedCandidate)
	}
	updateJob, err := manager.Import(context.Background(), updatedInspection.ID, updatedCandidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitForModJob(t, store, updateJob.ID, "completed")
	updated, err := store.GetMod(context.Background(), firstRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Enabled || updated.ID != firstRecord.ID || updated.Version != "2.0" {
		t.Fatalf("updated mod = %#v", updated)
	}
	payload, err := os.ReadFile(filepath.Join(updated.Path, "payload.txt"))
	if err != nil || string(payload) != "second" {
		t.Fatalf("updated payload = %q, %v", payload, err)
	}
	pending, _, err := store.GetKV(context.Background(), "pending_restart")
	if err != nil || pending != "true" {
		t.Fatalf("pending restart = %q, %v", pending, err)
	}
	backups, err := filepath.Glob(updated.Path + ".palpanel-backup-*")
	if err != nil || len(backups) != 0 {
		t.Fatalf("unexpected backups: %#v, %v", backups, err)
	}
}

func TestImportInspectionExpiresAndCanOnlyBeClaimedOnce(t *testing.T) {
	manager, store := newImportTestManager(t)
	inspection, err := manager.InspectUpload(context.Background(), bytes.NewReader(modArchive(t, "One", "OnePackage", "1", "one")), "one.zip")
	if err != nil {
		t.Fatal(err)
	}
	directory := manager.imports.records[inspection.ID].directory
	manager.imports.now = func() time.Time { return time.Now().Add(inspectionLifetime + time.Minute) }
	if _, err := manager.Import(context.Background(), inspection.ID, inspection.SelectedCandidateID); importFailureCode(err) != "inspection_expired" {
		t.Fatalf("expired inspection error = %v", err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("expired inspection directory was retained: %v", err)
	}

	manager.imports.now = time.Now
	second, err := manager.InspectUpload(context.Background(), bytes.NewReader(modArchive(t, "Two", "TwoPackage", "1", "two")), "two.zip")
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	started := make(chan struct{})
	blocker, err := manager.jobs.Submit(context.Background(), jobs.ClassLifecycle, "blocker", "waiting", func(_ context.Context, jobID string) {
		close(started)
		<-release
		_ = manager.jobs.Update(jobID, "completed", 100, "done", "")
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	job, err := manager.Import(context.Background(), second.ID, second.SelectedCandidateID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Import(context.Background(), second.ID, second.SelectedCandidateID); importFailureCode(err) != "inspection_claimed" {
		t.Fatalf("second claim error = %v", err)
	}
	close(release)
	waitForModJob(t, store, blocker.ID, "completed")
	waitForModJob(t, store, job.ID, "completed")
}

func TestImportIdentifiersAreCryptographicallyRandom(t *testing.T) {
	manager, _ := newImportTestManager(t)
	first, err := manager.InspectUpload(context.Background(), bytes.NewReader(modArchive(t, "One", "OnePackage", "1", "one")), "one.zip")
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.InspectUpload(context.Background(), bytes.NewReader(modArchive(t, "Two", "TwoPackage", "1", "two")), "two.zip")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || len(first.ID) != len("inspection_")+32 || len(first.Candidates[0].ID) != len("candidate_")+32 {
		t.Fatalf("unexpected import identifiers: %q %q %q", first.ID, second.ID, first.Candidates[0].ID)
	}
}

func TestGitHubReleaseRequiresSelectionForMultipleZipAssets(t *testing.T) {
	manager, _ := newImportTestManager(t)
	zipOne := modArchive(t, "GitHub One", "GitHubOne", "1", "one")
	zipTwo := modArchive(t, "GitHub Two", "GitHubTwo", "1", "two")
	manager.imports.downloader.resolver = staticResolver{
		"api.github.com":    {{IP: net.ParseIP("8.8.8.8")}},
		"downloads.example": {{IP: net.ParseIP("8.8.4.4")}},
	}
	redirectCheck := manager.imports.downloader.client.CheckRedirect
	manager.imports.downloader.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body []byte
		switch request.URL.Hostname() {
		case "api.github.com":
			body = []byte(`{"assets":[{"name":"one.zip","size":100,"browser_download_url":"https://downloads.example/one.zip"},{"name":"two.zip","size":200,"browser_download_url":"https://downloads.example/two.zip"},{"name":"notes.txt","browser_download_url":"https://downloads.example/notes.txt"}]}`)
		case "downloads.example":
			if strings.HasSuffix(request.URL.Path, "one.zip") {
				body = zipOne
			} else {
				body = zipTwo
			}
		default:
			return nil, errors.New("unexpected URL")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: request}, nil
	}), CheckRedirect: redirectCheck}

	inspection, err := manager.InspectSource(context.Background(), "https://github.com/example/project/releases/latest")
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Candidates) != 2 || inspection.SelectedCandidateID != "" {
		t.Fatalf("GitHub inspection = %#v", inspection)
	}
	var selectedID string
	for _, candidate := range inspection.Candidates {
		if candidate.FileName == "one.zip" {
			selectedID = candidate.ID
		}
		if candidate.Ready || candidate.Action != "unknown" {
			t.Fatalf("unselected candidate = %#v", candidate)
		}
	}
	selected, err := manager.SelectImportCandidate(context.Background(), inspection.ID, selectedID)
	if err != nil {
		t.Fatal(err)
	}
	selectedCandidate := candidateByID(t, selected, selectedID)
	if !selectedCandidate.Ready || selectedCandidate.PackageName != "GitHubOne" || selectedCandidate.Action != "new" {
		t.Fatalf("selected candidate = %#v", selectedCandidate)
	}
}

func TestHTTPZipSourceIsInspected(t *testing.T) {
	manager, _ := newImportTestManager(t)
	archive := modArchive(t, "HTTP ZIP", "HTTPZipPackage", "1", "http")
	manager.imports.downloader.resolver = staticResolver{
		"public.example": {{IP: net.ParseIP("8.8.8.8")}},
	}
	redirectCheck := manager.imports.downloader.client.CheckRedirect
	manager.imports.downloader.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        make(http.Header),
			Body:          io.NopCloser(bytes.NewReader(archive)),
			ContentLength: int64(len(archive)),
			Request:       request,
		}, nil
	}), CheckRedirect: redirectCheck}

	inspection, err := manager.InspectSource(context.Background(), "http://public.example/mod.zip")
	if err != nil {
		t.Fatal(err)
	}
	if inspection.SourceType != "https_zip" || len(inspection.Candidates) != 1 {
		t.Fatalf("HTTP ZIP inspection = %#v", inspection)
	}
	candidate := inspection.Candidates[0]
	if !candidate.Ready || candidate.PackageName != "HTTPZipPackage" || candidate.Action != "new" {
		t.Fatalf("HTTP ZIP candidate = %#v", candidate)
	}
}

func TestAtomicInstallRestoresExistingDirectoryWhenSettingsSnapshotFails(t *testing.T) {
	manager, store := newImportTestManager(t)
	target := filepath.Join(manager.cfg.WorkshopModsDir(), "mod_existing")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	previous := db.Mod{ID: "mod_existing", Name: "Old", Source: "upload", PackageName: "RollbackPackage", Path: target, Version: "1", Enabled: true}
	if err := store.UpsertMod(context.Background(), previous); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(manager.imports.root, "rollback", "Mod")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "Info.json"), []byte(`{"Name":"New","PackageName":"RollbackPackage","Version":"2"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(manager.cfg.PalModSettingsPath(), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.installPrepared(context.Background(), staged, "local_zip", "", WorkshopItem{}, false); err == nil {
		t.Fatal("expected settings snapshot failure")
	}
	if body, err := os.ReadFile(filepath.Join(target, "old.txt")); err != nil || string(body) != "old" {
		t.Fatalf("old directory was not restored: %q, %v", body, err)
	}
	stored, err := store.GetMod(context.Background(), previous.ID)
	if err != nil || stored.Version != "1" || !stored.Enabled {
		t.Fatalf("old database record was not preserved: %#v, %v", stored, err)
	}
}

func TestAtomicInstallRollsBackFilesWhenDatabaseUpdateFails(t *testing.T) {
	manager, store := newImportTestManager(t)
	target := filepath.Join(manager.cfg.WorkshopModsDir(), "mod_existing")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	previous := db.Mod{ID: "mod_existing", Name: "Old", Source: "upload", PackageName: "RollbackPackage", Path: target, Version: "1", Enabled: false}
	if err := store.UpsertMod(context.Background(), previous); err != nil {
		t.Fatal(err)
	}
	connection, err := sql.Open("sqlite", manager.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Exec(`CREATE TRIGGER reject_mod_update BEFORE UPDATE ON mods BEGIN SELECT RAISE(FAIL, 'forced update failure'); END`); err != nil {
		t.Fatal(err)
	}
	staged := writePreparedMod(t, manager, "RollbackPackage", "2", "new")
	if _, err := manager.installPrepared(context.Background(), staged, "local_zip", "", WorkshopItem{}, false); err == nil {
		t.Fatal("expected database update failure")
	}
	assertPreviousModRestored(t, store, previous, target)
}

func TestAtomicInstallRollsBackDatabaseSettingsAndFilesWhenRestartFlagFails(t *testing.T) {
	manager, store := newImportTestManager(t)
	target := filepath.Join(manager.cfg.WorkshopModsDir(), "mod_existing")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	previous := db.Mod{ID: "mod_existing", Name: "Old", Source: "upload", PackageName: "RollbackPackage", Path: target, Version: "1", Enabled: true}
	if err := store.UpsertMod(context.Background(), previous); err != nil {
		t.Fatal(err)
	}
	settingsPath := manager.cfg.PalModSettingsPath()
	settingsBody := []byte("[PalModSettings]\nbGlobalEnableMod=true\nActiveModList=RollbackPackage\n")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, settingsBody, 0o640); err != nil {
		t.Fatal(err)
	}
	connection, err := sql.Open("sqlite", manager.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Exec(`CREATE TRIGGER reject_restart_flag BEFORE INSERT ON kv WHEN NEW.key = 'pending_restart' BEGIN SELECT RAISE(FAIL, 'forced kv failure'); END`); err != nil {
		t.Fatal(err)
	}
	staged := writePreparedMod(t, manager, "RollbackPackage", "2", "new")
	if _, err := manager.installPrepared(context.Background(), staged, "local_zip", "", WorkshopItem{}, false); err == nil {
		t.Fatal("expected pending restart update failure")
	}
	assertPreviousModRestored(t, store, previous, target)
	if body, err := os.ReadFile(settingsPath); err != nil || !bytes.Equal(body, settingsBody) {
		t.Fatalf("settings were not restored: %q, %v", body, err)
	}
}

func TestWorkshopSourceRecognition(t *testing.T) {
	for _, source := range []string{
		"123456789",
		"http://steamcommunity.com/sharedfiles/filedetails/?id=123456789",
		"https://steamcommunity.com/sharedfiles/filedetails/?id=123456789",
		"https://www.steamcommunity.com/sharedfiles/filedetails/?id=123456789",
	} {
		if itemID, ok := workshopIDFromSource(source); !ok || itemID != "123456789" {
			t.Errorf("workshopIDFromSource(%q) = %q, %v", source, itemID, ok)
		}
	}
	if _, ok := workshopIDFromSource("https://example.com/?id=123456789"); ok {
		t.Fatal("untrusted host was recognized as Workshop")
	}
	for _, source := range []string{
		"ftp://steamcommunity.com/sharedfiles/filedetails/?id=123456789",
		"https://steamcommunity.com/not-a-workshop-page?id=123456789",
	} {
		if _, ok := workshopIDFromSource(source); ok {
			t.Fatalf("non-canonical Workshop source was recognized: %s", source)
		}
	}
}

func newImportTestManager(t *testing.T) (Manager, *db.Store) {
	t.Helper()
	root := t.TempDir()
	cfg := appconfig.Config{
		DataDir: root, ServerDir: filepath.Join(root, "server"), UploadsDir: filepath.Join(root, "uploads"),
		BackupsDir: filepath.Join(root, "backups"), LogsDir: filepath.Join(root, "logs"), DBPath: filepath.Join(root, "panel.db"),
		MaxUploadBytes: 16 << 20, WorkshopAppID: "1623730", DockerBinary: "false", DockerImage: "test",
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	executor := jobs.New(store, 1)
	manager := NewManager(cfg, store, docker.NewRunner(cfg), executor)
	return manager, store
}

func modArchive(t *testing.T, name, packageName, version, payload string) []byte {
	t.Helper()
	return createZip(t, []zipTestEntry{
		{name: "Mod/Info.json", body: `{"Name":"` + name + `","PackageName":"` + packageName + `","Version":"` + version + `"}`},
		{name: "Mod/payload.txt", body: payload},
	})
}

func writePreparedMod(t *testing.T, manager Manager, packageName, version, payload string) string {
	t.Helper()
	directory := filepath.Join(manager.imports.root, "prepared-"+version, "Mod")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "Info.json"), []byte(`{"Name":"Updated","PackageName":"`+packageName+`","Version":"`+version+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "payload.txt"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	return directory
}

func assertPreviousModRestored(t *testing.T, store *db.Store, previous db.Mod, target string) {
	t.Helper()
	if body, err := os.ReadFile(filepath.Join(target, "old.txt")); err != nil || string(body) != "old" {
		t.Fatalf("old directory was not restored: %q, %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(target, "payload.txt")); !os.IsNotExist(err) {
		t.Fatalf("new payload was retained: %v", err)
	}
	stored, err := store.GetMod(context.Background(), previous.ID)
	if err != nil || stored.Version != previous.Version || stored.Enabled != previous.Enabled {
		t.Fatalf("old database record was not restored: %#v, %v", stored, err)
	}
}

func onlyCandidate(t *testing.T, inspection ImportInspection) ImportCandidate {
	t.Helper()
	if len(inspection.Candidates) != 1 {
		t.Fatalf("candidates = %#v", inspection.Candidates)
	}
	return inspection.Candidates[0]
}

func candidateByID(t *testing.T, inspection ImportInspection, id string) ImportCandidate {
	t.Helper()
	for _, candidate := range inspection.Candidates {
		if candidate.ID == id {
			return candidate
		}
	}
	t.Fatalf("candidate %s not found", id)
	return ImportCandidate{}
}

func waitForModJob(t *testing.T, store *db.Store, jobID, status string) db.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := store.GetJob(context.Background(), jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == status {
			return job
		}
		if job.Status == "failed" && status != "failed" {
			t.Fatalf("job failed: %#v", job)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach %s", jobID, status)
	return db.Job{}
}

func importFailureCode(err error) string {
	var failure ImportFailure
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}
