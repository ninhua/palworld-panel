package auditdetail

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	maxDepth       = 8
	maxArrayItems  = 32
	maxStringRunes = 2048
	maxEncodedSize = 32 * 1024
)

var (
	bearerPattern     = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]+`)
	assignmentPattern = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|access[_-]?key|authorization|cookie)(\s*[:=]\s*)([^\s,;]+)`)
)

// Encode returns a bounded, recursively redacted JSON description of an API response.
// The result is suitable for PalPanel's existing audit_logs.message column.
func Encode(status int, success bool, payload any) string {
	key := "data"
	if !success {
		key = "error"
	}
	detail := map[string]any{
		"http_status": status,
		"ok":          success,
		key:           sanitize(payload, 0),
	}
	encoded, err := json.Marshal(detail)
	if err == nil && len(encoded) <= maxEncodedSize {
		return string(encoded)
	}
	fallback, _ := json.Marshal(map[string]any{
		"http_status": status,
		"ok":          success,
		"truncated":   true,
		"summary":     "response detail exceeded the audit size limit",
	})
	return string(fallback)
}

func sanitize(value any, depth int) any {
	if depth >= maxDepth {
		return "[MAX_DEPTH]"
	}
	switch typed := value.(type) {
	case nil, bool, json.Number:
		return typed
	case string:
		return sanitizeString(typed)
	case []byte:
		return sanitizeString(string(typed))
	case float32, float64, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return typed
	case []any:
		limit := len(typed)
		if limit > maxArrayItems {
			limit = maxArrayItems
		}
		items := make([]any, 0, limit+1)
		for _, item := range typed[:limit] {
			items = append(items, sanitize(item, depth+1))
		}
		if len(typed) > limit {
			items = append(items, fmt.Sprintf("[TRUNCATED %d ITEMS]", len(typed)-limit))
		}
		return items
	case map[string]any:
		return sanitizeMap(typed, depth)
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return sanitizeString(fmt.Sprint(value))
	}
	var normalized any
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return sanitizeString(fmt.Sprint(value))
	}
	return sanitize(normalized, depth+1)
}

func sanitizeMap(value map[string]any, depth int) map[string]any {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make(map[string]any, len(value))
	for _, key := range keys {
		if sensitiveKey(key) {
			result[key] = "[REDACTED]"
			continue
		}
		result[key] = sanitize(value[key], depth+1)
	}
	return result
}

func sensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "_", " ", "_").Replace(strings.TrimSpace(key)))
	for _, marker := range []string{
		"password", "passwd", "secret", "token", "authorization", "cookie", "csrf",
		"credential", "private_key", "api_key", "access_key", "refresh_key",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func sanitizeString(value string) string {
	value = bearerPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = assignmentPattern.ReplaceAllString(value, `${1}${2}[REDACTED]`)
	runes := []rune(value)
	if len(runes) <= maxStringRunes {
		return value
	}
	return string(runes[:maxStringRunes]) + fmt.Sprintf("…[TRUNCATED %d RUNES]", len(runes)-maxStringRunes)
}
