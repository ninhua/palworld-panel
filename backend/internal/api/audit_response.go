package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"palpanel/internal/auditdetail"
	"palpanel/internal/db"
)

const (
	auditResponseKey        = "patch.audit.response-detail"
	auditSuccessKey         = "patch.audit.success"
	maxAuditCaptureBodySize = 64 * 1024
)

type auditCaptureWriter struct {
	gin.ResponseWriter
	body      bytes.Buffer
	truncated bool
}

func (w *auditCaptureWriter) Write(data []byte) (int, error) {
	w.capture(data)
	return w.ResponseWriter.Write(data)
}

func (w *auditCaptureWriter) WriteString(data string) (int, error) {
	w.capture([]byte(data))
	return w.ResponseWriter.WriteString(data)
}

func (w *auditCaptureWriter) capture(data []byte) {
	remaining := maxAuditCaptureBodySize - w.body.Len()
	if remaining <= 0 {
		w.truncated = true
		return
	}
	if len(data) > remaining {
		_, _ = w.body.Write(data[:remaining])
		w.truncated = true
		return
	}
	_, _ = w.body.Write(data)
}

// DetailedAuditMiddleware records bounded, recursively redacted API response details for write operations.
// Standard ok/fail helpers provide the original data/error object. Direct JSON responses are captured as a fallback.
func DetailedAuditMiddleware(store *db.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if requestIsSafe(c.Request.Method) {
			c.Next()
			return
		}

		capture := &auditCaptureWriter{ResponseWriter: c.Writer}
		c.Writer = capture
		c.Next()

		statusCode := c.Writer.Status()
		success := statusCode < http.StatusBadRequest
		if override, exists := c.Get(auditSuccessKey); exists {
			if value, valid := override.(bool); valid {
				success = value
			}
		}
		status := "success"
		if !success {
			status = "failed"
		}
		detail, _ := c.Get(auditResponseKey)
		message, _ := detail.(string)
		if strings.TrimSpace(message) == "" {
			message = capturedAuditResponse(statusCode, success, capture)
		}

		principal := CurrentPrincipal(c)
		_ = store.CreateAuditLog(c.Request.Context(), db.AuditLog{
			ID:      newDetailedAuditLogID(),
			Actor:   principal.Name,
			Role:    string(principal.Role),
			Action:  detailedAuditAction(c),
			Target:  detailedAuditTarget(c),
			Status:  status,
			Message: message,
			IP:      c.ClientIP(),
		})
	}
}

func setAuditSuccess(c *gin.Context, success bool) {
	c.Set(auditSuccessKey, success)
}

func capturedAuditResponse(status int, success bool, capture *auditCaptureWriter) string {
	if capture.body.Len() > 0 && !capture.truncated {
		var payload any
		if err := json.Unmarshal(capture.body.Bytes(), &payload); err == nil {
			return auditdetail.Encode(status, success, payload)
		}
	}
	return auditdetail.Encode(status, success, map[string]any{
		"captured":  capture.body.Len() > 0,
		"truncated": capture.truncated,
		"note":      "handler response was not a complete JSON document",
	})
}

func detailedAuditAction(c *gin.Context) string {
	path := strings.TrimSpace(c.FullPath())
	if path == "" {
		path = c.Request.URL.Path
	}
	return strings.TrimSpace(c.Request.Method + " " + path)
}

func detailedAuditTarget(c *gin.Context) string {
	if len(c.Params) == 0 {
		return ""
	}
	if len(c.Params) == 1 {
		return strings.TrimSpace(c.Params[0].Value)
	}
	parts := make([]string, 0, len(c.Params))
	for _, parameter := range c.Params {
		if value := strings.TrimSpace(parameter.Value); value != "" {
			parts = append(parts, parameter.Key+"="+value)
		}
	}
	return strings.Join(parts, ", ")
}

func newDetailedAuditLogID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err == nil {
		return "audit-" + hex.EncodeToString(buffer)
	}
	return fmt.Sprintf("audit-%d", time.Now().UTC().UnixNano())
}
