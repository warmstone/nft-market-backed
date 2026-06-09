package handler

import (
	"net/http"
	"strings"

	"nft-market-backend/internal/ws"

	"github.com/gin-gonic/gin"
)

// WSHandler handles WebSocket upgrade requests.
type WSHandler struct {
	hub *ws.Hub
}

// NewWSHandler creates a WSHandler.
func NewWSHandler(hub *ws.Hub) *WSHandler {
	return &WSHandler{hub: hub}
}

// Handle upgrades GET /ws/orders to a WebSocket connection.
func (h *WSHandler) Handle(c *gin.Context) {
	collectionsStr := c.Query("collections")
	var collections []string
	if collectionsStr != "" {
		for _, col := range strings.Split(collectionsStr, ",") {
			col = strings.TrimSpace(col)
			if col != "" {
				collections = append(collections, col)
			}
		}
	}

	if err := h.hub.Upgrade(c.Writer, c.Request, collections); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "WS_UPGRADE_FAILED",
			"message": err.Error(),
		})
	}
}
