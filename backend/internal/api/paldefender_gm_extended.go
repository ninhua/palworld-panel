package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"

	"palpanel/internal/paldefender"
	"palpanel/internal/pallocalize"
)

func (s Server) palDefenderGMProgression(c *gin.Context) {
	response, err := s.defender.RESTProgression(c.Request.Context(), c.Param("id"))
	if err != nil {
		failPalDefenderGM(c, err)
		return
	}
	ok(c, response)
}

func (s Server) palDefenderGMGiveProgression(c *gin.Context) {
	var request paldefender.GiveProgressionRequest
	if err := bindPalDefenderGMJSON(c, &request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	s.runPalDefenderGMWrite(c, request, func(ctx context.Context) (any, error) {
		return s.defender.RESTGiveProgression(ctx, c.Param("id"), request)
	})
}

func (s Server) palDefenderGMTechs(c *gin.Context) {
	response, err := s.defender.RESTTechs(c.Request.Context(), c.Param("id"))
	if err != nil {
		failPalDefenderGM(c, err)
		return
	}
	ok(c, response)
}

func (s Server) palDefenderGMLearnTech(c *gin.Context) {
	var request paldefender.TechnologyRequest
	if err := bindPalDefenderGMJSON(c, &request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	s.runPalDefenderGMWrite(c, request, func(ctx context.Context) (any, error) {
		return s.defender.RESTLearnTechnology(ctx, c.Param("id"), request)
	})
}

func (s Server) palDefenderGMForgetTech(c *gin.Context) {
	var request paldefender.TechnologyRequest
	if err := bindPalDefenderGMJSON(c, &request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	s.runPalDefenderGMWrite(c, request, func(ctx context.Context) (any, error) {
		return s.defender.RESTForgetTechnology(ctx, c.Param("id"), request)
	})
}

func (s Server) palDefenderGMPals(c *gin.Context) {
	response, err := s.defender.RESTPals(c.Request.Context(), c.Param("id"))
	if err != nil {
		failPalDefenderGM(c, err)
		return
	}
	ok(c, response)
}

func (s Server) palDefenderGMGivePals(c *gin.Context) {
	var request paldefender.GivePalsRequest
	if err := bindPalDefenderGMJSON(c, &request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	s.runPalDefenderGMWrite(c, request, func(ctx context.Context) (any, error) {
		return s.defender.RESTGivePals(ctx, c.Param("id"), request)
	})
}

func (s Server) palDefenderGMGiveCustomPal(c *gin.Context) {
	var request paldefender.PalTemplate
	if err := bindPalDefenderGMJSON(c, &request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	s.runPalDefenderGMWrite(c, request, func(ctx context.Context) (any, error) {
		return s.defender.RESTGiveCustomPal(ctx, c.Param("id"), request)
	})
}

func (s Server) palDefenderGMGiveCustomPals(c *gin.Context) {
	var request paldefender.GiveCustomPalsRequest
	if err := bindPalDefenderGMJSON(c, &request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	s.runPalDefenderGMWrite(c, request, func(ctx context.Context) (any, error) {
		return s.defender.RESTGiveCustomPals(ctx, c.Param("id"), request.Template, request.Count)
	})
}

func (s Server) palDefenderGMReleasePal(c *gin.Context) {
	var request paldefender.ReleasePalRequest
	if err := bindPalDefenderGMJSON(c, &request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	s.runPalDefenderGMWrite(c, request, func(ctx context.Context) (any, error) {
		return s.defender.RCONReleasePal(ctx, c.Param("id"), request)
	})
}

func (s Server) palDefenderGMGivePalTemplates(c *gin.Context) {
	var request paldefender.GivePalTemplatesRequest
	if err := bindPalDefenderGMJSON(c, &request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	s.runPalDefenderGMWrite(c, request, func(ctx context.Context) (any, error) {
		return s.defender.RESTGivePalTemplates(ctx, c.Param("id"), request)
	})
}

func (s Server) palDefenderGMExportPals(c *gin.Context) {
	s.runPalDefenderGMWrite(c, gin.H{"player": c.Param("id")}, func(ctx context.Context) (any, error) {
		return s.defender.ExportPals(ctx, c.Param("id"))
	})
}

func (s Server) palDefenderGMExportedPalTemplates(c *gin.Context) {
	templates, err := s.defender.ListExportedPalTemplates(c.Param("id"))
	if err != nil {
		failPalDefenderGM(c, err)
		return
	}
	ok(c, gin.H{
		"player_id":     c.Param("id"),
		"templates":     templates,
		"reference_url": "https://github.com/Ultimeit/PalDefender/blob/main/docs/zh/Commands/index.md#exportpals",
	})
}

func (s Server) palDefenderGMExportedPalTemplate(c *gin.Context) {
	template, err := s.defender.ReadExportedPalTemplate(c.Param("id"), c.Param("name"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fail(c, http.StatusNotFound, "exported_pal_template_not_found", "Exported Pal template was not found")
			return
		}
		failPalDefenderGM(c, err)
		return
	}
	ok(c, template)
}

func (s Server) palDefenderGMCommandCatalog(c *gin.Context) {
	ok(c, gin.H{
		"commands":      paldefender.PalDefenderCommandCatalog(),
		"reference_url": "https://github.com/Ultimeit/PalDefender/blob/main/docs/zh/Commands/index.md",
	})
}

func (s Server) palDefenderGMRCONCommands(c *gin.Context) {
	response, err := s.defender.RCONCommands(c.Request.Context())
	if err != nil {
		failPalDefenderGM(c, err)
		return
	}
	ok(c, response)
}

func (s Server) palDefenderGMTechnologyCatalog(c *gin.Context) {
	response, err := s.defender.RCONTechnologyCatalog(c.Request.Context())
	if err != nil {
		failPalDefenderGM(c, err)
		return
	}
	ok(c, gin.H{"catalog": response, "reference_url": "https://paldeck.cc/technology"})
}

func (s Server) palDefenderGMLocalTechnologyCatalog(c *gin.Context) {
	limit, err := parseGMCatalogLimit(c)
	if err != nil {
		fail(c, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	items := pallocalize.SearchTechnologies(c.Query("q"), limit)
	ok(c, gin.H{"items": items, "returned": len(items)})
}

func (s Server) palDefenderGMPalCatalog(c *gin.Context) {
	limit, err := parseGMCatalogLimit(c)
	if err != nil {
		fail(c, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	items := pallocalize.SearchPalsWithOptions(c.Query("q"), limit, c.Query("advanced") == "true")
	ok(c, gin.H{"items": items, "returned": len(items)})
}

func (s Server) palDefenderGMPassiveCatalog(c *gin.Context) {
	limit, err := parseGMCatalogLimit(c)
	if err != nil {
		fail(c, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	items := pallocalize.SearchPassives(c.Query("q"), limit)
	ok(c, gin.H{"items": items, "returned": len(items)})
}

func parseGMCatalogLimit(c *gin.Context) (int, error) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limit < 1 || limit > 5000 {
		return 0, errors.New("limit must be between 1 and 5000")
	}
	return limit, nil
}

func (s Server) palDefenderGMSkinCatalog(c *gin.Context) {
	response, err := s.defender.RCONSkinCatalog(c.Request.Context())
	if err != nil {
		failPalDefenderGM(c, err)
		return
	}
	ok(c, gin.H{"catalog": response, "reference_url": "https://paldeck.cc/pals"})
}

func (s Server) palDefenderGMReferences(c *gin.Context) {
	ok(c, gin.H{
		"pals":        "https://paldeck.cc/pals",
		"pal_creator": "https://paldeck.cc/creator",
		"technology":  "https://paldeck.cc/technology",
		"passives":    "https://paldeck.cc/passives",
		"skills":      "https://paldeck.cc/skills",
		"commands":    "https://github.com/Ultimeit/PalDefender/blob/main/docs/zh/Commands/index.md",
	})
}

func (s Server) palDefenderGMListTemplates(c *gin.Context) {
	templates, err := s.defender.ListPalTemplates()
	if err != nil {
		failPalDefenderGM(c, err)
		return
	}
	ok(c, gin.H{"templates": templates, "reference_url": "https://paldeck.cc/creator"})
}

func (s Server) palDefenderGMGetTemplate(c *gin.Context) {
	template, err := s.defender.ReadPalTemplate(c.Param("name"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fail(c, http.StatusNotFound, "pal_template_not_found", "Pal template was not found")
			return
		}
		failPalDefenderGM(c, err)
		return
	}
	ok(c, template)
}

func (s Server) palDefenderGMPutTemplate(c *gin.Context) {
	var template paldefender.PalTemplate
	if err := bindPalDefenderGMJSON(c, &template); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	info, err := s.defender.WritePalTemplate(c.Param("name"), template)
	if err != nil {
		failPalDefenderGM(c, err)
		return
	}
	ok(c, gin.H{"template": info, "reload_required": false})
}

func (s Server) palDefenderGMDeleteTemplate(c *gin.Context) {
	if err := s.defender.DeletePalTemplate(c.Param("name")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fail(c, http.StatusNotFound, "pal_template_not_found", "Pal template was not found")
			return
		}
		failPalDefenderGM(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

func (s Server) palDefenderAccessSettings(c *gin.Context) {
	settings, err := s.defender.ReadAccessSettings()
	if err != nil {
		failPalDefenderGM(c, err)
		return
	}
	ok(c, settings)
}

func (s Server) palDefenderPutAccessSettings(c *gin.Context) {
	var request paldefender.AccessSettingsUpdate
	if err := bindPalDefenderGMJSON(c, &request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	settings, err := s.defender.WriteAccessSettings(request)
	if err != nil {
		failPalDefenderGM(c, err)
		return
	}
	ok(c, settings)
}

func (s Server) palDefenderWhitelist(c *gin.Context) {
	response, err := s.defender.RCONWhitelist(c.Request.Context())
	if err != nil {
		failPalDefenderGM(c, err)
		return
	}
	ok(c, response)
}

func (s Server) palDefenderWhitelistAdd(c *gin.Context) {
	s.runPalDefenderGMWrite(c, gin.H{"player": c.Param("id")}, func(ctx context.Context) (any, error) {
		return s.defender.RCONWhitelistAdd(ctx, c.Param("id"))
	})
}

func (s Server) palDefenderWhitelistRemove(c *gin.Context) {
	s.runPalDefenderGMWrite(c, gin.H{"player": c.Param("id")}, func(ctx context.Context) (any, error) {
		return s.defender.RCONWhitelistRemove(ctx, c.Param("id"))
	})
}

func (s Server) palDefenderSetAdmin(c *gin.Context) {
	s.runPalDefenderGMWrite(c, gin.H{"player": c.Param("id")}, func(ctx context.Context) (any, error) {
		return s.defender.RCONSetAdmin(ctx, c.Param("id"))
	})
}
