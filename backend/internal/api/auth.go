package api

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/appconfig"
	panelauth "palpanel/internal/auth"
)

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

const principalKey = "principal"

type Principal struct {
	UserID     string               `json:"-"`
	Name       string               `json:"name"`
	Role       Role                 `json:"role"`
	Credential panelauth.Credential `json:"-"`
}

type Permission string

const (
	PermRead          Permission = "read"
	PermServerControl Permission = "server:control"
	PermConfigWrite   Permission = "config:write"
	PermBackupWrite   Permission = "backup:write"
	PermModsWrite     Permission = "mods:write"
	PermPlayersWrite  Permission = "players:write"
	PermSecurityWrite Permission = "security:write"
	PermAuditRead     Permission = "audit:read"
	PermWorldReset    Permission = "world:reset"
	PermAIConfig      Permission = "ai:config"
)

var rolePermissions = map[Role]map[Permission]bool{
	RoleAdmin: {
		PermRead: true, PermServerControl: true, PermConfigWrite: true, PermBackupWrite: true,
		PermModsWrite: true, PermPlayersWrite: true, PermSecurityWrite: true, PermAuditRead: true,
		PermWorldReset: true, PermAIConfig: true,
	},
	RoleOperator: {
		PermRead: true, PermServerControl: true, PermConfigWrite: true, PermBackupWrite: true,
		PermModsWrite: true, PermPlayersWrite: true,
	},
	RoleViewer: {PermRead: true},
}

func PermissionsForRole(role Role) []Permission {
	permissions := make([]Permission, 0, len(rolePermissions[role]))
	for permission, allowed := range rolePermissions[role] {
		if allowed {
			permissions = append(permissions, permission)
		}
	}
	sort.Slice(permissions, func(i, j int) bool { return permissions[i] < permissions[j] })
	return permissions
}

func Auth(cfg appconfig.Config, service *panelauth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.RequireAuth {
			c.Set(principalKey, Principal{Name: "local", Role: RoleAdmin, Credential: panelauth.CredentialLocal})
			c.Next()
			return
		}
		principal, err := authenticateRequest(c, service)
		if err != nil {
			fail(c, http.StatusUnauthorized, "authentication_required", "login or a valid development key is required")
			c.Abort()
			return
		}
		if principal.Credential == panelauth.CredentialSession && !requestIsSafe(c.Request.Method) && !requestIsSameOrigin(c.Request) {
			fail(c, http.StatusForbidden, "cross_site_request_rejected", "state-changing session requests must be same-origin")
			c.Abort()
			return
		}
		c.Set(principalKey, principal)
		c.Next()
	}
}

func authenticateRequest(c *gin.Context, service *panelauth.Service) (Principal, error) {
	header := strings.TrimSpace(c.GetHeader("Authorization"))
	if header != "" {
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) {
			return Principal{}, sql.ErrNoRows
		}
		identity, err := service.AuthenticateAPIKey(c.Request.Context(), strings.TrimSpace(strings.TrimPrefix(header, prefix)))
		return principalFromIdentity(identity), err
	}
	cookie, err := c.Cookie(panelauth.SessionCookieName)
	if err != nil {
		return Principal{}, err
	}
	identity, err := service.AuthenticateSession(c.Request.Context(), cookie)
	return principalFromIdentity(identity), err
}

func principalFromIdentity(identity panelauth.Identity) Principal {
	return Principal{UserID: identity.UserID, Name: identity.Username, Role: Role(identity.Role), Credential: identity.Credential}
}

func Require(permission Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := CurrentPrincipal(c)
		if rolePermissions[principal.Role][permission] {
			c.Next()
			return
		}
		fail(c, http.StatusForbidden, "permission_denied", "permission denied")
		c.Abort()
	}
}

// RequireInteractiveAdmin keeps host-level diagnostics behind an authenticated
// browser session. API keys and authentication-disabled deployments cannot use
// these endpoints because they would otherwise expose a remote execution path.
func RequireInteractiveAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := CurrentPrincipal(c)
		if principal.Role == RoleAdmin && principal.Credential == panelauth.CredentialSession {
			c.Next()
			return
		}
		fail(c, http.StatusForbidden, "interactive_admin_required", "an authenticated administrator browser session is required")
		c.Abort()
	}
}

func CurrentPrincipal(c *gin.Context) Principal {
	if value, ok := c.Get(principalKey); ok {
		if principal, ok := value.(Principal); ok {
			return principal
		}
	}
	return Principal{Name: "unknown", Role: RoleViewer}
}

func SameOriginWrite() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requestIsSafe(c.Request.Method) && !requestIsSameOrigin(c.Request) {
			fail(c, http.StatusForbidden, "cross_site_request_rejected", "request must be same-origin")
			c.Abort()
			return
		}
		c.Next()
	}
}

func requestIsSafe(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func requestIsSameOrigin(request *http.Request) bool {
	source := strings.TrimSpace(request.Header.Get("Origin"))
	if source == "" {
		source = strings.TrimSpace(request.Header.Get("Referer"))
	}
	if source == "" {
		return true
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, request.Host)
}

func isAuthenticationMiss(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, http.ErrNoCookie)
}
