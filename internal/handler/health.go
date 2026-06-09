package handler

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/net/context"
)

type HealthHandler struct {
	db    *sql.DB
	redis *redis.Client
	rpc   RPCHealthChecker
}

type RPCHealthChecker interface {
	BlockNumber(ctx context.Context) (uint64, error)
}

func NewHealthHandler(db *sql.DB, redis *redis.Client, rpc RPCHealthChecker) *HealthHandler {
	return &HealthHandler{db: db, redis: redis, rpc: rpc}
}

func (h *HealthHandler) Health(c *gin.Context) {
	ctx := context.Background()
	healthy := true
	checks := make(map[string]string)

	if err := h.db.PingContext(ctx); err != nil {
		checks["database"] = "unhealthy: " + err.Error()
		healthy = false
	} else {
		checks["database"] = "ok"
	}

	if err := h.redis.Ping(ctx).Err(); err != nil {
		checks["redis"] = "unhealthy: " + err.Error()
		healthy = false
	} else {
		checks["redis"] = "ok"
	}

	if _, err := h.rpc.BlockNumber(ctx); err != nil {
		checks["rpc"] = "unhealthy: " + err.Error()
		healthy = false
	} else {
		checks["rpc"] = "ok"
	}

	status := http.StatusOK
	if !healthy {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{"status": status == http.StatusOK, "checks": checks})
}

func (h *HealthHandler) Ready(c *gin.Context) {
	h.Health(c)
}
