# NFT Market Backend — Production Evolution Design

## Overview

本设计说明书描述如何将 nft-market-backend 从 MVP 演进为生产级项目。采取自底向上的推进策略：基础设施 → 安全 → 功能 → 质量。

## Phase 1: Infrastructure

### 1.1 Structured Logging (zap)

Replace `log.Printf` across the entire codebase with `uber-go/zap`.

- New package: `internal/log/log.go` — global logger singleton
- Production config: JSON output, info level by default, configurable via env var `LOG_LEVEL`
- All existing log sites converted to structured calls with typed fields

### 1.2 Health / Readiness Endpoints

Two new endpoints, dependency-injected via closures:

| Endpoint | Purpose | Checks |
|----------|---------|--------|
| `GET /health` | Load balancer liveness | DB ping + Redis ping + RPC block number |
| `GET /ready` | K8s readiness probe | Same checks, independently togglable |

### 1.3 Request ID Middleware

- Generate UUID per request if `X-Request-ID` header absent
- Inject into `gin.Context` and response header
- Available to all downstream logging

### 1.4 Access Log Middleware

- Logs method, path, status, latency, request_id after each request
- Uses zap structured fields for searchability

### 1.5 Automatic Database Migrations

- Use `golang-migrate/migrate` to run `migrations/` at startup
- `migration: enable` in config.yaml, with option to skip via env
- Migration failure = service refuses to start

### 1.6 CI Pipeline (GitHub Actions)

- `.github/workflows/ci.yml`
- Jobs: lint (go vet + golangci-lint), test (go test -cover), build (go build + Docker)
- Triggers: push + pull_request

## Phase 2: Security

### 2.1 Wallet Signature Login (JWT)

Flow:
1. `GET /api/v1/auth/challenge?address=0x...` → returns nonce + challenge message
2. Client signs challenge with wallet (personal_sign)
3. `POST /api/v1/auth/login {address, signature}` → verifies ECDSA recovery → returns JWT

- Challenge format: ERC-4361 simplified ("I am signing in to NFT Market.\nNonce: ...\nIssued At: ...")
- JWT: HS256, 24h expiry, secret from env `JWT_SECRET`
- Nonce stored in Redis with 5-min TTL

### 2.2 Auth Middleware

- Extracts `Authorization: Bearer <token>`, validates JWT
- Injects `address` into context for downstream use
- Unauthenticated requests → 401

Protected routes: `POST /orders`, `GET /users/:address/orders`, `POST /graphql`, `GET /graphql`
Public routes (no auth): `GET /orders`, `GET /collections`, `GET /stats`, `GET /health`, `GET /ready`, `GET /ws/orders`, auth endpoints

### 2.3 CORS Restriction

- Replace `AllowAllOrigins: true` with configurable allowlist in config.yaml
- `server.allowed_origins: ["http://localhost:3000", "https://..."]`

### 2.4 Input Validation Fixes

- Add `http.MaxBytesReader` body size limit middleware (default 1MB)
- Fix `parseOrderFilter` silent Atoi fallback → return 400 on invalid input
- Fix `big.Int.SetString()` missing error checks

## Phase 3: Feature Completion

### 3.1 Full GraphQL Layer (gqlgen)

New package: `internal/graphql/`

Schema: Query (orders, collections, stats), Mutation (submitOrder), Subscription (orderUpdated)

- Resolvers call existing service layer — no logic duplication
- `BigInt` scalar serializes to JSON string (avoids JS number overflow)
- `orderUpdated` subscription bridges WebSocket Hub events to GraphQL channel
- GraphQL Playground served at `/api/v1/graphql`
- Authenticated via JWT (same auth middleware)

### 3.2 OpenAPI Documentation (swag)

- Add swag annotations to every handler function
- `make docs` generates `docs/` directory
- Swagger UI mounted at `/api/v1/docs`
- GraphQL documentation via built-in Playground

## Phase 4: Quality

### 4.1 Tests

| Layer | Approach | Coverage target |
|-------|----------|-----------------|
| domain | Pure Go test (existing) | Maintain |
| repository | testcontainers-go + real PostgreSQL | CRUD, filtering, pagination |
| service | Mock repository + real signature logic | Validation paths, error branches |
| handler | httptest + mock service | Status codes, response format, auth |

Key test scenarios:
- FixedPrice + DutchAuction order submit
- Signature validation failure → ORDER_SIGNATURE_INVALID
- Expired order → ORDER_EXPIRED
- Dutch auction invalid params → INVALID_DUTCH_AUCTION
- Duplicate salt → ORDER_DUPLICATE
- Repository: create, find by hash, filtered find, pagination, status update

### 4.2 Prometheus Metrics

- `GET /metrics` endpoint for Prometheus scraping
- HTTP middleware: requests_total, request_duration_seconds
- Business counters: orders_submitted_total, events_processed_total
- DB histograms: db_queries_duration_seconds by operation

### 4.3 Error Handling Fixes

- Replace `_ = s.cache.InvalidateOrders(...)` and similar silent discards with logged warnings
- Introduce `domain.AppError{Code, Message, Err}` to replace `fmt.Errorf("CODE: %w")` string pattern
- Handler extracts code via `errors.As` instead of `strings.Split`

### 4.4 Additional Cleanups

- Remove root-level compiled binary from repo
- Remove unused gqlgen boilerplate
- Fix `context.Background()` → pass request context through layers where appropriate

## File Change Summary

| Phase | New Files | Modified Files |
|-------|-----------|----------------|
| Phase 1 | `internal/log/log.go`, `internal/middleware/requestid.go`, `internal/middleware/accesslog.go`, `internal/handler/health.go`, `.github/workflows/ci.yml` | `cmd/api/main.go`, all `*.go` (log.Printf → zap) |
| Phase 2 | `internal/handler/auth.go`, `internal/service/auth.go`, `internal/middleware/auth.go`, `internal/domain/auth.go` | `internal/middleware/cors.go`, `internal/handler/order.go`, `cmd/api/main.go`, `config/config.yaml` |
| Phase 3 | `internal/graphql/schema.graphql`, `internal/graphql/resolver.go`, `internal/graphql/*.resolvers.go`, `internal/graphql/scalars.go`, `internal/graphql/subscription.go`, `gqlgen.yml`, `docs/` | `cmd/api/main.go`, `internal/handler/*.go` (swag annotations) |
| Phase 4 | `internal/domain/errors.go`, `internal/repository/*_test.go`, `internal/service/*_test.go`, `internal/handler/*_test.go`, `internal/middleware/metrics.go` | `cmd/api/main.go`, `config/config.yaml`, `go.mod` |

## Verification Gates

After each phase:
1. `go build ./...` succeeds
2. `go vet ./...` passes
3. Any new endpoints return expected responses
4. Phase 4 additionally: `go test ./...` passes with ≥50% coverage
