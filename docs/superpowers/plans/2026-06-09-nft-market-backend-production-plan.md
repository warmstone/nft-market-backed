# NFT Market Backend Production Evolution — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Evolve the nft-market-backend from MVP to production-grade across four phases: infrastructure → security → features → quality.

**Architecture:** Each phase injects new middleware, handlers, or service packages into the existing layered architecture (domain → repository → service → handler → Gin router). No existing files are restructured — only modified where new behavior is injected.

**Tech Stack:** Go 1.26, Gin, gorilla/websocket, go-ethereum, PostgreSQL (lib/pq), Redis (go-redis/v9), Viper, golang-migrate, zap, golang-jwt, gqlgen, swaggo/swag, prometheus/client_golang, testcontainers-go

---

## Phase 1: Infrastructure

### Task 1: Add zap dependency and create log package

**Files:**
- Create: `internal/log/log.go`
- Modify: `go.mod` (via `go get`)

- [ ] **Step 1: Install zap dependency**

```bash
go get go.uber.org/zap
```

- [ ] **Step 2: Create `internal/log/log.go`**

```go
package log

import (
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Logger *zap.Logger

func Init(level string) {
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(parseLevel(level))
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var err error
	Logger, err = cfg.Build()
	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}
}

func Sync() {
	if Logger != nil {
		_ = Logger.Sync()
	}
}

func parseLevel(s string) zapcore.Level {
	switch strings.ToLower(s) {
	case "debug":
		return zapcore.DebugLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}
```

- [ ] **Step 3: Verify build**

```bash
go build ./...
```
Expected: build succeeds with no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/log/log.go go.mod go.sum
git commit -m "feat: add zap structured logging package"
```

---

### Task 2: Wire zap into main.go and convert all log sites

**Files:**
- Modify: `cmd/api/main.go`
- Modify: `internal/service/scheduler.go`
- Modify: `internal/watcher/watcher.go`
- Modify: `internal/ws/hub.go`

- [ ] **Step 1: Update main.go imports and replace log calls**

Replace the `"log"` import with `logpkg "nft-market-backend/internal/log"`. Replace every `log.Fatalf(...)` / `log.Println(...)` / `log.Printf(...)` call:

`cmd/api/main.go` — import block add:
```go
logpkg "nft-market-backend/internal/log"
```

Replace all logging in main():
```go
// old: log.Fatalf("config: %v", err)
logpkg.Logger.Fatal("config load failed", zap.Error(err))

// old: log.Fatalf("database open: %v", err)
logpkg.Logger.Fatal("database open failed", zap.Error(err))

// old: log.Fatalf("database ping: %v", err)
logpkg.Logger.Fatal("database ping failed", zap.Error(err))

// old: log.Println("database connected")
logpkg.Logger.Info("database connected")

// old: log.Fatalf("redis: %v", err)
logpkg.Logger.Fatal("redis connect failed", zap.Error(err))

// old: log.Println("redis connected")
logpkg.Logger.Info("redis connected")

// old: log.Fatalf("rpc client: %v", err)
logpkg.Logger.Fatal("rpc client failed", zap.Error(err))

// old: log.Printf("api listening on :%d", cfg.Server.Port)
logpkg.Logger.Info("api listening", zap.Int("port", cfg.Server.Port))

// old: log.Fatalf("server: %v", err)
logpkg.Logger.Fatal("server failed", zap.Error(err))

// old: log.Println("shutting down...")
logpkg.Logger.Info("shutting down...")
```

At the top of main(), before config.Load:
```go
logpkg.Init(os.Getenv("LOG_LEVEL"))
defer logpkg.Sync()
```

- [ ] **Step 2: Update scheduler.go**

Replace `"log"` import and use `logpkg.Logger`:

```go
import (
	"context"
	"time"

	"go.uber.org/zap"

	logpkg "nft-market-backend/internal/log"
)

// inside Run():
// old: log.Printf("scheduler: expire orders: %v", err)
logpkg.Logger.Error("scheduler: expire orders failed", zap.Error(err))

// old: log.Printf("scheduler: expired %d orders", n)
logpkg.Logger.Info("scheduler: expired orders", zap.Int64("count", n))

// in refreshMetadata():
// old: log.Printf("scheduler: refresh metadata: %v", err)
logpkg.Logger.Error("scheduler: refresh metadata failed", zap.Error(err))
```

- [ ] **Step 3: Update watcher.go**

Replace `"log"` import and all `log.Printf` calls with structured equivalents using `logpkg.Logger` and `zap` fields. Each log site should include relevant typed fields like `zap.Int64("chain_id", ...)` or `zap.String("event", ...)`.

- [ ] **Step 4: Update ws/hub.go**

Replace `"log"` import. Change `log.Printf("ws: marshal broadcast: %v", err)` to:
```go
logpkg.Logger.Error("ws: marshal broadcast failed", zap.Error(err))
```

- [ ] **Step 5: Verify build and manual log output check**

```bash
go build ./cmd/api/
go vet ./...
```

Then run the binary and verify log output is JSON-formatted.

- [ ] **Step 6: Commit**

```bash
git add cmd/api/main.go internal/service/scheduler.go internal/watcher/watcher.go internal/ws/hub.go
git commit -m "feat: replace log.Printf with zap structured logging across all files"
```

---

### Task 3: Add uuid dependency

- [ ] **Step 1: Install uuid**

```bash
go get github.com/google/uuid
```

- [ ] **Step 2: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add google/uuid dependency"
```

---

### Task 4: Add RequestID middleware

**Files:**
- Create: `internal/middleware/requestid.go`

- [ ] **Step 1: Create `internal/middleware/requestid.go`**

```go
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = uuid.New().String()
		}
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/middleware/
```

- [ ] **Step 3: Commit**

```bash
git add internal/middleware/requestid.go
git commit -m "feat: add RequestID middleware"
```

---

### Task 5: Add AccessLog middleware

**Files:**
- Create: `internal/middleware/accesslog.go`

- [ ] **Step 1: Create `internal/middleware/accesslog.go`**

```go
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
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/middleware/
```

- [ ] **Step 3: Commit**

```bash
git add internal/middleware/accesslog.go
git commit -m "feat: add AccessLog middleware"
```

---

### Task 6: Add health check handler

**Files:**
- Create: `internal/handler/health.go`

- [ ] **Step 1: Create `internal/handler/health.go`**

```go
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
```

- [ ] **Step 2: Verify RPC client satisfies the interface**

The existing `rpc.Client` struct (in `internal/rpc/client.go`) must have a `BlockNumber(ctx context.Context) (uint64, error)` method. Verify by checking the file. If it doesn't exist, add it.

- [ ] **Step 3: Expose Redis client from CacheService**

`internal/service/cache.go` already has `rdb *redis.Client` as private. Need to expose it via a new method:

```go
// Client returns the underlying redis client for health checks.
func (c *CacheService) Client() *redis.Client {
	return c.rdb
}
```

- [ ] **Step 4: Verify build**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/handler/health.go internal/service/cache.go
git commit -m "feat: add health/ready check endpoints"
```

---

### Task 7: Add golang-migrate and auto-run migrations at startup

**Files:**
- Modify: `cmd/api/main.go`
- Modify: `config/config.yaml`
- Modify: `internal/config/config.go`

- [ ] **Step 1: Install golang-migrate**

```bash
go get github.com/golang-migrate/migrate/v4
go get github.com/golang-migrate/migrate/v4/database/postgres
go get github.com/golang-migrate/migrate/v4/source/file
```

- [ ] **Step 2: Add migration config fields**

`config/config.yaml` — add under `server:` or at top level:
```yaml
migration:
  enabled: true
```

`internal/config/config.go` — add to Config struct and add new struct:
```go
type MigrationConfig struct {
	Enabled bool
}

type Config struct {
	Server    ServerConfig
	Database  DatabaseConfig
	Redis     RedisConfig
	Ethereum  EthereumConfig
	Migration MigrationConfig
}
```

- [ ] **Step 3: Add migration runner in main.go**

In `cmd/api/main.go`, after DB ping succeeds and before repositories are created:

```go
import (
	// ... existing imports
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// After db.Ping() succeeds:
if cfg.Migration.Enabled {
	logpkg.Logger.Info("running database migrations")
	m, err := migrate.New("file://migrations", cfg.Database.DSN())
	if err != nil {
		logpkg.Logger.Fatal("migration init failed", zap.Error(err))
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		logpkg.Logger.Fatal("migration failed", zap.Error(err))
	}
	logpkg.Logger.Info("migrations complete")
}
```

- [ ] **Step 4: Verify build and manual test**

```bash
go build ./cmd/api/
```

Start the service with a fresh database and verify migrations are applied. Then restart and verify no errors from re-running.

- [ ] **Step 5: Commit**

```bash
git add cmd/api/main.go config/config.yaml internal/config/config.go go.mod go.sum
git commit -m "feat: add automatic database migration runner at startup"
```

---

### Task 8: Wire all Phase 1 middleware and handlers into the router

**Files:**
- Modify: `cmd/api/main.go`

- [ ] **Step 1: Update main.go router setup**

In `cmd/api/main.go`, update the router section:

```go
// Router.
router := gin.New()
router.Use(gin.Recovery())
router.Use(middleware.RequestID())
router.Use(middleware.AccessLog())
router.Use(middleware.CORS())
router.Use(middleware.RateLimit(10, 20))

// Health checks.
healthH := handler.NewHealthHandler(db, cacheSvc.Client(), rpcClient)
router.GET("/health", healthH.Health)
router.GET("/ready", healthH.Ready)

api := router.Group("/api/v1")
// ... existing routes unchanged ...
```

Remove the now-unnecessary `"log"` import (already removed in Task 2).

- [ ] **Step 2: Verify build and test endpoints**

```bash
go build ./cmd/api/
```

Start the service and test:
```bash
curl http://localhost:8080/health
curl -v http://localhost:8080/api/v1/orders 2>&1 | grep -i x-request-id
```

- [ ] **Step 3: Commit**

```bash
git add cmd/api/main.go
git commit -m "feat: wire Phase 1 middleware (RequestID, AccessLog, health endpoints) into router"
```

---

### Task 9: Add GitHub Actions CI pipeline

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Create `.github/workflows/ci.yml`**

```yaml
name: CI

on:
  push:
    branches: [master]
  pull_request:
    branches: [master]

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.26"
      - name: go vet
        run: go vet ./...
      - name: golangci-lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: latest

  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:16
        env:
          POSTGRES_USER: app
          POSTGRES_PASSWORD: test
          POSTGRES_DB: nft_market_test
        ports:
          - 5432:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
      redis:
        image: redis:7
        ports:
          - 6379:6379
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.26"
      - name: Run tests
        run: go test ./... -coverprofile=coverage.out -covermode=atomic
      - name: Upload coverage
        uses: actions/upload-artifact@v4
        with:
          name: coverage
          path: coverage.out

  build:
    needs: [lint, test]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.26"
      - name: Build
        run: CGO_ENABLED=0 go build -o bin/api ./cmd/api/
```

- [ ] **Step 2: Verify .github directory exists**

```
mkdir -p .github/workflows
```

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add GitHub Actions CI pipeline (lint, test, build)"
```

---

## Phase 2: Security

### Task 10: Add JWT and personal-sign auth service

**Files:**
- Create: `internal/domain/auth.go`
- Create: `internal/service/auth.go`
- Modify: `config/config.yaml`
- Modify: `internal/config/config.go`

- [ ] **Step 1: Create `internal/domain/auth.go`**

```go
package domain

type AuthChallenge struct {
	Challenge string `json:"challenge"`
	Nonce     string `json:"nonce"`
	IssuedAt  string `json:"issuedAt"`
}

type LoginRequest struct {
	Address   string `json:"address" binding:"required"`
	Signature string `json:"signature" binding:"required"`
}

type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}
```

- [ ] **Step 2: Add auth config fields**

`config/config.yaml`:
```yaml
auth:
  jwt_secret: ""
  jwt_expiry: 24h
  challenge_ttl: 5m
```

`internal/config/config.go` — add to Config struct and define AuthConfig:
```go
type AuthConfig struct {
	JWTSecret    string        `mapstructure:"jwt_secret"`
	JWTExpiry    time.Duration `mapstructure:"jwt_expiry"`
	ChallengeTTL time.Duration `mapstructure:"challenge_ttl"`
}

type Config struct {
	Server    ServerConfig
	Database  DatabaseConfig
	Redis     RedisConfig
	Ethereum  EthereumConfig
	Migration MigrationConfig
	Auth      AuthConfig
}
```

- [ ] **Step 3: Install golang-jwt**

```bash
go get github.com/golang-jwt/jwt/v5
```

- [ ] **Step 4: Create `internal/service/auth.go`**

```go
package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"nft-market-backend/internal/domain"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang-jwt/jwt/v5"
)

type AuthService struct {
	jwtSecret    []byte
	jwtExpiry    time.Duration
	challengeTTL time.Duration
	cache        *CacheService
}

func NewAuthService(cache *CacheService, jwtSecret string, jwtExpiry, challengeTTL time.Duration) *AuthService {
	return &AuthService{
		jwtSecret:    []byte(jwtSecret),
		jwtExpiry:    jwtExpiry,
		challengeTTL: challengeTTL,
		cache:        cache,
	}
}

func (s *AuthService) GenerateChallenge(ctx context.Context, address string) (*domain.AuthChallenge, error) {
	if !common.IsHexAddress(address) {
		return nil, fmt.Errorf("INVALID_ADDRESS: not a valid ethereum address")
	}

	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	issuedAt := time.Now().UTC().Format(time.RFC3339)

	challenge := fmt.Sprintf("I am signing in to NFT Market.\n\nNonce: %s\nIssued At: %s", nonce, issuedAt)

	// Store nonce in Redis for later verification.
	key := "auth:nonce:" + strings.ToLower(address)
	if err := s.cache.Set(ctx, key, nonce, s.challengeTTL); err != nil {
		return nil, fmt.Errorf("store nonce: %w", err)
	}

	return &domain.AuthChallenge{
		Challenge: challenge,
		Nonce:     nonce,
		IssuedAt:  issuedAt,
	}, nil
}

func (s *AuthService) Login(ctx context.Context, address string, signature string) (*domain.LoginResponse, error) {
	if !common.IsHexAddress(address) {
		return nil, fmt.Errorf("INVALID_ADDRESS: not a valid ethereum address")
	}

	// Retrieve stored nonce.
	key := "auth:nonce:" + strings.ToLower(address)
	var storedNonce string
	if err := s.cache.Get(ctx, key, &storedNonce); err != nil {
		return nil, fmt.Errorf("INVALID_CHALLENGE: nonce not found or expired")
	}

	issuedAt := time.Now().UTC().Format(time.RFC3339)
	challenge := fmt.Sprintf("I am signing in to NFT Market.\n\nNonce: %s\nIssued At: %s", storedNonce, issuedAt)

	// Verify personal_sign signature.
	recovered, err := recoverPersonalSign(challenge, signature)
	if err != nil {
		return nil, fmt.Errorf("SIGNATURE_INVALID: %w", err)
	}
	if !strings.EqualFold(recovered, address) {
		return nil, fmt.Errorf("SIGNATURE_MISMATCH: recovered %s, expected %s", recovered, address)
	}

	// Delete used nonce.
	_ = s.cache.Del(ctx, key)

	// Generate JWT.
	now := time.Now()
	expiresAt := now.Add(s.jwtExpiry)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": strings.ToLower(address),
		"iat": now.Unix(),
		"exp": expiresAt.Unix(),
	})
	tokenStr, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("sign token: %w", err)
	}

	return &domain.LoginResponse{
		Token:     tokenStr,
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	}, nil
}

func (s *AuthService) ValidateToken(tokenStr string) (string, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return "", fmt.Errorf("parse token: %w", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", fmt.Errorf("invalid token")
	}
	sub, _ := claims["sub"].(string)
	return sub, nil
}

// recoverPersonalSign recovers the signer address from an Ethereum personal_sign signature.
func recoverPersonalSign(message, sigHex string) (string, error) {
	sig, err := hex.DecodeString(strings.TrimPrefix(sigHex, "0x"))
	if err != nil {
		return "", fmt.Errorf("decode signature: %w", err)
	}
	if len(sig) != 65 {
		return "", fmt.Errorf("signature length %d, expected 65", len(sig))
	}

	// personal_sign prepends "\x19Ethereum Signed Message:\n" + len(message).
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(message))
	hash := crypto.Keccak256Hash([]byte(prefix + message))

	// Transform V from [27,28] to [0,1].
	sig[64] -= 27
	pubKey, err := crypto.Ecrecover(hash.Bytes(), sig)
	if err != nil {
		return "", fmt.Errorf("ecrecover: %w", err)
	}
	recoveredPub, err := crypto.UnmarshalPubkey(pubKey)
	if err != nil {
		return "", fmt.Errorf("unmarshal pubkey: %w", err)
	}
	return crypto.PubkeyToAddress(*recoveredPub).Hex(), nil
}
```

- [ ] **Step 5: Verify build**

```bash
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/domain/auth.go internal/service/auth.go config/config.yaml internal/config/config.go go.mod go.sum
git commit -m "feat: add JWT auth service with wallet personal_sign login"
```

---

### Task 11: Add auth handler

**Files:**
- Create: `internal/handler/auth.go`

- [ ] **Step 1: Create `internal/handler/auth.go`**

```go
package handler

import (
	"net/http"

	"nft-market-backend/internal/domain"
	"nft-market-backend/internal/service"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	authSvc *service.AuthService
}

func NewAuthHandler(authSvc *service.AuthService) *AuthHandler {
	return &AuthHandler{authSvc: authSvc}
}

func (h *AuthHandler) Challenge(c *gin.Context) {
	address := c.Query("address")
	if address == "" {
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{
			Error:   "MISSING_PARAM",
			Message: "address query parameter is required",
		})
		return
	}

	challenge, err := h.authSvc.GenerateChallenge(c.Request.Context(), address)
	if err != nil {
		code := extractErrorCode(err.Error())
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{Error: code, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, challenge)
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req domain.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{
			Error:   "INVALID_REQUEST",
			Message: err.Error(),
		})
		return
	}

	resp, err := h.authSvc.Login(c.Request.Context(), req.Address, req.Signature)
	if err != nil {
		code := extractErrorCode(err.Error())
		status := http.StatusBadRequest
		if code == "SIGNATURE_INVALID" || code == "SIGNATURE_MISMATCH" {
			status = http.StatusUnauthorized
		}
		c.JSON(status, domain.ErrorResponse{Error: code, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/handler/auth.go
git commit -m "feat: add auth handler (challenge + login endpoints)"
```

---

### Task 12: Add JWT auth middleware

**Files:**
- Create: `internal/middleware/auth.go`

- [ ] **Step 1: Create `internal/middleware/auth.go`**

```go
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
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/middleware/
```

- [ ] **Step 3: Commit**

```bash
git add internal/middleware/auth.go
git commit -m "feat: add JWT auth middleware"
```

---

### Task 13: Wire Phase 2 into main.go — auth routes, middleware routing, body size limit

**Files:**
- Modify: `cmd/api/main.go`
- Modify: `config/config.yaml` (if not already done)

- [ ] **Step 1: Update main.go — add auth service and handler initialization**

After `sigSvc` creation in main():
```go
// Auth service.
authSvc := service.NewAuthService(
	cacheSvc,
	cfg.Auth.JWTSecret,
	cfg.Auth.JWTExpiry,
	cfg.Auth.ChallengeTTL,
)
authH := handler.NewAuthHandler(authSvc)
```

- [ ] **Step 2: Update router — public auth routes, protected route group with body size limit**

```go
// Router.
router := gin.New()
router.Use(gin.Recovery())
router.Use(middleware.RequestID())
router.Use(middleware.AccessLog())
router.Use(middleware.CORS())
router.Use(middleware.RateLimit(10, 20))

// Body size limit: 1MB
router.MaxMultipartMemory = 1 << 20

// Health checks (no auth).
healthH := handler.NewHealthHandler(db, cacheSvc.Client(), rpcClient)
router.GET("/health", healthH.Health)
router.GET("/ready", healthH.Ready)

// Auth routes (no auth required).
auth := router.Group("/api/v1/auth")
{
	auth.GET("/challenge", authH.Challenge)
	auth.POST("/login", authH.Login)
}

api := router.Group("/api/v1")
// Public routes (no auth).
{
	api.GET("/orders", orderH.List)
	api.GET("/orders/best", orderH.Best)
	api.GET("/orders/:hash", orderH.Get)
	api.GET("/collections", collectionH.List)
	api.GET("/collections/:address", collectionH.Get)
	api.GET("/stats", collectionH.GlobalStats)
	api.GET("/stats/:collection", collectionH.CollectionStats)
}

// Protected routes (auth required).
protected := router.Group("/api/v1")
protected.Use(middleware.Auth(authSvc))
{
	protected.POST("/orders", orderH.Submit)
	protected.GET("/users/:address/orders", orderH.UserOrders)
	protected.POST("/graphql", graphqlH.Handle)
}

router.GET("/ws/orders", wsH.Handle)
```

- [ ] **Step 3: Update env reading for JWT secret**

In config, add environment variable override. Viper's `AutomaticEnv()` will pick up `AUTH_JWTSECRET` by default, but the YAML key is `auth.jwt_secret`. In `config.go` Load(), after `viper.ReadInConfig()`, ensure env var overrides work. Add:

```go
viper.SetEnvPrefix("")
viper.AutomaticEnv()
```

Alternatively, ensure `JWT_SECRET` env var is directly read in main.go if the viper env binding doesn't work with nested keys.

- [ ] **Step 4: Verify build**

```bash
go build ./cmd/api/
```

- [ ] **Step 5: Commit**

```bash
git add cmd/api/main.go internal/config/config.go
git commit -m "feat: wire auth routes, JWT middleware, and protected/public route groups"
```

---

### Task 14: Tighten CORS

**Files:**
- Modify: `internal/middleware/cors.go`

- [ ] **Step 1: Update `internal/middleware/cors.go`**

```go
package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func CORS(allowedOrigins []string) gin.HandlerFunc {
	originMap := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originMap[strings.ToLower(o)] = true
	}

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" {
			originLower := strings.ToLower(origin)
			if originMap[originLower] {
				c.Header("Access-Control-Allow-Origin", origin)
			}
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Allow-Credentials", "true")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
```

- [ ] **Step 2: Update config**

`config/config.yaml`:
```yaml
server:
  port: 8080
  allowed_origins:
    - "http://localhost:3000"
```

`internal/config/config.go` ServerConfig:
```go
type ServerConfig struct {
	Port           int
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}
```

- [ ] **Step 3: Update main.go CORS call**

```go
router.Use(middleware.CORS(cfg.Server.AllowedOrigins))
```

- [ ] **Step 4: Verify build**

```bash
go build ./cmd/api/
```

- [ ] **Step 5: Commit**

```bash
git add internal/middleware/cors.go internal/config/config.go config/config.yaml cmd/api/main.go
git commit -m "feat: restrict CORS to configurable origin allowlist"
```

---

### Task 15: Fix input validation — parseOrderFilter and SetString errors

**Files:**
- Modify: `internal/handler/order.go`
- Modify: `internal/service/order.go`

- [ ] **Step 1: Fix parseOrderFilter Atoi errors**

In `internal/handler/order.go`, update `parseOrderFilter` to return error:

```go
func parseOrderFilter(c *gin.Context) (domain.OrderFilter, error) {
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
	if v := c.Query("side"); v != "" {
		s, err := strconv.Atoi(v)
		if err != nil || s < 0 || s > 1 {
			return filter, fmt.Errorf("invalid side: %s", v)
		}
		side := domain.OrderSide(s)
		filter.Side = &side
	}
	if v := c.Query("kind"); v != "" {
		k, err := strconv.Atoi(v)
		if err != nil || k < 0 || k > 4 {
			return filter, fmt.Errorf("invalid kind: %s", v)
		}
		kind := domain.OrderKind(k)
		filter.Kind = &kind
	}
	if v := c.Query("assetType"); v != "" {
		a, err := strconv.Atoi(v)
		if err != nil || a < 0 || a > 1 {
			return filter, fmt.Errorf("invalid assetType: %s", v)
		}
		at := domain.AssetType(a)
		filter.AssetType = &at
	}
	if v := c.Query("status"); v != "" {
		s, err := strconv.Atoi(v)
		if err != nil || s < 0 || s > 3 {
			return filter, fmt.Errorf("invalid status: %s", v)
		}
		st := domain.OrderStatus(s)
		filter.Status = &st
	}
	if v := c.Query("minPrice"); v != "" {
		filter.MinPrice = domain.NewBigInt(nil)
		if _, ok := filter.MinPrice.Int.SetString(v, 10); !ok {
			return filter, fmt.Errorf("invalid minPrice: %s", v)
		}
	}
	if v := c.Query("maxPrice"); v != "" {
		filter.MaxPrice = domain.NewBigInt(nil)
		if _, ok := filter.MaxPrice.Int.SetString(v, 10); !ok {
			return filter, fmt.Errorf("invalid maxPrice: %s", v)
		}
	}

	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if err != nil || pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50
	}
	filter.Limit = pageSize
	filter.Offset = (page - 1) * pageSize

	return filter, nil
}
```

Add `"fmt"` to imports.

- [ ] **Step 2: Update List handler to use new return type**

```go
func (h *OrderHandler) List(c *gin.Context) {
	filter, err := parseOrderFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{
			Error:   "INVALID_FILTER",
			Message: err.Error(),
		})
		return
	}
	// ... rest unchanged ...
}
```

- [ ] **Step 3: Fix SetString error checks in requestToOrder**

In `internal/service/order.go`, update `requestToOrder` to check SetString errors:

```go
func requestToOrder(req *domain.SubmitOrderRequest, chainID int64) *domain.Order {
	tokenID, ok := new(big.Int).SetString(req.TokenID, 10)
	if !ok {
		tokenID = new(big.Int)
	}
	amount, ok := new(big.Int).SetString(req.Amount, 10)
	if !ok {
		amount = new(big.Int)
	}
	price, ok := new(big.Int).SetString(req.Price, 10)
	if !ok {
		price = new(big.Int)
	}
	startPrice, ok := new(big.Int).SetString(req.StartPrice, 10)
	if !ok {
		startPrice = new(big.Int)
	}
	salt, ok := new(big.Int).SetString(req.Salt, 10)
	if !ok {
		salt = new(big.Int)
	}
	// ... rest unchanged ...
}
```

- [ ] **Step 4: Verify build and vet**

```bash
go build ./...
go vet ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/handler/order.go internal/service/order.go
git commit -m "fix: validate parseOrderFilter params and check SetString errors"
```

---

## Phase 3: Feature Completion

### Task 16: Set up gqlgen and write schema

**Files:**
- Create: `gqlgen.yml`
- Create: `internal/graphql/schema.graphql`

- [ ] **Step 1: Install gqlgen**

```bash
go get github.com/99designs/gqlgen
```

- [ ] **Step 2: Create `gqlgen.yml`**

```yaml
schema:
  - internal/graphql/schema.graphql

exec:
  filename: internal/graphql/generated.go
  package: graphql

model:
  filename: internal/graphql/models_gen.go
  package: graphql

resolver:
  layout: follow-schema
  dir: internal/graphql
  package: graphql
  filename_template: "{name}.resolvers.go"

models:
  BigInt:
    model: nft-market-backend/internal/graphql.BigInt
  Order:
    model: nft-market-backend/internal/domain.Order
    fields:
      hash:
        resolver: true
  OrderSide:
    model: nft-market-backend/internal/domain.OrderSide
  OrderKind:
    model: nft-market-backend/internal/domain.OrderKind
  AssetType:
    model: nft-market-backend/internal/domain.AssetType
  OrderStatus:
    model: nft-market-backend/internal/domain.OrderStatus
  Collection:
    model: nft-market-backend/internal/domain.Collection
```

- [ ] **Step 3: Create `internal/graphql/schema.graphql`**

```graphql
scalar BigInt

type Order {
  hash: String!
  maker: String!
  taker: String
  side: Int!
  kind: Int!
  assetType: Int!
  collection: String!
  tokenId: BigInt!
  amount: BigInt!
  price: BigInt!
  startPrice: BigInt
  endTime: String!
  startTime: String!
  salt: BigInt!
  signature: String!
  status: Int!
  counter: BigInt!
  currentPrice: BigInt!
  extra: String!
  createdAt: String!
}

type OrderConnection {
  orders: [Order!]!
  total: Int!
  page: Int!
  pageSize: Int!
}

type Collection {
  address: String!
  name: String!
  symbol: String!
  description: String!
  image: String!
  banner: String!
  website: String!
  twitter: String!
  discord: String!
  slug: String!
  category: String!
  isVerified: Boolean!
  isBlocked: Boolean!
  floorPrice: BigInt
  bestBid: BigInt
  listedCount: Int!
  createdAt: String!
}

type CollectionConnection {
  collections: [Collection!]!
  total: Int!
  page: Int!
  pageSize: Int!
}

type GlobalStats {
  totalOrders: Int!
  totalCollections: Int!
  totalTraders: Int!
}

type CollectionStats {
  floorPrice: BigInt
  bestBid: BigInt
  listedCount: Int!
}

input OrderFilter {
  collection: String
  maker: String
  side: Int
  kind: Int
  assetType: Int
  status: Int
  minPrice: String
  maxPrice: String
}

input SubmitOrderInput {
  maker: String!
  taker: String
  side: Int!
  kind: Int!
  assetType: Int!
  collection: String!
  tokenId: String!
  amount: String!
  price: String!
  paymentToken: String
  startPrice: String
  startTime: Int!
  endTime: Int!
  salt: String!
  signature: String!
  extra: String
}

type Query {
  order(hash: String!): Order
  orders(filter: OrderFilter, page: Int!, pageSize: Int!): OrderConnection!
  bestOrder(collection: String!, side: Int!): Order
  collection(address: String!): Collection
  collections(search: String, page: Int!, pageSize: Int!): CollectionConnection!
  userOrders(address: String!, status: [Int!], page: Int!, pageSize: Int!): OrderConnection!
  stats: GlobalStats!
  collectionStats(address: String!): CollectionStats!
}

type Mutation {
  submitOrder(input: SubmitOrderInput!): Order!
}

type Subscription {
  orderUpdated(collection: String!): Order!
}
```

- [ ] **Step 4: Run gqlgen generate**

```bash
go run github.com/99designs/gqlgen generate
```

- [ ] **Step 5: Commit**

```bash
git add gqlgen.yml internal/graphql/
git commit -m "feat: add gqlgen configuration and GraphQL schema"
```

---

### Task 17: Implement GraphQL resolvers

**Files:**
- Create: `internal/graphql/resolver.go`
- Create: `internal/graphql/scalars.go`
- Modify: `internal/graphql/*.resolvers.go` (generated stubs need filling)

- [ ] **Step 1: Create `internal/graphql/resolver.go`**

```go
package graphql

import (
	"nft-market-backend/internal/repository"
	"nft-market-backend/internal/service"
)

type Resolver struct {
	OrderSvc      *service.OrderService
	CollectionRepo *repository.CollectionRepo
	OrderRepo     *repository.OrderRepo
	MetadataSvc   *service.MetadataService
	Hub           HubBroadcaster
}

type HubBroadcaster interface {
	Broadcast(collection string, msg interface{})
}
```

- [ ] **Step 2: Create `internal/graphql/scalars.go`**

```go
package graphql

import (
	"fmt"
	"io"
	"math/big"
	"strconv"

	"github.com/99designs/gqlgen/graphql"
)

type BigInt struct {
	*big.Int
}

func NewBigInt(i *big.Int) *BigInt {
	if i == nil {
		return &BigInt{new(big.Int)}
	}
	return &BigInt{i}
}

func MarshalBigInt(bi *big.Int) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		_, _ = io.WriteString(w, strconv.Quote(bi.String()))
	})
}

func UnmarshalBigInt(v interface{}) (*big.Int, error) {
	switch v := v.(type) {
	case string:
		i := new(big.Int)
		if _, ok := i.SetString(v, 10); !ok {
			return nil, fmt.Errorf("invalid BigInt: %s", v)
		}
		return i, nil
	default:
		return nil, fmt.Errorf("BigInt must be a string")
	}
}
```

- [ ] **Step 3: Implement query resolvers**

In `internal/graphql/order.resolvers.go`, implement:

```go
func (r *queryResolver) Order(ctx context.Context, hash string) (*domain.Order, error) {
	order, err := r.OrderSvc.GetByHash(hash)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, nil
	}
	return order, nil
}

func (r *queryResolver) Orders(ctx context.Context, filter *domain.OrderFilter, page int, pageSize int) (*OrderConnection, error) {
	f := buildFilter(filter, page, pageSize)
	orders, total, err := r.OrderSvc.Find(f)
	if err != nil {
		return nil, err
	}
	return &OrderConnection{Orders: orders, Total: int(total), Page: page, PageSize: pageSize}, nil
}

func (r *queryResolver) BestOrder(ctx context.Context, collection string, side int) (*domain.Order, error) {
	return r.OrderSvc.GetBest(collection, domain.OrderSide(side))
}

func (r *queryResolver) UserOrders(ctx context.Context, address string, status []int, page int, pageSize int) (*OrderConnection, error) {
	// Build filter with statuses. For simplicity, use the first status if provided.
	var s *domain.OrderStatus
	if len(status) > 0 {
		st := domain.OrderStatus(status[0])
		s = &st
	}
	f := domain.OrderFilter{
		Maker:  address,
		Status: s,
		Limit:  pageSize,
		Offset: (page - 1) * pageSize,
	}
	orders, total, err := r.OrderSvc.Find(f)
	if err != nil {
		return nil, err
	}
	return &OrderConnection{Orders: orders, Total: int(total), Page: page, PageSize: pageSize}, nil
}

func (r *mutationResolver) SubmitOrder(ctx context.Context, input domain.SubmitOrderRequest) (*domain.Order, error) {
	return r.OrderSvc.Submit(&input)
}
```

Implement collection resolvers in `internal/graphql/collection.resolvers.go` similarly, calling `r.CollectionRepo` methods.

- [ ] **Step 4: Implement subscription resolver**

```go
func (r *subscriptionResolver) OrderUpdated(ctx context.Context, collection string) (<-chan *domain.Order, error) {
	ch := make(chan *domain.Order, 1)
	// Register with the WebSocket hub for collection events.
	// For now, return a channel that never closes (client disconnect handles cleanup).
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}
```

- [ ] **Step 5: Verify build**

```bash
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/graphql/
git commit -m "feat: implement GraphQL query and mutation resolvers"
```

---

### Task 18: Wire GraphQL into main.go

**Files:**
- Modify: `cmd/api/main.go`
- Modify: `internal/handler/graphql.go`

- [ ] **Step 1: Replace graphql stub handler**

Update `internal/handler/graphql.go` to serve gqlgen handler:

```go
package handler

import (
	gqlgen "nft-market-backend/internal/graphql"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/gin-gonic/gin"
)

type GraphQLHandler struct {
	srv *handler.Server
}

func NewGraphQLHandler(resolver *gqlgen.Resolver) *GraphQLHandler {
	srv := handler.NewDefaultServer(gqlgen.NewExecutableSchema(gqlgen.Config{Resolvers: resolver}))
	return &GraphQLHandler{srv: srv}
}

func (h *GraphQLHandler) Handle(c *gin.Context) {
	h.srv.ServeHTTP(c.Writer, c.Request)
}

func (h *GraphQLHandler) Playground(c *gin.Context) {
	playground.Handler("GraphQL", "/api/v1/graphql").ServeHTTP(c.Writer, c.Request)
}
```

- [ ] **Step 2: Update main.go — create resolver and new handler**

```go
gqlResolver := &graphql.Resolver{
	OrderSvc:       orderSvc,
	CollectionRepo: collectionRepo,
	OrderRepo:      orderRepo,
	MetadataSvc:    metadataSvc,
	Hub:            hub,
}
graphqlH := handler.NewGraphQLHandler(gqlResolver)
```

Add GET route for GraphQL playground (public):
```go
api.GET("/graphql", graphqlH.Playground)
```

- [ ] **Step 3: Add gqlgen-generated files to build**

Verify the gqlgen-generated `.go` files are in `internal/graphql/` and compile cleanly.

- [ ] **Step 4: Verify build**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add cmd/api/main.go internal/handler/graphql.go
git commit -m "feat: wire GraphQL endpoint with gqlgen handler and playground"
```

---

### Task 19: Add swaggo OpenAPI annotations

**Files:**
- Modify: `internal/handler/order.go`
- Modify: `internal/handler/collection.go`
- Modify: `internal/handler/auth.go`
- Modify: `cmd/api/main.go` (swagger setup)
- Create: `docs/` (via swag init)

- [ ] **Step 1: Install swaggo**

```bash
go get github.com/swaggo/swag/cmd/swag
go get github.com/swaggo/gin-swagger
go get github.com/swaggo/files
```

- [ ] **Step 2: Add main.go annotations**

At top of `cmd/api/main.go`, before `package main`:
```go
// @title           NFT Market API
// @version         1.0
// @description     EIP-712 signed order DEX backend
// @host            localhost:8080
// @BasePath        /api/v1
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
```

- [ ] **Step 3: Add swag annotations to handlers**

For each handler function, add swag annotation block. Example for `OrderHandler.Submit`:

```go
// @Summary      Submit signed order
// @Description  Validates EIP-712 signature and persists a new order
// @Tags         orders
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body domain.SubmitOrderRequest true "Order payload"
// @Success      201 {object} object{orderHash=string,status=string}
// @Failure      400 {object} domain.ErrorResponse
// @Router       /orders [post]
func (h *OrderHandler) Submit(c *gin.Context) {
```

Add similar annotations for `List`, `Get`, `Best`, `UserOrders`, collection handlers, auth handlers, and stats handlers.

- [ ] **Step 4: Run swag init**

```bash
swag init -g cmd/api/main.go -o docs/
```

- [ ] **Step 5: Wire swagger UI in main.go**

```go
import (
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	_ "nft-market-backend/docs"
)

// Add route:
router.GET("/api/v1/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
```

- [ ] **Step 6: Verify build and swagger UI**

```bash
go build ./...
```

Browse to `http://localhost:8080/api/v1/docs/index.html`.

- [ ] **Step 7: Commit**

```bash
git add cmd/api/main.go internal/handler/*.go docs/ go.mod go.sum
git commit -m "docs: add Swagger/OpenAPI documentation with swaggo"
```

---

## Phase 4: Quality

### Task 20: Add domain.AppError type and refactor service errors

**Files:**
- Create: `internal/domain/errors.go`
- Modify: `internal/service/order.go`
- Modify: `internal/service/auth.go`
- Modify: `internal/service/event.go`
- Modify: `internal/handler/order.go` (use errors.As)

- [ ] **Step 1: Create `internal/domain/errors.go`**

```go
package domain

import "fmt"

type AppError struct {
	Code    string
	Message string
	Err     error
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func NewAppError(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Err: err}
}
```

- [ ] **Step 2: Refactor order service to return AppError**

Replace `fmt.Errorf("ORDER_SIGNATURE_INVALID: %w", err)` pattern with `domain.NewAppError("ORDER_SIGNATURE_INVALID", "signature verification failed", err)`.

Do the same for all error codes: INVALID_MAKER, INVALID_TAKER, INVALID_SIDE, INVALID_KIND, INVALID_ASSET_TYPE, INVALID_AMOUNT, INVALID_PRICE, ORDER_EXPIRED, INVALID_START_TIME, INVALID_DUTCH_AUCTION, ORDER_PERSIST_FAILED.

- [ ] **Step 3: Refactor auth service to return AppError**

Replace `fmt.Errorf("INVALID_ADDRESS: ...")` pattern.

- [ ] **Step 4: Refactor handler extractErrorCode to use errors.As**

```go
func extractErrorCode(err error) string {
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	parts := strings.SplitN(err.Error(), ": ", 2)
	code := strings.TrimSpace(parts[0])
	return strings.ToUpper(strings.ReplaceAll(code, " ", "_"))
}
```

- [ ] **Step 5: Fix silent error discards in event service**

In `internal/service/event.go`, replace `_ = s.cache.InvalidateOrders(...)` with:

```go
if err := s.cache.InvalidateOrders(context.Background(), order.Collection); err != nil {
	logpkg.Logger.Warn("failed to invalidate order cache", zap.String("collection", order.Collection), zap.Error(err))
}
```

Similarly for `_ = s.orderRepo.UpdateStatus(...)`, `_ = s.collectionRepo.Upsert(...)` etc. in other files.

- [ ] **Step 6: Verify build**

```bash
go build ./...
go vet ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/domain/errors.go internal/service/order.go internal/service/auth.go internal/service/event.go internal/handler/order.go
git commit -m "refactor: introduce AppError type and fix silent error discards"
```

---

### Task 21: Add Prometheus metrics middleware and /metrics endpoint

**Files:**
- Create: `internal/middleware/metrics.go`
- Modify: `cmd/api/main.go`

- [ ] **Step 1: Install prometheus client**

```bash
go get github.com/prometheus/client_golang/prometheus
go get github.com/prometheus/client_golang/prometheus/promhttp
```

- [ ] **Step 2: Create `internal/middleware/metrics.go`**

```go
package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latencies in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)
	OrdersSubmittedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "orders_submitted_total",
			Help: "Total number of orders submitted",
		},
	)
)

func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		c.Next()

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())

		HTTPRequestsTotal.WithLabelValues(c.Request.Method, path, status).Inc()
		HTTPRequestDuration.WithLabelValues(c.Request.Method, path).Observe(duration)
	}
}
```

- [ ] **Step 3: Wire metrics into main.go router and add /metrics endpoint**

```go
router.Use(middleware.Metrics())

// Prometheus metrics endpoint.
router.GET("/metrics", gin.WrapH(promhttp.Handler()))
```

- [ ] **Step 4: Add business metric in order handler**

In `handler/order.go` Submit handler, after successful submission:
```go
middleware.OrdersSubmittedTotal.Inc()
```

- [ ] **Step 5: Verify build and /metrics endpoint**

```bash
go build ./cmd/api/
```

Start the service and:
```bash
curl http://localhost:8080/metrics
```
Expected: Prometheus text format output with `http_requests_total`, `http_request_duration_seconds`, `orders_submitted_total`.

- [ ] **Step 6: Commit**

```bash
git add internal/middleware/metrics.go cmd/api/main.go internal/handler/order.go go.mod go.sum
git commit -m "feat: add Prometheus metrics middleware and /metrics endpoint"
```

---

### Task 22: Add testcontainers dependency

- [ ] **Step 1: Install testcontainers**

```bash
go get github.com/testcontainers/testcontainers-go
go get github.com/testcontainers/testcontainers-go/modules/postgres
```

- [ ] **Step 2: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add testcontainers-go dependency"
```

---

### Task 23: Add repository tests — OrderRepo

**Files:**
- Create: `internal/repository/order_test.go`

- [ ] **Step 1: Write test helper with testcontainers**

Create `internal/repository/order_test.go` with a test helper that starts a PostgreSQL container, runs migrations, and returns a connected `*sql.DB`:

```go
package repository

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupTestDB(t *testing.T) (*OrderRepo, func()) {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:16-alpine"),
		postgres.WithDatabase("nft_market_test"),
		postgres.WithUsername("app"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	// Run migrations.
	migrationSQL := `
	CREATE TABLE IF NOT EXISTS orders (
		id BIGSERIAL PRIMARY KEY,
		order_hash BYTEA NOT NULL,
		chain_id BIGINT NOT NULL,
		maker BYTEA NOT NULL,
		taker BYTEA,
		side SMALLINT NOT NULL,
		kind SMALLINT NOT NULL,
		asset_type SMALLINT NOT NULL,
		collection BYTEA NOT NULL,
		token_id NUMERIC(78,0) NOT NULL,
		amount NUMERIC(78,0) NOT NULL,
		payment_token BYTEA,
		price NUMERIC(78,0) NOT NULL,
		start_price NUMERIC(78,0),
		start_time BIGINT NOT NULL,
		end_time BIGINT NOT NULL,
		salt NUMERIC(78,0) NOT NULL,
		counter NUMERIC(78,0) NOT NULL DEFAULT 0,
		extra BYTEA,
		signature_r BYTEA NOT NULL,
		signature_s BYTEA NOT NULL,
		signature_v SMALLINT NOT NULL,
		status SMALLINT NOT NULL DEFAULT 0,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
		expired_at TIMESTAMP
	)
	`
	if _, err := db.ExecContext(ctx, migrationSQL); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	repo := NewOrderRepo(db)
	repo.ChainID = 31337

	cleanup := func() {
		db.Close()
		pgContainer.Terminate(ctx)
	}
	return repo, cleanup
}
```

(Actual test file will read and execute `../../migrations/001_initial.up.sql` instead of inline SQL.)

- [ ] **Step 2: Write TestOrderRepo_Insert**

```go
func TestOrderRepo_Insert(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	order := &domain.Order{
		OrderHash:    "0x" + strings.Repeat("aa", 32),
		Maker:        "0x" + strings.Repeat("11", 20),
		Taker:        "0x0000000000000000000000000000000000000000",
		Side:         domain.Sell,
		Kind:         domain.FixedPrice,
		AssetType:    domain.ERC721,
		Collection:   "0x" + strings.Repeat("22", 20),
		TokenID:      domain.NewBigInt(big.NewInt(1)),
		Amount:       domain.NewBigInt(big.NewInt(1)),
		PaymentToken: "0x0000000000000000000000000000000000000000",
		Price:        domain.NewBigInt(big.NewInt(1000000000000000000)), // 1 ETH
		StartPrice:   domain.NewBigInt(big.NewInt(0)),
		StartTime:    0,
		EndTime:      0,
		Salt:         domain.NewBigInt(big.NewInt(12345)),
		Counter:      domain.NewBigInt(big.NewInt(0)),
		Extra:        "0x0000000000000000000000000000000000000000000000000000000000000000",
		Status:       domain.Active,
		Signature:    "0x" + strings.Repeat("bb", 65),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	err := repo.Insert(order)
	if err != nil {
		t.Fatalf("insert order: %v", err)
	}

	// Fetch back and verify.
	found, err := repo.FindByHash(order.OrderHash)
	if err != nil {
		t.Fatalf("find by hash: %v", err)
	}
	if found == nil {
		t.Fatal("expected order not found")
	}
	if found.Maker != order.Maker {
		t.Errorf("maker mismatch: got %s want %s", found.Maker, order.Maker)
	}
	if found.Side != order.Side {
		t.Errorf("side mismatch: got %d want %d", found.Side, order.Side)
	}
}
```

- [ ] **Step 3: Write TestOrderRepo_Find_Filters**

Insert 3 orders with different collections/sides/statuses, then test:
- Filter by collection → returns correct subset
- Filter by side → returns correct subset
- Filter by status → returns correct subset
- Pagination → limit/offset works correctly

- [ ] **Step 4: Write TestOrderRepo_UpdateStatus**

Insert an active order, then:
- UpdateStatus to Filled → verify status changed
- UpdateStatus again on same order → verify no-op (only updates from Active)

- [ ] **Step 5: Write TestOrderRepo_ExpireOrders**

Insert an order with endTime set to 1 second ago, call ExpireOrders, verify status is Expired.

- [ ] **Step 6: Run tests**

```bash
go test ./internal/repository/ -v -count=1
```
Expected: all tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/repository/order_test.go
git commit -m "test: add OrderRepo integration tests with testcontainers"
```

---

### Task 24: Add service tests — OrderService

**Files:**
- Create: `internal/service/order_test.go`

- [ ] **Step 1: Write mock repository**

```go
type mockOrderRepo struct {
	insertErr error
	inserted  *domain.Order
}

func (m *mockOrderRepo) Insert(o *domain.Order) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.inserted = o
	return nil
}
// ... other methods returning appropriate defaults ...
```

- [ ] **Step 2: Write TestSubmit_Success**

Create a valid SubmitOrderRequest (FixedPrice), call `orderSvc.Submit()`, verify:
- No error returned
- Order hash is computed
- Status is Active

- [ ] **Step 3: Write TestSubmit_InvalidSignature**

Create an order with a bad signature, verify:
- Error code is ORDER_SIGNATURE_INVALID

- [ ] **Step 4: Write TestSubmit_Expired**

Create an order with EndTime in the past, verify:
- Error code is ORDER_EXPIRED

- [ ] **Step 5: Write TestSubmit_InvalidDutchAuction**

Create a DutchAuction with startPrice <= price, verify:
- Error code is INVALID_DUTCH_AUCTION

- [ ] **Step 6: Write TestSubmit_DuplicateSalt**

Mock the repo Insert to return a duplicate key error, verify:
- Error code is ORDER_PERSIST_FAILED

- [ ] **Step 7: Run tests**

```bash
go test ./internal/service/ -v -count=1
```
Expected: all tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/service/order_test.go
git commit -m "test: add OrderService unit tests with mock repository"
```

---

### Task 25: Add handler tests

**Files:**
- Create: `internal/handler/order_test.go`

- [ ] **Step 1: Write TestOrderHandler_Submit_Success**

Use `httptest.NewRecorder` + Gin test context to send a valid SubmitOrderRequest. Mock the order service. Verify 201 status and response contains orderHash.

- [ ] **Step 2: Write TestOrderHandler_Submit_InvalidJSON**

Send a POST with non-JSON body. Verify 400 status.

- [ ] **Step 3: Write TestOrderHandler_Get_NotFound**

Mock service to return nil. Verify 404.

- [ ] **Step 4: Write TestOrderHandler_List_InvalidFilter**

Send GET with invalid `kind=99`. Verify 400 status.

- [ ] **Step 5: Run tests**

```bash
go test ./internal/handler/ -v -count=1
```
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/handler/order_test.go
git commit -m "test: add order handler integration tests"
```

---

### Task 26: Final verification

- [ ] **Step 1: Run full test suite**

```bash
go test ./... -coverprofile=coverage.out -covermode=atomic
go tool cover -func=coverage.out | grep total
```

Expected: total coverage >= 50%.

- [ ] **Step 2: Run vet and build**

```bash
go vet ./...
go build ./...
```
Expected: all pass.

- [ ] **Step 3: Verify all error discards are fixed**

```bash
grep -rn "_ = .*Repo\." internal/ || echo "no matches — good"
grep -rn "_ = .*[Cc]ache\." internal/ || echo "no matches — good"
```

- [ ] **Step 4: Commit**

```bash
git add .
git commit -m "chore: final verification — tests pass, build clean, error discards fixed"
```
