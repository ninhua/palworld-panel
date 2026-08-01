package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/paldefender"
	"palpanel/internal/pallocalize"
	"palpanel/internal/palrest"
	"palpanel/internal/saveindex"
)

func (s Server) cleanSaveBase(c *gin.Context) {
	active, err := s.store.ActiveSaveSource(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "save_source_read_failed", err.Error())
		return
	}
	if active.Kind != "server" {
		fail(c, http.StatusConflict, "base_cleanup_server_only", "基地清理只允许当前运行服务器存档，历史存档为只读视图")
		return
	}

	index, status, err := s.playerIndexCurrent(c, s.serverSaveIndex, true)
	if err != nil && !status.Stale {
		fail(c, http.StatusServiceUnavailable, "server_save_index_unavailable", err.Error())
		return
	}
	base, found := findBase(index.Bases, c.Param("id"))
	if !found {
		fail(c, http.StatusNotFound, "base_not_found", "base not found in the current server save")
		return
	}

	probe, err := s.defender.RCONGetNearestBase(c.Request.Context(), base.Location.X, base.Location.Y, base.Location.Z)
	if err != nil {
		fail(c, http.StatusBadGateway, "base_probe_failed", baseCleanupRCONMessage(err))
		return
	}
	if !baseProbeMatches(probe.Output, base) {
		fail(c, http.StatusConflict, "base_target_mismatch", "坐标附近的基地不属于索引中的目标公会，已停止清理")
		return
	}

	if _, err := s.defender.RCONKillNearestBase(c.Request.Context(), base.Location.X, base.Location.Y, base.Location.Z); err != nil {
		fail(c, http.StatusBadGateway, "base_cleanup_failed", baseCleanupRCONMessage(err))
		return
	}
	if _, err := s.palworldREST().Do(c.Request.Context(), http.MethodPost, "save", nil); err != nil {
		fail(c, http.StatusBadGateway, "base_save_failed", "基地已执行清理，但世界保存失败："+baseCleanupRESTMessage(err))
		return
	}
	s.invalidateServerSaveIndex()
	s.invalidateSaveCaches()
	_, rebuildStatus, rebuildErr := s.serverSaveIndex.Rebuild(c.Request.Context())
	indexRefreshed := rebuildErr == nil
	indexRefreshError := ""
	if rebuildErr != nil {
		indexRefreshError = rebuildErr.Error()
	}
	ok(c, gin.H{
		"cleaned":              true,
		"base":                 flattenBase(base),
		"saved":                true,
		"status":               status,
		"index_refreshed":      indexRefreshed,
		"index_refresh_status": rebuildStatus,
		"index_refresh_error":  indexRefreshError,
	})
}

func findBase(bases []saveindex.Base, id string) (saveindex.Base, bool) {
	needle := strings.TrimSpace(id)
	for _, base := range bases {
		if matchesID(needle, base.ID, base.Name, pallocalize.BaseName(base.Name)) {
			return base, true
		}
	}
	return saveindex.Base{}, false
}

func baseProbeMatches(output string, base saveindex.Base) bool {
	output = strings.TrimSpace(output)
	if output == "" {
		return false
	}
	lower := strings.ToLower(output)
	for _, failure := range []string{"no base", "not found", "error", "failed", "没有基地", "未找到", "失败"} {
		if strings.Contains(lower, failure) {
			return false
		}
	}
	identities := []string{base.GuildID, base.GuildName, pallocalize.GuildName(base.GuildName)}
	for _, identity := range identities {
		identity = strings.TrimSpace(identity)
		if identity != "" && strings.Contains(lower, strings.ToLower(identity)) {
			return true
		}
	}
	return base.GuildID == "" && base.GuildName == ""
}

func baseCleanupRCONMessage(err error) string {
	switch {
	case errors.Is(err, paldefender.ErrRCONDisabled):
		return "Palworld RCON 未启用"
	case errors.Is(err, paldefender.ErrRCONPasswordMissing):
		return "Palworld 管理员密码未配置"
	case errors.Is(err, paldefender.ErrRCONAuthentication):
		return "Palworld RCON 认证失败"
	case errors.Is(err, paldefender.ErrRCONUnavailable):
		return "Palworld 服务器未运行或 RCON 不可用"
	default:
		return err.Error()
	}
}

func baseCleanupRESTMessage(err error) string {
	if errors.Is(err, palrest.ErrResponseTooLarge) {
		return "Palworld REST 响应过大"
	}
	return err.Error()
}
