package handler

import (
	"net/http"
	"strconv"

	"nft-market-backend/internal/domain"
	"nft-market-backend/internal/repository"

	"github.com/gin-gonic/gin"
)

// CollectionHandler handles REST endpoints for collections and stats.
type CollectionHandler struct {
	collectionRepo *repository.CollectionRepo
	orderRepo      *repository.OrderRepo
}

// NewCollectionHandler creates a CollectionHandler.
func NewCollectionHandler(collectionRepo *repository.CollectionRepo, orderRepo *repository.OrderRepo) *CollectionHandler {
	return &CollectionHandler{collectionRepo: collectionRepo, orderRepo: orderRepo}
}

// List handles GET /api/v1/collections.
func (h *CollectionHandler) List(c *gin.Context) {
	search := c.Query("search")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))

	collections, total, err := h.collectionRepo.FindAll(search, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"collections": collections,
		"total":       total,
		"page":        page,
		"pageSize":    pageSize,
	})
}

// Get handles GET /api/v1/collections/:address.
func (h *CollectionHandler) Get(c *gin.Context) {
	address := c.Param("address")

	collection, err := h.collectionRepo.FindByAddress(address)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: err.Error(),
		})
		return
	}
	if collection == nil {
		c.JSON(http.StatusNotFound, domain.ErrorResponse{
			Error:   "COLLECTION_NOT_FOUND",
			Message: "Collection not found",
		})
		return
	}

	// Enrich with market data.
	floor, _ := h.orderRepo.GetBestPrice(address, domain.Sell)
	bestBid, _ := h.orderRepo.GetBestPrice(address, domain.Buy)
	listed, _ := h.orderRepo.GetListedCount(address)

	c.JSON(http.StatusOK, gin.H{
		"collection": collection,
		"floorPrice": floor,
		"bestBid":    bestBid,
		"listed":     listed,
	})
}

// GlobalStats handles GET /api/v1/stats.
func (h *CollectionHandler) GlobalStats(c *gin.Context) {
	totalOrders, _ := h.collectionRepo.GetTotalOrders()
	totalCollections, _ := h.collectionRepo.GetCollectionCount()
	totalTraders, _ := h.orderRepo.GetActiveMakerCount()

	c.JSON(http.StatusOK, gin.H{
		"totalOrders":      totalOrders,
		"totalCollections": totalCollections,
		"totalTraders":     totalTraders,
	})
}

// CollectionStats handles GET /api/v1/stats/:collection.
func (h *CollectionHandler) CollectionStats(c *gin.Context) {
	address := c.Param("collection")

	floor, _ := h.orderRepo.GetBestPrice(address, domain.Sell)
	bestBid, _ := h.orderRepo.GetBestPrice(address, domain.Buy)
	listed, _ := h.orderRepo.GetListedCount(address)

	c.JSON(http.StatusOK, gin.H{
		"collection": address,
		"floorPrice": floor,
		"bestBid":    bestBid,
		"listed":     listed,
	})
}
