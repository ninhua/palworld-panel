package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"palpanel/internal/auditdetail"
)

func ok(c *gin.Context, data any) {
	succeed(c, http.StatusOK, data)
}

func accepted(c *gin.Context, data any) {
	succeed(c, http.StatusAccepted, data)
}

func created(c *gin.Context, data any) {
	succeed(c, http.StatusCreated, data)
}

func succeed(c *gin.Context, status int, data any) {
	c.Set(auditResponseKey, auditdetail.Encode(status, true, data))
	c.JSON(status, gin.H{"ok": true, "data": data})
}

func fail(c *gin.Context, status int, code, message string) {
	detail := gin.H{"code": code, "message": message}
	c.Set(auditResponseKey, auditdetail.Encode(status, false, detail))
	c.JSON(status, gin.H{"ok": false, "error": detail})
}
