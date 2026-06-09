package middleware

import (
	"time"

	logpkg "nft-market-backend/internal/log"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		fields := []zap.Field{
			zap.Int("status", status),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.Duration("latency", latency),
			zap.String("ip", c.ClientIP()),
			zap.String("request_id", c.GetString("request_id")),
		}

		if status >= 500 {
			logpkg.Logger.Error("access", fields...)
		} else if status >= 400 {
			logpkg.Logger.Warn("access", fields...)
		} else {
			logpkg.Logger.Info("access", fields...)
		}
	}
}
