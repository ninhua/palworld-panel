package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/shop"
)

type gameShopCatalogRequest struct {
	PlayerUID string `json:"player_uid"`
	Nickname  string `json:"nickname,omitempty"`
	SteamID   string `json:"steam_id,omitempty"`
	Query     string `json:"query,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

type gameShopOrderRequest struct {
	PlayerUID      string `json:"player_uid"`
	Nickname       string `json:"nickname,omitempty"`
	SteamID        string `json:"steam_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
	Product        string `json:"product"`
	Quantity       int64  `json:"quantity,omitempty"`
}

type gameShopOrdersRequest struct {
	PlayerUID string `json:"player_uid"`
	Nickname  string `json:"nickname,omitempty"`
	SteamID   string `json:"steam_id,omitempty"`
	Status    string `json:"status,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

func (s Server) gameShopCatalog(c *gin.Context) {
	var request gameShopCatalogRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	points, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	account, err := points.EnsureAccount(c.Request.Context(), request.PlayerUID, request.Nickname, request.SteamID)
	if err != nil {
		economyFailure(c, err)
		return
	}
	service, err := s.shopService()
	if err != nil {
		shopFailure(c, err)
		return
	}
	catalog, err := service.PlayerCatalog(c.Request.Context(), points, account.PlayerUID, request.Query, request.Limit, request.Offset)
	if err != nil {
		shopFailure(c, err)
		return
	}
	ok(c, catalog)
}

func (s Server) gameShopCreateOrder(c *gin.Context) {
	var request gameShopOrderRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.Product = strings.TrimSpace(request.Product)
	if request.IdempotencyKey == "" || len(request.IdempotencyKey) > 128 || request.Product == "" {
		fail(c, http.StatusBadRequest, "shop_request_invalid", "idempotency_key and product are required")
		return
	}
	if request.Quantity == 0 {
		request.Quantity = 1
	}
	points, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	account, err := points.EnsureAccount(c.Request.Context(), request.PlayerUID, request.Nickname, request.SteamID)
	if err != nil {
		economyFailure(c, err)
		return
	}
	service, err := s.shopService()
	if err != nil {
		shopFailure(c, err)
		return
	}
	command, err := service.ExecutePlayerCommand(c.Request.Context(), points, shop.NewPalDefenderDispatcher(s.defender), shop.PlayerCommandRequest{
		EventID: request.IdempotencyKey, PlayerUID: account.PlayerUID, Nickname: request.Nickname, SteamID: request.SteamID,
		Message: fmt.Sprintf("兑换 %s %d", request.Product, request.Quantity), AllowBareCommand: true,
	}, "game-integration:"+account.PlayerUID)
	if err != nil {
		shopFailure(c, err)
		return
	}
	if !command.Handled || command.Order == nil {
		fail(c, http.StatusBadRequest, "shop_request_invalid", command.Reply)
		return
	}
	if command.OrderStatus == "delivered" {
		created(c, command)
		return
	}
	ok(c, command)
}

func (s Server) gameShopOrders(c *gin.Context) {
	var request gameShopOrdersRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	points, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	account, err := points.EnsureAccount(c.Request.Context(), request.PlayerUID, request.Nickname, request.SteamID)
	if err != nil {
		economyFailure(c, err)
		return
	}
	service, err := s.shopService()
	if err != nil {
		shopFailure(c, err)
		return
	}
	limit := request.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if request.Offset < 0 {
		request.Offset = 0
	}
	items, err := service.ListOrdersFiltered(c.Request.Context(), shop.OrderFilter{
		Status: request.Status, PlayerUID: account.PlayerUID, Limit: limit, Offset: request.Offset,
	})
	if err != nil {
		shopFailure(c, err)
		return
	}
	ok(c, gin.H{"player_uid": account.PlayerUID, "balance": account.Balance, "items": items, "count": len(items), "limit": limit, "offset": request.Offset})
}
