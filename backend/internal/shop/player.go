package shop

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"palpanel/internal/economy"
)

var ErrProductAmbiguous = errors.New("shop product selector is ambiguous")

const (
	playerCatalogPageSize = 5
	playerOrdersPageSize  = 5
)

type PlayerCatalogItem struct {
	Code           string `json:"code"`
	ProductID      string `json:"product_id"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	Price          int64  `json:"price"`
	Stock          int64  `json:"stock"`
	PerPlayerLimit int64  `json:"per_player_limit"`
	Purchased      int64  `json:"purchased"`
	RemainingLimit int64  `json:"remaining_limit"`
	DeliveryMode   string `json:"delivery_mode"`
	Available      bool   `json:"available"`
}

type PlayerCatalogResult struct {
	PlayerUID string              `json:"player_uid"`
	Balance   int64               `json:"balance"`
	Query     string              `json:"query,omitempty"`
	Items     []PlayerCatalogItem `json:"items"`
	Count     int                 `json:"count"`
	Total     int                 `json:"total"`
	Limit     int                 `json:"limit"`
	Offset    int                 `json:"offset"`
}

type PlayerCommandRequest struct {
	EventID          string `json:"event_id"`
	PlayerUID        string `json:"player_uid"`
	Nickname         string `json:"nickname,omitempty"`
	SteamID          string `json:"steam_id,omitempty"`
	Message          string `json:"message"`
	CommandPrefix    string `json:"command_prefix,omitempty"`
	AllowBareCommand bool   `json:"allow_bare_command"`
}

type PlayerCommandResult struct {
	EventID       string `json:"event_id,omitempty"`
	PlayerUID     string `json:"player_uid,omitempty"`
	Handled       bool   `json:"handled"`
	Duplicate     bool   `json:"duplicate"`
	Command       string `json:"command,omitempty"`
	Reply         string `json:"reply,omitempty"`
	ProductCode   string `json:"product_code,omitempty"`
	OrderID       string `json:"order_id,omitempty"`
	OrderStatus   string `json:"order_status,omitempty"`
	DeliveryState string `json:"delivery_state,omitempty"`
	Balance       int64  `json:"balance,omitempty"`
	Order         *Order `json:"order,omitempty"`
}

func PublicProductCode(id string) string {
	value := strings.TrimSpace(id)
	if separator := strings.LastIndex(value, "_"); separator >= 0 && separator < len(value)-1 {
		value = value[separator+1:]
	}
	if len(value) > 8 {
		value = value[:8]
	}
	return strings.ToUpper(value)
}

func (s *Service) PlayerCatalog(ctx context.Context, ledger Economy, playerUID, query string, limit, offset int) (PlayerCatalogResult, error) {
	playerUID = normalizePlayerUID(playerUID)
	if playerUID == "" {
		return PlayerCatalogResult{}, economy.ErrInvalidPlayerUID
	}
	limit, offset = normalizePlayerPage(limit, offset, playerCatalogPageSize)
	query = strings.TrimSpace(query)
	account, err := ledger.GetAccount(ctx, playerUID)
	if err != nil {
		return PlayerCatalogResult{}, err
	}

	where := ` WHERE p.enabled=1`
	args := []any{}
	if query != "" {
		pattern := "%" + query + "%"
		where += ` AND (p.name LIKE ? COLLATE NOCASE OR p.description LIKE ? COLLATE NOCASE OR p.id LIKE ? COLLATE NOCASE)`
		args = append(args, pattern, pattern, pattern)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shop_products p`+where, args...).Scan(&total); err != nil {
		return PlayerCatalogResult{}, err
	}
	selectArgs := append([]any{playerUID}, args...)
	selectArgs = append(selectArgs, limit, offset)
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,p.name,p.description,p.price,p.stock,p.per_player_limit,p.delivery_mode,
		COALESCE((SELECT SUM(o.quantity) FROM shop_orders o WHERE o.product_id=p.id AND o.player_uid=? AND o.status IN ('pending','delivered')),0)
		FROM shop_products p`+where+` ORDER BY p.updated_at DESC,p.id LIMIT ? OFFSET ?`, selectArgs...)
	if err != nil {
		return PlayerCatalogResult{}, err
	}
	defer rows.Close()
	items := make([]PlayerCatalogItem, 0, limit)
	for rows.Next() {
		var item PlayerCatalogItem
		if err := rows.Scan(&item.ProductID, &item.Name, &item.Description, &item.Price, &item.Stock, &item.PerPlayerLimit, &item.DeliveryMode, &item.Purchased); err != nil {
			return PlayerCatalogResult{}, err
		}
		item.Code = PublicProductCode(item.ProductID)
		item.RemainingLimit = -1
		if item.PerPlayerLimit > 0 {
			item.RemainingLimit = max(item.PerPlayerLimit-item.Purchased, 0)
		}
		item.Available = item.Stock != 0 && item.RemainingLimit != 0
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return PlayerCatalogResult{}, err
	}
	return PlayerCatalogResult{PlayerUID: playerUID, Balance: account.Balance, Query: query, Items: items, Count: len(items), Total: total, Limit: limit, Offset: offset}, nil
}

func (s *Service) ResolvePublicProduct(ctx context.Context, selector string) (Product, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return Product{}, ErrProductNotFound
	}
	if product, err := s.GetProduct(ctx, selector); err == nil {
		if !product.Enabled {
			return Product{}, ErrProductDisabled
		}
		return product, nil
	} else if !errors.Is(err, ErrProductNotFound) && !errors.Is(err, sql.ErrNoRows) {
		return Product{}, err
	}
	items, err := s.ListProducts(ctx, false, maximumLimit, 0)
	if err != nil {
		return Product{}, err
	}
	matches := make([]Product, 0, 2)
	for _, product := range items {
		if strings.EqualFold(PublicProductCode(product.ID), selector) || strings.EqualFold(product.Name, selector) {
			matches = append(matches, product)
		}
	}
	if len(matches) == 0 {
		return Product{}, ErrProductNotFound
	}
	if len(matches) > 1 {
		return Product{}, ErrProductAmbiguous
	}
	return matches[0], nil
}

func (s *Service) ExecutePlayerCommand(ctx context.Context, ledger Economy, dispatcher Dispatcher, request PlayerCommandRequest, actor string) (PlayerCommandResult, error) {
	request.EventID = strings.TrimSpace(request.EventID)
	request.PlayerUID = normalizePlayerUID(request.PlayerUID)
	request.Message = strings.TrimSpace(request.Message)
	result := PlayerCommandResult{EventID: request.EventID, PlayerUID: request.PlayerUID}
	command, arguments, handled := parsePlayerShopCommand(request.Message, request.CommandPrefix, request.AllowBareCommand)
	if !handled {
		return result, nil
	}
	result.Handled = true
	result.Command = command
	if request.PlayerUID == "" {
		result.Reply = "无法识别玩家身份，暂时不能使用积分商城。"
		return result, nil
	}

	switch command {
	case "catalog":
		page, search := parseCatalogArguments(arguments)
		catalog, err := s.PlayerCatalog(ctx, ledger, request.PlayerUID, search, playerCatalogPageSize, (page-1)*playerCatalogPageSize)
		if err != nil {
			return PlayerCommandResult{}, err
		}
		result.Balance = catalog.Balance
		result.Reply = formatPlayerCatalog(catalog, page)
		return result, nil
	case "orders":
		page := parsePositivePage(arguments)
		orders, err := s.ListOrdersFiltered(ctx, OrderFilter{PlayerUID: request.PlayerUID, Limit: playerOrdersPageSize, Offset: (page - 1) * playerOrdersPageSize})
		if err != nil {
			return PlayerCommandResult{}, err
		}
		account, err := ledger.GetAccount(ctx, request.PlayerUID)
		if err != nil {
			return PlayerCommandResult{}, err
		}
		result.Balance = account.Balance
		result.Reply = formatPlayerOrders(orders, page, account.Balance)
		return result, nil
	case "redeem":
		selector, quantity, valid := parseRedeemArguments(arguments)
		if !valid {
			result.Reply = "用法：兑换 <商品兑换码或完整名称> [数量]。"
			return result, nil
		}
		product, err := s.ResolvePublicProduct(ctx, selector)
		if err != nil {
			if message, ok := playerPurchaseError(err); ok {
				result.Reply = message
				return result, nil
			}
			return PlayerCommandResult{}, err
		}
		result.ProductCode = PublicProductCode(product.ID)
		idempotencyKey := request.EventID
		if idempotencyKey == "" {
			idempotencyKey = newID("chat", request.PlayerUID+product.ID)
		}
		orderResult, err := s.CreateOrder(ctx, ledger, CreateOrderRequest{
			IdempotencyKey: playerOrderIdempotencyKey(idempotencyKey),
			ProductID:      product.ID,
			PlayerUID:      request.PlayerUID,
			Nickname:       request.Nickname,
			SteamID:        request.SteamID,
			Quantity:       quantity,
		}, actor)
		if err != nil {
			if message, ok := playerPurchaseError(err); ok {
				result.Reply = message
				return result, nil
			}
			return PlayerCommandResult{}, err
		}
		result.Duplicate = orderResult.Duplicate
		result.Balance = orderResult.Account.Balance
		order := orderResult.Order
		if order.DeliveryMode != DeliveryModeManual && order.Status == "pending" {
			delivered, deliveryErr := s.DeliverOrder(ctx, ledger, dispatcher, order.ID, actor)
			if deliveryErr == nil {
				orderResult = delivered
				order = delivered.Order
				result.Balance = delivered.Account.Balance
			} else if current, loadErr := s.GetOrder(ctx, order.ID); loadErr == nil {
				order = current
			}
		}
		result.OrderID = order.ID
		result.OrderStatus = order.Status
		result.DeliveryState = order.DeliveryState
		result.Order = &order
		result.Reply = formatPurchaseReply(order, result.Balance, result.ProductCode)
		return result, nil
	default:
		return result, nil
	}
}

func playerOrderIdempotencyKey(eventID string) string {
	value := "game-shop:" + strings.TrimSpace(eventID)
	if len(value) <= 128 {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return "game-shop:" + hex.EncodeToString(sum[:16])
}

func parsePlayerShopCommand(message, prefix string, allowBare bool) (string, string, bool) {
	message = strings.TrimSpace(message)
	prefix = strings.TrimSpace(prefix)
	if message == "" {
		return "", "", false
	}
	if prefix != "" && strings.HasPrefix(message, prefix) {
		message = strings.TrimSpace(strings.TrimPrefix(message, prefix))
	} else if prefix != "" && !allowBare {
		return "", "", false
	}
	aliases := []struct {
		command string
		labels  []string
	}{
		{command: "orders", labels: []string{"我的订单", "订单"}},
		{command: "catalog", labels: []string{"商城", "商店"}},
		{command: "redeem", labels: []string{"兑换", "购买"}},
	}
	for _, item := range aliases {
		for _, label := range item.labels {
			if message == label {
				return item.command, "", true
			}
			if strings.HasPrefix(message, label) {
				remainder := strings.TrimPrefix(message, label)
				if remainder != "" && strings.TrimSpace(remainder) != remainder {
					return item.command, strings.TrimSpace(remainder), true
				}
			}
		}
	}
	return "", "", false
}

func parseCatalogArguments(arguments string) (int, string) {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		return 1, ""
	}
	if page, err := strconv.Atoi(arguments); err == nil && page > 0 {
		return page, ""
	}
	return 1, arguments
}

func parsePositivePage(arguments string) int {
	page, err := strconv.Atoi(strings.TrimSpace(arguments))
	if err != nil || page < 1 {
		return 1
	}
	return page
}

func parseRedeemArguments(arguments string) (string, int64, bool) {
	fields := strings.Fields(strings.TrimSpace(arguments))
	if len(fields) == 0 {
		return "", 0, false
	}
	quantity := int64(1)
	selectorFields := fields
	if len(fields) > 1 {
		if parsed, err := strconv.ParseInt(fields[len(fields)-1], 10, 64); err == nil {
			quantity = parsed
			selectorFields = fields[:len(fields)-1]
		}
	}
	selector := strings.TrimSpace(strings.Join(selectorFields, " "))
	return selector, quantity, selector != "" && quantity >= 1 && quantity <= 1000
}

func formatPlayerCatalog(catalog PlayerCatalogResult, page int) string {
	if len(catalog.Items) == 0 {
		if catalog.Query != "" {
			return fmt.Sprintf("【积分商城】未找到与“%s”匹配的商品。当前积分：%d。", catalog.Query, catalog.Balance)
		}
		return fmt.Sprintf("【积分商城】当前没有可展示的商品。当前积分：%d。", catalog.Balance)
	}
	pages := max((catalog.Total+catalog.Limit-1)/catalog.Limit, 1)
	lines := []string{fmt.Sprintf("【积分商城】第%d/%d页 · 当前积分：%d", page, pages, catalog.Balance)}
	for _, item := range catalog.Items {
		availability := "可兑换"
		if !item.Available {
			availability = "已售罄或已达限购"
		}
		stock := "库存不限"
		if item.Stock >= 0 {
			stock = fmt.Sprintf("库存%d", item.Stock)
		}
		limit := "不限购"
		if item.RemainingLimit >= 0 {
			limit = fmt.Sprintf("还可购%d", item.RemainingLimit)
		}
		lines = append(lines, fmt.Sprintf("[%s] %s · %d积分 · %s/%s · %s", item.Code, item.Name, item.Price, stock, limit, availability))
	}
	lines = append(lines, "兑换：兑换 <兑换码> [数量]；搜索：商城 <关键词>；翻页：商城 <页码>。")
	return strings.Join(lines, "\n")
}

func formatPlayerOrders(orders []Order, page int, balance int64) string {
	if len(orders) == 0 {
		return fmt.Sprintf("【我的订单】第%d页暂无订单。当前积分：%d。", page, balance)
	}
	lines := []string{fmt.Sprintf("【我的订单】第%d页 · 当前积分：%d", page, balance)}
	for _, order := range orders {
		lines = append(lines, fmt.Sprintf("[%s] %s ×%d · %d积分 · %s/%s", PublicProductCode(order.ProductID), order.ProductName, order.Quantity, order.TotalPoints, playerOrderStatusLabel(order.Status), playerDeliveryStateLabel(order.DeliveryState)))
	}
	lines = append(lines, "翻页：我的订单 <页码>。")
	return strings.Join(lines, "\n")
}

func formatPurchaseReply(order Order, balance int64, code string) string {
	prefix := fmt.Sprintf("商品[%s] %s ×%d，订单 %s。", code, order.ProductName, order.Quantity, order.ID)
	switch {
	case order.Status == "delivered":
		return fmt.Sprintf("兑换成功：%s 已完成交付。当前积分：%d。", prefix, balance)
	case order.DeliveryMode == DeliveryModeManual:
		return fmt.Sprintf("订单已创建：%s 等待管理员人工发放。已预扣%d积分，当前积分：%d。", prefix, order.TotalPoints, balance)
	case order.DeliveryState == DeliveryStateFailed:
		return fmt.Sprintf("订单已创建，但自动交付失败：%s 积分保持预扣，管理员可重试或退款。当前积分：%d。", prefix, balance)
	case order.DeliveryState == DeliveryStateProcessing:
		return fmt.Sprintf("订单交付结果待核对：%s 请勿重复兑换，管理员确认后处理。当前积分：%d。", prefix, balance)
	case order.DeliveryState == DeliveryStateSucceeded:
		return fmt.Sprintf("商品已发放，积分正在结算：%s 请稍后查询订单。当前积分：%d。", prefix, balance)
	default:
		return fmt.Sprintf("订单已创建：%s 正在等待自动交付。当前积分：%d。", prefix, balance)
	}
}

func playerPurchaseError(err error) (string, bool) {
	switch {
	case errors.Is(err, ErrProductNotFound):
		return "未找到该商品，请先发送“商城”查看兑换码。", true
	case errors.Is(err, ErrProductAmbiguous):
		return "商品名称不唯一，请使用商城中显示的兑换码。", true
	case errors.Is(err, ErrProductDisabled):
		return "该商品已经下架。", true
	case errors.Is(err, ErrInsufficientStock):
		return "该商品库存不足。", true
	case errors.Is(err, ErrPlayerLimit):
		return "已达到该商品的个人限购数量。", true
	case errors.Is(err, ErrInvalidQuantity), errors.Is(err, ErrInvalidOrder), errors.Is(err, ErrInvalidProduct):
		return "兑换数量或商品配置无效。", true
	case errors.Is(err, economy.ErrInsufficientBalance):
		return "积分不足，无法兑换该商品。", true
	case errors.Is(err, ErrDeliveryInProgress), errors.Is(err, ErrDeliveryUncertain):
		return "已有订单正在核对交付结果，请勿重复兑换。", true
	default:
		return "", false
	}
}

func playerOrderStatusLabel(status string) string {
	switch status {
	case "pending":
		return "待处理"
	case "delivered":
		return "已完成"
	case "cancelled":
		return "已取消"
	default:
		return status
	}
}

func playerDeliveryStateLabel(state string) string {
	switch state {
	case DeliveryStateManual:
		return "人工交付"
	case DeliveryStatePending:
		return "等待交付"
	case DeliveryStateProcessing:
		return "核对中"
	case DeliveryStateFailed:
		return "交付失败"
	case DeliveryStateSucceeded:
		return "交付成功"
	default:
		return state
	}
}

func normalizePlayerPage(limit, offset, fallback int) (int, int) {
	if limit <= 0 {
		limit = fallback
	}
	if limit > 20 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
