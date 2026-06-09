package middleware

import (
	"net/http"
	"strings"

	"nft-market-backend/internal/service"

	"github.com/gin-gonic/gin"
)

func Auth(authSvc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "UNAUTHORIZED",
				"message": "Authorization header required",
			})
			return
		}

		token := strings.TrimPrefix(header, "Bearer ")
		address, err := authSvc.ValidateToken(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "INVALID_TOKEN",
				"message": err.Error(),
			})
			return
		}

		c.Set("address", address)
		c.Next()
	}
}
