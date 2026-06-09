package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type bucket struct {
	tokens    float64
	lastCheck time.Time
}

const maxBuckets = 50_000

// RateLimit returns a token-bucket rate limiter keyed by client IP.
// rate is tokens per second, burst is the maximum bucket size.
func RateLimit(rate float64, burst int) gin.HandlerFunc {
	mu := sync.Mutex{}
	buckets := make(map[string]*bucket)

	// GC stale entries every 5 minutes.
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			mu.Lock()
			for ip, b := range buckets {
				if time.Since(b.lastCheck) > 5*time.Minute {
					delete(buckets, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return func(c *gin.Context) {
		ip := c.ClientIP()

		mu.Lock()
		b, ok := buckets[ip]
		if !ok {
			if len(buckets) >= maxBuckets {
				mu.Unlock()
				c.Next()
				return
			}
			b = &bucket{tokens: float64(burst), lastCheck: time.Now()}
			buckets[ip] = b
		}

		now := time.Now()
		elapsed := now.Sub(b.lastCheck).Seconds()
		b.tokens += elapsed * rate
		if b.tokens > float64(burst) {
			b.tokens = float64(burst)
		}
		b.lastCheck = now

		if b.tokens < 1 {
			mu.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":   "RATE_LIMITED",
				"message": "Too many requests",
			})
			return
		}
		b.tokens--
		mu.Unlock()

		c.Next()
	}
}
