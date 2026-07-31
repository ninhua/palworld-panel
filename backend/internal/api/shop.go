package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/economy"
	"palpanel/internal/shop"
)

func init() {
	patchFeatures = append(patchFeatures,
		"economy-shop-products",
		"economy-shop-orders",
		"economy-shop-idempotent-redemption",
		"economy-shop-manual-fulfillment",
	)
}

func (s Server) registerShopRoutes(api *gin.RouterGroup) {
	group := api.Group("/shop")
	group.GET("/summary", Require(PermRead), s.shopSummary)
	group.GET("/products", Require(PermRead), s.shopProducts)
	group.POST("/products", Require(PermConfigWrite), s.createShopProduct)
	group.PUT("/products/:id", Require(PermConfigWrite), s.updateShopProduct)
	group.DELETE("/products/:id", Require(PermConfigWrite), s.archiveShopProduct)
	group.GET("/orders", Require(PermRead), s.shopOrders)
	group.POST("/orders", Require(PermPlayersWrite), s.createShopOrder)
	group.POST("/orders/:id/complete", Require(PermPlayersWrite), s.completeShopOrder)
	group.POST("/orders/:id/cancel", Require(PermPlayersWrite), s.cancelShopOrder)
}

func (s Server) shopService() (*shop.Service, error) { return shop.ForPath(s.cfg.DBPath) }

func (s Server) shopSummary(c *gin.Context) {
	service, err := s.shopService()
	if err != nil {
		shopFailure(c, err)
		return
	}
	result, err := service.Summary(c.Request.Context())
	if err != nil {
		shopFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) shopProducts(c *gin.Context) {
	service, err := s.shopService()
	if err != nil {
		shopFailure(c, err)
		return
	}
	items, err := service.ListProducts(c.Request.Context(), shopQueryBool(c, "include_disabled"), economyQueryInt(c, "limit", 100), economyQueryInt(c, "offset", 0))
	if err != nil {
		shopFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func (s Server) createShopProduct(c *gin.Context) {
	var input shop.ProductInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.shopService()
	if err != nil {
		shopFailure(c, err)
		return
	}
	product, err := service.CreateProduct(c.Request.Context(), input)
	if err != nil {
		shopFailure(c, err)
		return
	}
	created(c, product)
}

func (s Server) updateShopProduct(c *gin.Context) {
	var input shop.ProductInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.shopService()
	if err != nil {
		shopFailure(c, err)
		return
	}
	product, err := service.UpdateProduct(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		shopFailure(c, err)
		return
	}
	ok(c, product)
}

func (s Server) archiveShopProduct(c *gin.Context) {
	service, err := s.shopService()
	if err != nil {
		shopFailure(c, err)
		return
	}
	product, err := service.ArchiveProduct(c.Request.Context(), c.Param("id"))
	if err != nil {
		shopFailure(c, err)
		return
	}
	ok(c, product)
}

func (s Server) shopOrders(c *gin.Context) {
	service, err := s.shopService()
	if err != nil {
		shopFailure(c, err)
		return
	}
	items, err := service.ListOrders(c.Request.Context(), c.Query("status"), c.Query("player_uid"), economyQueryInt(c, "limit", 100), economyQueryInt(c, "offset", 0))
	if err != nil {
		shopFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func (s Server) createShopOrder(c *gin.Context) {
	var request shop.CreateOrderRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.shopService()
	if err != nil {
		shopFailure(c, err)
		return
	}
	ledger, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	result, err := service.CreateOrder(c.Request.Context(), ledger, request, CurrentPrincipal(c).Name)
	if err != nil {
		shopFailure(c, err)
		return
	}
	if result.Duplicate {
		ok(c, result)
		return
	}
	created(c, result)
}

func (s Server) completeShopOrder(c *gin.Context) {
	service, err := s.shopService()
	if err != nil {
		shopFailure(c, err)
		return
	}
	ledger, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	result, err := service.CompleteOrder(c.Request.Context(), ledger, c.Param("id"), CurrentPrincipal(c).Name)
	if err != nil {
		shopFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) cancelShopOrder(c *gin.Context) {
	service, err := s.shopService()
	if err != nil {
		shopFailure(c, err)
		return
	}
	ledger, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	result, err := service.CancelOrder(c.Request.Context(), ledger, c.Param("id"), CurrentPrincipal(c).Name)
	if err != nil {
		shopFailure(c, err)
		return
	}
	ok(c, result)
}

func shopQueryBool(c *gin.Context, key string) bool {
	value, err := strconv.ParseBool(strings.TrimSpace(c.Query(key)))
	return err == nil && value
}

func shopFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, shop.ErrInvalidProduct), errors.Is(err, shop.ErrInvalidQuantity), errors.Is(err, shop.ErrInvalidOrder):
		fail(c, http.StatusBadRequest, "shop_request_invalid", err.Error())
	case errors.Is(err, shop.ErrProductNotFound), errors.Is(err, shop.ErrOrderNotFound), errors.Is(err, sql.ErrNoRows):
		fail(c, http.StatusNotFound, "shop_resource_not_found", err.Error())
	case errors.Is(err, shop.ErrProductDisabled), errors.Is(err, shop.ErrInsufficientStock), errors.Is(err, shop.ErrPlayerLimit), errors.Is(err, shop.ErrOrderSettled), errors.Is(err, shop.ErrReservationConflict), errors.Is(err, economy.ErrInsufficientBalance), errors.Is(err, economy.ErrReservationSettled):
		fail(c, http.StatusConflict, "shop_order_conflict", err.Error())
	default:
		fail(c, http.StatusInternalServerError, "shop_operation_failed", err.Error())
	}
}
