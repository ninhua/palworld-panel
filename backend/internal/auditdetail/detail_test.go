package auditdetail

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEncodeKeepsDetailedResponseAndRedactsSecrets(t *testing.T) {
	encoded := Encode(202, true, map[string]any{
		"job": map[string]any{
			"id": "job-1", "type": "panel_update", "status": "waiting",
			"message": "已进入任务队列", "token": "do-not-store",
		},
		"authorization": "Bearer abc.def.ghi",
	})
	var detail map[string]any
	if err := json.Unmarshal([]byte(encoded), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail["http_status"] != float64(202) || detail["ok"] != true {
		t.Fatalf("unexpected envelope: %#v", detail)
	}
	data := detail["data"].(map[string]any)
	job := data["job"].(map[string]any)
	if job["id"] != "job-1" || job["message"] != "已进入任务队列" {
		t.Fatalf("detailed response missing: %#v", job)
	}
	if job["token"] != "[REDACTED]" || data["authorization"] != "[REDACTED]" {
		t.Fatalf("secret was not redacted: %#v", data)
	}
}

func TestEncodeBoundsLargeResponses(t *testing.T) {
	items := make([]any, 100)
	for index := range items {
		items[index] = strings.Repeat("x", 4096)
	}
	encoded := Encode(200, true, map[string]any{"items": items})
	if len(encoded) > maxEncodedSize {
		t.Fatalf("encoded audit detail is too large: %d", len(encoded))
	}
	if !strings.Contains(encoded, "TRUNCATED") && !strings.Contains(encoded, `"truncated":true`) {
		t.Fatalf("large response was not marked truncated: %s", encoded)
	}
}

func TestEncodeRedactsBearerText(t *testing.T) {
	encoded := Encode(500, false, map[string]any{"message": "upstream rejected Bearer abc.def.ghi"})
	if strings.Contains(encoded, "abc.def.ghi") || !strings.Contains(encoded, "[REDACTED]") {
		t.Fatalf("bearer token was not redacted: %s", encoded)
	}
}

func TestEncodeRedactsCredentialAssignmentsInText(t *testing.T) {
	encoded := Encode(200, true, map[string]any{"message": "password=hunter2 token: abc123 safe=value"})
	if strings.Contains(encoded, "hunter2") || strings.Contains(encoded, "abc123") {
		t.Fatalf("credential assignment was not redacted: %s", encoded)
	}
	if !strings.Contains(encoded, "password=[REDACTED]") || !strings.Contains(encoded, "token: [REDACTED]") {
		t.Fatalf("credential labels were not preserved: %s", encoded)
	}
}
