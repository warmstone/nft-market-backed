package handler

import (
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"nft-market-backend/internal/domain"
	"nft-market-backend/internal/service"

	"github.com/gin-gonic/gin"
)

// OrderHandler handles REST endpoints for orders.
type OrderHandler struct {
	orderSvc    *service.OrderService
	metadataSvc *service.MetadataService
}

// NewOrderHandler creates an OrderHandler.
func NewOrderHandler(orderSvc *service.OrderService, metadataSvc *service.MetadataService) *OrderHandler {
	return &OrderHandler{orderSvc: orderSvc, metadataSvc: metadataSvc}
}

// Submit handles POST /api/v1/orders.
func (h *OrderHandler) Submit(c *gin.Context) {
	var req domain.SubmitOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{
			Error:   "INVALID_REQUEST",
			Message: err.Error(),
		})
		return
	}

	order, err := h.orderSvc.Submit(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{
			Error:   extractErrorCode(err.Error()),
			Message: err.Error(),
		})
		return
	}

	// Enqueue metadata fetch for this NFT.
	h.metadataSvc.Enqueue(order.Collection, order.TokenID.Int.String())

	c.JSON(http.StatusCreated, gin.H{
		"orderHash": order.OrderHash,
		"status":    "active",
	})
}

// List handles GET /api/v1/orders.
func (h *OrderHandler) List(c *gin.Context) {
	filter := parseOrderFilter(c)
	orders, total, err := h.orderSvc.Find(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"orders":   orders,
		"total":    total,
		"page":     (filter.Offset / filter.Limit) + 1,
		"pageSize": filter.Limit,
	})
}

// Get handles GET /api/v1/orders/:hash.
func (h *OrderHandler) Get(c *gin.Context) {
	hash := c.Param("hash")
	order, err := h.orderSvc.GetByHash(hash)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: err.Error(),
		})
		return
	}
	if order == nil {
		c.JSON(http.StatusNotFound, domain.ErrorResponse{
			Error:   "ORDER_NOT_FOUND",
			Message: "Order not found",
		})
		return
	}

	c.JSON(http.StatusOK, order)
}

// Best handles GET /api/v1/orders/best.
func (h *OrderHandler) Best(c *gin.Context) {
	collection := c.Query("collection")
	sideStr := c.Query("side")

	if collection == "" {
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{
			Error:   "MISSING_PARAM",
			Message: "collection is required",
		})
		return
	}

	side := domain.Sell
	if sideStr == "1" {
		side = domain.Buy
	}

	order, err := h.orderSvc.GetBest(collection, side)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: err.Error(),
		})
		return
	}
	if order == nil {
		c.JSON(http.StatusNotFound, domain.ErrorResponse{
			Error:   "NO_ORDERS",
			Message: "No active orders found",
		})
		return
	}

	c.JSON(http.StatusOK, order)
}

// UserOrders handles GET /api/v1/users/:address/orders.
func (h *OrderHandler) UserOrders(c *gin.Context) {
	address := c.Param("address")
	var status *domain.OrderStatus
	if s := c.Query("status"); s != "" {
		st := domain.OrderStatus(0)
		switch s {
		case "0":
			st = domain.Active
		case "1":
			st = domain.Filled
		case "2":
			st = domain.Cancelled
		case "3":
			st = domain.Expired
		default:
			c.JSON(http.StatusBadRequest, domain.ErrorResponse{
				Error:   "INVALID_STATUS",
				Message: "status must be 0-3",
			})
			return
		}
		status = &st
	}

	orders, err := h.orderSvc.GetUserOrders(address, status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"orders": orders})
}

func parseOrderFilter(c *gin.Context) domain.OrderFilter {
	filter := domain.OrderFilter{}

	if v := c.Query("collection"); v != "" {
		filter.Collection = v
	}
	if v := c.Query("maker"); v != "" {
		filter.Maker = v
	}
	if v := c.Query("paymentToken"); v != "" {
		filter.PaymentToken = v
	}
	if v := c.Query("tokenId"); v != "" {
		tokenID := new(big.Int)
		if _, ok := tokenID.SetString(v, 10); ok {
			filter.TokenID = &domain.BigInt{Int: tokenID}
		}
	}
	if v := c.Query("side"); v != "" {
		s, _ := strconv.Atoi(v)
		side := domain.OrderSide(s)
		filter.Side = &side
	}
	if v := c.Query("kind"); v != "" {
		k, _ := strconv.Atoi(v)
		kind := domain.OrderKind(k)
		filter.Kind = &kind
	}
	if v := c.Query("assetType"); v != "" {
		a, _ := strconv.Atoi(v)
		at := domain.AssetType(a)
		filter.AssetType = &at
	}
	if v := c.Query("status"); v != "" {
		s, _ := strconv.Atoi(v)
		st := domain.OrderStatus(s)
		filter.Status = &st
	}
	if v := c.Query("minPrice"); v != "" {
		filter.MinPrice = domain.NewBigInt(nil)
		filter.MinPrice.Int.SetString(v, 10)
	}
	if v := c.Query("maxPrice"); v != "" {
		filter.MaxPrice = domain.NewBigInt(nil)
		filter.MaxPrice.Int.SetString(v, 10)
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if pageSize > 50 {
		pageSize = 50
	}
	filter.Limit = pageSize
	filter.Offset = (page - 1) * pageSize

	return filter
}

func extractErrorCode(errMsg string) string {
	parts := strings.SplitN(errMsg, ": ", 2)
	code := strings.TrimSpace(parts[0])
	return strings.ToUpper(strings.ReplaceAll(code, " ", "_"))
}
