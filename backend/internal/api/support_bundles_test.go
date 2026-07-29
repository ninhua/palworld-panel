package api

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeSupportTextRedactsSensitiveMaterial(t *testing.T) {
	input := `Authorization: Bearer abcdefghijklmnop password=secret123 {"token":"json-secret"} path=/home/container/palworld 192.168.1.8 76561198012345678 01234567-89ab-cdef-0123-456789abcdef`
	output := sanitizeSupportText(input)
	for _, forbidden := range []string{"abcdefghijklmnop", "secret123", "json-secret", "/home/container", "192.168.1.8", "76561198012345678", "01234567-89ab-cdef-0123-456789abcdef"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("sanitized output still contains %q: %s", forbidden, output)
		}
	}
	for _, marker := range []string{"<redacted>", "<path>", "<ip>", "<numeric-id>", "<guid>"} {
		if !strings.Contains(output, marker) {
			t.Fatalf("sanitized output missing %q: %s", marker, output)
		}
	}
}

func TestSanitizeSupportValueRedactsByKey(t *testing.T) {
	value := map[string]any{
		"password":    "hello",
		"server_path": "/srv/palworld",
		"steam_id":    "76561198012345678",
		"actor":       "admin",
		"nested":      map[string]any{"token": "abc"},
	}
	result := sanitizeSupportValue(value, "").(map[string]any)
	if result["password"] != "<redacted>" || result["server_path"] != "<path>" || result["steam_id"] != "<redacted-id>" || result["actor"] != "<redacted-id>" {
		t.Fatalf("unexpected redaction result: %#v", result)
	}
	nested := result["nested"].(map[string]any)
	if nested["token"] != "<redacted>" {
		t.Fatalf("nested secret not redacted: %#v", nested)
	}
}

func TestWriteSupportBundleArchiveCreatesDeterministicSafeEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.zip")
	entries := map[string][]byte{"b.json": []byte("b"), "a.json": []byte("a")}
	if err := writeSupportBundleArchive(path, entries); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if len(reader.File) != 2 || reader.File[0].Name != "a.json" || reader.File[1].Name != "b.json" {
		t.Fatalf("unexpected ZIP order: %#v", reader.File)
	}
	for _, file := range reader.File {
		if file.Mode().Perm() != 0o600 {
			t.Fatalf("entry %s mode = %o", file.Name, file.Mode().Perm())
		}
	}
}

func TestWriteSupportBundleArchiveRejectsUnsafeEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.zip")
	if err := writeSupportBundleArchive(path, map[string][]byte{"../secret": []byte("x")}); err == nil {
		t.Fatal("expected unsafe entry rejection")
	}
}

func TestCollectSanitizedLogTailsUsesAllowlistAndLimits(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "palpanel.log"), []byte("token=topsecret\n/home/container/test\n10.0.0.2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.json"), []byte(`{"token":"topsecret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result := collectSanitizedLogTails(dir)
	if len(result) != 1 {
		t.Fatalf("expected one log entry, got %d", len(result))
	}
	for name, data := range result {
		if name != "logs/recent-1.tail.txt" {
			t.Fatalf("unexpected log entry %q", name)
		}
		text := string(data)
		if strings.Contains(text, "topsecret") || strings.Contains(text, "/home/container") || strings.Contains(text, "10.0.0.2") {
			t.Fatalf("log was not sanitized: %s", text)
		}
	}
}

func TestSupportBundleIDPattern(t *testing.T) {
	if !supportBundleIDPattern.MatchString(strings.Repeat("a", 32)) {
		t.Fatal("valid id rejected")
	}
	for _, value := range []string{"../x", strings.Repeat("a", 31), strings.Repeat("A", 32)} {
		if supportBundleIDPattern.MatchString(value) {
			t.Fatalf("invalid id accepted: %q", value)
		}
	}
}

func TestSupportBundleDownloadNameIgnoresMetadataFileName(t *testing.T) {
	metadata := supportBundleMetadata{
		ID:        strings.Repeat("a", 32),
		FileName:  "../../unsafe.zip",
		CreatedAt: "2026-07-30T01:02:03Z",
	}
	if got, want := supportBundleDownloadName(metadata), "palpanel-support-20260730T010203Z-aaaaaaaa.zip"; got != want {
		t.Fatalf("download name = %q, want %q", got, want)
	}
}
