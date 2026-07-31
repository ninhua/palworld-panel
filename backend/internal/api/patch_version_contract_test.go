package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	openAPIPatchVersionPattern   = regexp.MustCompile(`^\s*version:\s*\{type:\s*string,\s*const:\s*([^}\s]+)\s*\}\s*$`)
	generatedPatchVersionPattern = regexp.MustCompile(`^\s*"version":\s*"([^"]+)";\s*$`)
)

func TestPatchVersionArtifactsStayInSync(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	openAPIVersion := readOpenAPIPatchVersion(t, filepath.Join(root, "docs", "openapi.yaml"))
	generatedVersion := readGeneratedPatchVersion(t, filepath.Join(root, "frontend", "src", "api", "generated", "contracts.ts"))

	if openAPIVersion != patchVersion {
		t.Fatalf("OpenAPI PatchInfo version = %q, runtime patchVersion = %q", openAPIVersion, patchVersion)
	}
	if generatedVersion != patchVersion {
		t.Fatalf("generated contracts PatchInfo version = %q, runtime patchVersion = %q; run npm run generate:api-types and commit the result", generatedVersion, patchVersion)
	}
}

func readOpenAPIPatchVersion(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read OpenAPI document: %v", err)
	}
	inPatchInfo := false
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "PatchInfo:" {
			inPatchInfo = true
			continue
		}
		if inPatchInfo && trimmed == "PatchInfoEnvelope:" {
			break
		}
		if !inPatchInfo {
			continue
		}
		if matches := openAPIPatchVersionPattern.FindStringSubmatch(line); len(matches) == 2 {
			return matches[1]
		}
	}
	t.Fatalf("OpenAPI PatchInfo patch.version const was not found")
	return ""
}

func readGeneratedPatchVersion(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated API contracts: %v", err)
	}
	inPatchInfo := false
	inPatch := false
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == `"PatchInfo": {` {
			inPatchInfo = true
			continue
		}
		if !inPatchInfo {
			continue
		}
		if trimmed == `"patch": {` {
			inPatch = true
			continue
		}
		if inPatch && trimmed == "};" {
			break
		}
		if inPatch {
			if matches := generatedPatchVersionPattern.FindStringSubmatch(line); len(matches) == 2 {
				return matches[1]
			}
		}
	}
	t.Fatalf("generated contracts PatchInfo patch.version was not found")
	return ""
}
