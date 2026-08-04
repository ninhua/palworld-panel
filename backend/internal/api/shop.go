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
		"economy-shop-paldefender-item-delivery",
		"economy-shop-paldefender-pal-template-delivery",
		"economy-shop-delivery-retry",
		"economy-shop-delivery-reconciliation",
		"economy-shop-delivery-audit",
		"economy-shop-batch-delivery",
		"economy-shop-catalog-product-editor",
		"economy-shop-pal-template-category-filter",
		"economy-shop-account-alias-resolution",
		"economy-shop-redemption-diagnostics",
		"economy-shop-detailed-game-failure-reply",
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
	group.GET("/delivery-events", Require(PermRead), s.shopDeliveryEvents)
	group.GET("/redemption-attempts", Require(PermRead), s.shopRedemptionAttempts)
	group.POST("/maintenance/deliver", Require(PermPlayersWrite), s.deliverShopOrdersBatch)
	group.POST("/orders", Require(PermPlayersWrite), s.createShopOrder)
	group.POST("/orders/:id/deliver", Require(PermPlayersWrite), s.deliverShopOrder)
	group.POST("/orders/:id/delivery/reset", Require(PermPlayersWrite), s.resetShopOrderDelivery)
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
	items, err := service.ListOrdersFiltered(c.Request.Context(), shop.OrderFilter{
		Status: c.Query("status"), PlayerUID: c.Query("player_uid"), DeliveryState: c.Query("delivery_state"), DeliveryMode: c.Query("delivery_mode"),
		Limit: economyQueryInt(c, "limit", 100), Offset: economyQueryInt(c, "offset", 0),
	})
	if err != nil {
		shopFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func (s Server) shopDeliveryEvents(c *gin.Context) {
	service, err := s.shopService()
	if err != nil {
		shopFailure(c, err)
		return
	}
	items, err := service.ListDeliveryEvents(c.Request.Context(), shop.DeliveryEventFilter{
		OrderID: c.Query("order_id"), EventType: c.Query("event_type"),
		Limit: economyQueryInt(c, "limit", 100), Offset: economyQueryInt(c, "offset", 0),
	})
	if err != nil {
		shopFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func (s Server) shopRedemptionAttempts(c *gin.Context) {
	service, err := s.shopService()
	if err != nil {
		shopFailure(c, err)
		return
	}
	items, err := service.ListRedemptionAttempts(c.Request.Context(), shop.RedemptionAttemptFilter{
		Result: c.Query("result"), PlayerUID: c.Query("player_uid"),
		Limit: economyQueryInt(c, "limit", 200), Offset: economyQueryInt(c, "offset", 0),
	})
	if err != nil {
		shopFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func (s Server) deliverShopOrdersBatch(c *gin.Context) {
	var request shop.BatchDeliveryRequest
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
	result, err := service.DeliverBatch(c.Request.Context(), ledger, shop.NewPalDefenderDispatcher(s.defender), request, CurrentPrincipal(c).Name)
	if err != nil {
		shopFailure(c, err)
		return
	}
	ok(c, result)
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
	result, _, err := service.CreateOrderWithDiagnostics(c.Request.Context(), ledger, request, "panel", request.IdempotencyKey, CurrentPrincipal(c).Name)
	if err != nil {
		shopFailure(c, err)
		return
	}
	if result.Duplicate {
		ok(c, result)
		return
	}
	if result.Order.DeliveryMode != shop.DeliveryModeManual {
		delivered, deliveryErr := service.DeliverOrder(c.Request.Context(), ledger, shop.NewPalDefenderDispatcher(s.defender), result.Order.ID, CurrentPrincipal(c).Name)
		if deliveryErr == nil {
			created(c, delivered)
			return
		}
		if current, loadErr := service.GetOrder(c.Request.Context(), result.Order.ID); loadErr == nil {
			result.Order = current
		}
	}
	created(c, result)
}

func (s Server) deliverShopOrder(c *gin.Context) {
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
	result, err := service.DeliverOrder(c.Request.Context(), ledger, shop.NewPalDefenderDispatcher(s.defender), c.Param("id"), CurrentPrincipal(c).Name)
	if err != nil {
		shopFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) resetShopOrderDelivery(c *gin.Context) {
	service, err := s.shopService()
	if err != nil {
		shopFailure(c, err)
		return
	}
	order, err := service.ResetDelivery(c.Request.Context(), c.Param("id"), CurrentPrincipal(c).Name)
	if err != nil {
		shopFailure(c, err)
		return
	}
	ok(c, order)
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
	case errors.Is(err, shop.ErrInvalidProduct), errors.Is(err, shop.ErrInvalidQuantity), errors.Is(err, shop.ErrInvalidOrder), errors.Is(err, shop.ErrDeliveryNotAutomatic), errors.Is(err, shop.ErrInvalidBatch), errors.Is(err, shop.ErrProductAmbiguous):
		fail(c, http.StatusBadRequest, "shop_request_invalid", err.Error())
	case errors.Is(err, shop.ErrProductNotFound), errors.Is(err, shop.ErrOrderNotFound), errors.Is(err, sql.ErrNoRows):
		fail(c, http.StatusNotFound, "shop_resource_not_found", err.Error())
	case errors.Is(err, shop.ErrProductDisabled), errors.Is(err, shop.ErrInsufficientStock), errors.Is(err, shop.ErrPlayerLimit), errors.Is(err, shop.ErrOrderSettled), errors.Is(err, shop.ErrReservationConflict), errors.Is(err, shop.ErrDeliveryInProgress), errors.Is(err, shop.ErrDeliveryUncertain), errors.Is(err, shop.ErrDeliveryUnsafeCancel), errors.Is(err, shop.ErrDeliveryResetInvalid), errors.Is(err, shop.ErrDeliveryPlayerMissing), errors.Is(err, economy.ErrInsufficientBalance), errors.Is(err, economy.ErrReservationSettled), errors.Is(err, economy.ErrShopAccountIdentityAmbiguous):
		fail(c, http.StatusConflict, "shop_order_conflict", err.Error())
	case errors.Is(err, shop.ErrDeliveryFailed):
		fail(c, http.StatusBadGateway, "shop_delivery_failed", err.Error())
	default:
		fail(c, http.StatusInternalServerError, "shop_operation_failed", err.Error())
	}
}
