package api

import (
	"errors"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"

	"palpanel/internal/mods"
)

type configRestoreRequest struct {
	Revision string `json:"revision"`
}

// registerModConfigurationRoutes keeps this feature's route and permission
// matrix together. routes.go only needs to call it before the generic Mod
// enable/disable routes.
func (s Server) registerModConfigurationRoutes(api *gin.RouterGroup) {
	api.GET("/mods/configurations", s.listModConfigurations)
	api.GET("/mods/configurations/:adapter", s.getModConfiguration)
	api.PUT("/mods/configurations/:adapter", Require(PermModsWrite), s.updateModConfiguration)
	api.POST("/mods/configurations/:adapter/actions", Require(PermModsWrite), s.runModConfigurationAction)
	api.GET("/mods/configurations/:adapter/backups", s.listModConfigurationBackups)
	api.POST("/mods/configurations/:adapter/backups/:backup/restore", Require(PermModsWrite), s.restoreModConfigurationBackup)
	api.GET("/mods/:id/files", s.listModConfigFiles)
	api.GET("/mods/:id/files/:file", s.getModConfigFile)
	api.PUT("/mods/:id/files/:file", Require(PermModsWrite), s.updateModConfigFile)
	api.GET("/mods/:id/files/:file/backups", s.listModConfigFileBackups)
	api.POST("/mods/:id/files/:file/backups/:backup/restore", Require(PermModsWrite), s.restoreModConfigFileBackup)
}

func (s Server) runModConfigurationAction(c *gin.Context) {
	var request mods.ConfigurationActionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_configuration_request", err.Error())
		return
	}
	result, err := s.mods.RunConfigurationAction(c.Request.Context(), c.Param("adapter"), request)
	if err != nil {
		failModConfiguration(c, err)
		return
	}
	ok(c, result)
}

func (s Server) listModConfigurations(c *gin.Context) {
	items, err := s.mods.ListConfigurations(c.Request.Context())
	if err != nil {
		failModConfiguration(c, err)
		return
	}
	ok(c, items)
}

func (s Server) getModConfiguration(c *gin.Context) {
	document, err := s.mods.GetConfiguration(c.Request.Context(), c.Param("adapter"), c.Query("file"))
	if err != nil {
		failModConfiguration(c, err)
		return
	}
	ok(c, document)
}

func (s Server) updateModConfiguration(c *gin.Context) {
	var request mods.ConfigWriteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_configuration_request", err.Error())
		return
	}
	document, err := s.mods.WriteConfiguration(c.Request.Context(), c.Param("adapter"), c.Query("file"), request)
	if err != nil {
		failModConfiguration(c, err)
		return
	}
	ok(c, document)
}

func (s Server) listModConfigurationBackups(c *gin.Context) {
	items, err := s.mods.ListConfigurationBackups(c.Request.Context(), c.Param("adapter"), c.Query("file"))
	if err != nil {
		failModConfiguration(c, err)
		return
	}
	ok(c, items)
}

func (s Server) restoreModConfigurationBackup(c *gin.Context) {
	var request configRestoreRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_configuration_request", err.Error())
		return
	}
	document, err := s.mods.RestoreConfigurationBackup(c.Request.Context(), c.Param("adapter"), c.Query("file"), c.Param("backup"), request.Revision)
	if err != nil {
		failModConfiguration(c, err)
		return
	}
	ok(c, document)
}

func (s Server) listModConfigFiles(c *gin.Context) {
	items, err := s.mods.ListModConfigFiles(c.Request.Context(), c.Param("id"))
	if err != nil {
		failModConfiguration(c, err)
		return
	}
	ok(c, items)
}

func (s Server) getModConfigFile(c *gin.Context) {
	document, err := s.mods.GetModConfigFile(c.Request.Context(), c.Param("id"), c.Param("file"))
	if err != nil {
		failModConfiguration(c, err)
		return
	}
	ok(c, document)
}

func (s Server) updateModConfigFile(c *gin.Context) {
	var request mods.ConfigWriteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_configuration_request", err.Error())
		return
	}
	document, err := s.mods.WriteModConfigFile(c.Request.Context(), c.Param("id"), c.Param("file"), request)
	if err != nil {
		failModConfiguration(c, err)
		return
	}
	ok(c, document)
}

func (s Server) listModConfigFileBackups(c *gin.Context) {
	items, err := s.mods.ListModConfigBackups(c.Request.Context(), c.Param("id"), c.Param("file"))
	if err != nil {
		failModConfiguration(c, err)
		return
	}
	ok(c, items)
}

func (s Server) restoreModConfigFileBackup(c *gin.Context) {
	var request configRestoreRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_configuration_request", err.Error())
		return
	}
	document, err := s.mods.RestoreModConfigBackup(c.Request.Context(), c.Param("id"), c.Param("file"), c.Param("backup"), request.Revision)
	if err != nil {
		failModConfiguration(c, err)
		return
	}
	ok(c, document)
}

func failModConfiguration(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	code := "mod_configuration_failed"
	var configErr mods.ConfigurationError
	if errors.As(err, &configErr) {
		code = configErr.Code
		switch configErr.Code {
		case "configuration_adapter_not_found", "configuration_file_not_found", "configuration_root_not_found", "configuration_backup_not_found", "mod_not_found", "mod_has_no_config_root":
			status = http.StatusNotFound
		case "configuration_revision_conflict", "configuration_already_initialized":
			status = http.StatusConflict
		case "configuration_file_too_large":
			status = http.StatusRequestEntityTooLarge
		case "configuration_file_forbidden", "configuration_not_utf8", "configuration_parse_failed", "configuration_validation_failed", "configuration_action_not_supported", "executable_confirmation_required", "invalid_configuration_request":
			status = http.StatusBadRequest
		case "configuration_unavailable", "mod_dependency_missing":
			status = http.StatusUnprocessableEntity
		case "unsafe_configuration_path":
			status = http.StatusForbidden
		}
	} else if errors.Is(err, fs.ErrNotExist) {
		status = http.StatusNotFound
	}
	fail(c, status, code, err.Error())
}
