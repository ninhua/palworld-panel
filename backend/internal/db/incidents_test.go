package db

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestIncidentStoreDeduplicatesReopensAndTracksTimeline(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "incidents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	first, event, err := store.UpsertIncident(ctx, IncidentInput{DedupeKey: "job:backup:io", Kind: "job_failure", Severity: "error", Source: "job:backup", Title: "备份失败", Summary: "write failed", Details: map[string]any{"job_id": "job_1"}})
	if err != nil || first.Status != "open" || first.Occurrences != 1 || event.Type != "opened" {
		t.Fatalf("first incident = %#v, %#v, %v", first, event, err)
	}
	second, event, err := store.UpsertIncident(ctx, IncidentInput{DedupeKey: "job:backup:io", Kind: "job_failure", Severity: "critical", Source: "job:backup", Title: "备份失败", Summary: "write failed again"})
	if err != nil || second.ID != first.ID || second.Occurrences != 2 || second.Severity != "critical" || event.Type != "occurred" {
		t.Fatalf("second incident = %#v, %#v, %v", second, event, err)
	}
	resolved, event, err := store.SetIncidentStatus(ctx, first.ID, "resolved", "admin", "fixed")
	if err != nil || resolved.Status != "resolved" || resolved.ResolvedAt == "" || event.Type != "resolved" {
		t.Fatalf("resolved = %#v, %#v, %v", resolved, event, err)
	}
	reopened, event, err := store.UpsertIncident(ctx, IncidentInput{DedupeKey: "job:backup:io", Kind: "job_failure", Severity: "error", Source: "job:backup", Title: "备份失败", Summary: "failed after repair"})
	if err != nil || reopened.Status != "open" || reopened.Occurrences != 3 || event.Type != "reopened" {
		t.Fatalf("reopened = %#v, %#v, %v", reopened, event, err)
	}
	events, err := store.ListIncidentEvents(ctx, first.ID, 20)
	if err != nil || len(events) != 4 {
		t.Fatalf("events = %#v, %v", events, err)
	}
}

func TestFailedJobsCreateOneSanitizedIncidentPerJob(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.CreateJob(ctx, "job_backup_1", "backup", "queued backup"); err != nil {
		t.Fatal(err)
	}
	secretError := "/home/container/private/save.sav: permission denied"
	if err := store.UpdateJobWithCode(ctx, "job_backup_1", "failed", 50, "backup archive write failed", secretError, "backup_write_failed"); err != nil {
		t.Fatal(err)
	}
	// Repeating the terminal write for the same job must not increase occurrences.
	if err := store.UpdateJobWithCode(ctx, "job_backup_1", "failed", 50, "backup archive write failed", secretError, "backup_write_failed"); err != nil {
		t.Fatal(err)
	}
	items, total, err := store.ListIncidents(ctx, IncidentListFilter{Source: "job:backup", Limit: 20})
	if err != nil || total != 1 || len(items) != 1 || items[0].Occurrences != 1 {
		t.Fatalf("incidents = %#v, total=%d, %v", items, total, err)
	}
	encoded := items[0].Summary
	if strings.Contains(encoded, "/home/container") || strings.Contains(encoded, "save.sav") {
		t.Fatalf("raw job error leaked into incident summary: %q", encoded)
	}
	if items[0].Details["error_code"] != "backup_write_failed" || items[0].Details["job_id"] != "job_backup_1" {
		t.Fatalf("details = %#v", items[0].Details)
	}
}

func TestAlertsSynchronizeIncidentStatus(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	alert := Alert{ID: "alert_1", Severity: "warning", Title: "REST unavailable", Message: "three failed probes", Source: "monitor:rest", Status: "open"}
	if err := store.CreateAlert(ctx, alert); err != nil {
		t.Fatal(err)
	}
	items, _, err := store.ListIncidents(ctx, IncidentListFilter{Source: alert.Source, Limit: 10})
	if err != nil || len(items) != 1 || items[0].Status != "open" {
		t.Fatalf("incident = %#v, %v", items, err)
	}
	if err := store.AckAlert(ctx, alert.ID); err != nil {
		t.Fatal(err)
	}
	acknowledged, err := store.GetIncident(ctx, items[0].ID)
	if err != nil || acknowledged.Status != "acknowledged" {
		t.Fatalf("acknowledged = %#v, %v", acknowledged, err)
	}
	if err := store.ResolveAlert(ctx, alert.ID); err != nil {
		t.Fatal(err)
	}
	resolved, err := store.GetIncident(ctx, items[0].ID)
	if err != nil || resolved.Status != "resolved" {
		t.Fatalf("resolved = %#v, %v", resolved, err)
	}
}

func TestWebhookDeliveryQueueIsEnabledExplicitly(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.SetIncidentWebhookEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	incident, _, err := store.UpsertIncident(ctx, IncidentInput{DedupeKey: "test:webhook", Kind: "test", Severity: "info", Source: "test", Title: "test"})
	if err != nil {
		t.Fatal(err)
	}
	due, err := store.ListDueIncidentDeliveries(ctx, "9999-12-31T23:59:59Z", 20)
	if err != nil || len(due) != 1 || due[0].Incident.ID != incident.ID {
		t.Fatalf("due = %#v, %v", due, err)
	}
	if err := store.MarkIncidentDeliveryDelivered(ctx, due[0].Delivery.ID); err != nil {
		t.Fatal(err)
	}
	deliveries, err := store.ListIncidentDeliveries(ctx, incident.ID, 20)
	if err != nil || len(deliveries) != 1 || deliveries[0].Status != "delivered" || deliveries[0].Attempts != 1 {
		t.Fatalf("deliveries = %#v, %v", deliveries, err)
	}
}

func TestConcurrentIncidentUpsertsAreSerialized(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := store.UpsertIncident(ctx, IncidentInput{
				DedupeKey: "monitor:rest-unavailable", Kind: "monitor_alert", Severity: "warning",
				Source: "monitor:rest", Title: "REST unavailable", Summary: "probe failed",
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent upsert failed: %v", err)
		}
	}
	items, total, err := store.ListIncidents(ctx, IncidentListFilter{Source: "monitor:rest", Limit: 20})
	if err != nil || total != 1 || len(items) != 1 || items[0].Occurrences != workers {
		t.Fatalf("incidents = %#v, total=%d, err=%v", items, total, err)
	}
}

func TestIncidentTextAndDetailsAreRedacted(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "redaction.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	item, _, err := store.UpsertIncident(context.Background(), IncidentInput{
		DedupeKey: "redaction:test", Kind: "test", Severity: "error", Source: "test", Title: "delivery failed",
		Summary: `failed at /home/container/private/save.sav endpoint=https://hooks.example.com/private/path token=secret-value`,
		Details: map[string]any{"safe": "C:\\PalServer\\secret.log", "log_path": "/private/log", "nested": map[string]any{"password": "secret", "code": "failed"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded := item.Summary
	if strings.Contains(encoded, "save.sav") || strings.Contains(encoded, "/private/path") || strings.Contains(encoded, "secret-value") {
		t.Fatalf("incident summary was not redacted: %q", encoded)
	}
	if strings.Contains(string(mustJSON(t, item.Details)), "secret.log") || strings.Contains(string(mustJSON(t, item.Details)), "log_path") || strings.Contains(string(mustJSON(t, item.Details)), "password") {
		t.Fatalf("incident details were not redacted: %#v", item.Details)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestConcurrentFailedJobWritesCreateOneOccurrence(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "job-concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.CreateJob(ctx, "job_backup_concurrent", "backup", "queued"); err != nil {
		t.Fatal(err)
	}
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- store.UpdateJobWithCode(ctx, "job_backup_concurrent", "failed", 50, "backup failed", "/private/save.sav", "backup_write_failed")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := store.ListIncidents(ctx, IncidentListFilter{Source: "job:backup", Limit: 20})
	if err != nil || total != 1 || len(items) != 1 || items[0].Occurrences != 1 {
		t.Fatalf("incidents = %#v, total=%d, err=%v", items, total, err)
	}
}
