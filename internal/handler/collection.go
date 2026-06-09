package handler

import (
	"math/big"
	"net/http"
	"strconv"

	"nft-market-backend/internal/domain"
	"nft-market-backend/internal/repository"
	"nft-market-backend/internal/service"

	"github.com/gin-gonic/gin"
)

// CollectionHandler handles REST endpoints for collections and stats.
type CollectionHandler struct {
	collectionRepo *repository.CollectionRepo
	orderRepo      *repository.OrderRepo
	metadataSvc    *service.MetadataService
}

// NewCollectionHandler creates a CollectionHandler.
func NewCollectionHandler(
	collectionRepo *repository.CollectionRepo,
	orderRepo *repository.OrderRepo,
	metadataSvc *service.MetadataService,
) *CollectionHandler {
	return &CollectionHandler{collectionRepo: collectionRepo, orderRepo: orderRepo, metadataSvc: metadataSvc}
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
		"collection":  collection,
		"floorPrice":  bigIntString(floor),
		"bestBid":     bigIntString(bestBid),
		"listed":      listed,
		"listedCount": listed,
	})
}

// Asset handles GET /api/v1/assets/:collection/:tokenId.
func (h *CollectionHandler) Asset(c *gin.Context) {
	collectionAddr := c.Param("collection")
	tokenIDStr := c.Param("tokenId")

	tokenID := new(big.Int)
	if _, ok := tokenID.SetString(tokenIDStr, 10); !ok {
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{
			Error:   "INVALID_TOKEN_ID",
			Message: "tokenId must be a decimal integer",
		})
		return
	}
	token := &domain.BigInt{Int: tokenID}

	collection, err := h.collectionRepo.FindByAddress(collectionAddr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: err.Error(),
		})
		return
	}

	metadata, err := h.collectionRepo.GetNFTMetadata(collectionAddr, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: err.Error(),
		})
		return
	}
	if metadata == nil && h.metadataSvc != nil {
		h.metadataSvc.Enqueue(collectionAddr, tokenIDStr)
	}

	sell := domain.Sell
	buy := domain.Buy
	active := domain.Active
	baseFilter := domain.OrderFilter{
		Collection: collectionAddr,
		TokenID:    token,
		Status:     &active,
		Limit:      50,
	}

	listingFilter := baseFilter
	listingFilter.Side = &sell
	listings, _, err := h.orderRepo.Find(listingFilter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: err.Error(),
		})
		return
	}

	offerFilter := baseFilter
	offerFilter.Side = &buy
	offers, _, err := h.orderRepo.Find(offerFilter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: err.Error(),
		})
		return
	}

	activityFilter := domain.OrderFilter{
		Collection: collectionAddr,
		TokenID:    token,
		Limit:      30,
	}
	activity, _, err := h.orderRepo.Find(activityFilter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, domain.AssetDetail{
		Collection: collection,
		Metadata:   metadata,
		TokenID:    token,
		Listings:   listings,
		Offers:     offers,
		Activity:   activity,
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
		"collection":  address,
		"floorPrice":  bigIntString(floor),
		"bestBid":     bigIntString(bestBid),
		"listed":      listed,
		"listedCount": listed,
	})
}

func bigIntString(v *big.Int) string {
	if v == nil {
		return ""
	}
	return v.String()
}
