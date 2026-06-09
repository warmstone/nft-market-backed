# NFT Market Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go backend for the NFT Signed Order DEX that stores signed orders, syncs on-chain events, aggregates metadata, and serves REST/GraphQL/WebSocket APIs.

**Architecture:** Single Go binary with 4 goroutines (HTTP server, event watcher, metadata fetcher, order janitor). PostgreSQL for persistence, Redis for caching. Internal packages separated by layer: domain → repository → service → handler.

**Tech Stack:** Go 1.22+, Gin, gorilla/websocket, gqlgen, PostgreSQL 15+, Redis 7+, go-ethereum, golang-migrate, Viper

**Spec:** `docs/superpowers/specs/2026-06-09-nft-market-backend-design.md`

---

## File Map

```
nft-market-backend/
├── cmd/api/main.go              # Entry point, wire all components, start goroutines
├── internal/
│   ├── config/config.go         # Viper → Config struct
│   ├── domain/
│   │   ├── order.go             # Order struct, enums (OrderSide/Kind/AssetType/Status), OrderFilter, OrderResponse, currentPrice()
│   │   ├── event.go             # ContractEvent struct, event type constants, per-event data structs
│   │   └── collection.go        # Collection, NFTMetadata, CollectionDetail, GlobalStats structs
│   ├── handler/
│   │   ├── order.go             # POST/GET /orders, GET /orders/:hash, GET /orders/best
│   │   ├── collection.go        # GET /collections, GET /collections/:address, GET /stats
│   │   ├── graphql.go           # POST /graphql (gqlgen stub for v1)
│   │   └── ws.go                # GET /ws/orders upgrade
│   ├── service/
│   │   ├── cache.go             # Redis get/set/del/invalidate, key pattern: orders:{collection}
│   │   ├── signature.go         # EIP-712 typed data hash + ECDSA recover, verify against maker
│   │   ├── order.go             # Submit (validate 13 rules + persist), Find, GetByHash, GetBest, formatResponse
│   │   ├── event.go             # Handle dispatch: OrderFulfilled/Cancelled/CounterIncremented/CollectionUpdated
│   │   ├── metadata.go          # tokenURI RPC call → IPFS/HTTPS fetch → parse JSON → upsert nft_metadata
│   │   └── scheduler.go         # 5-min expire orders, 24h metadata refresh
│   ├── repository/
│   │   ├── order.go             # CRUD + filtered Find + UpdateStatus + CancelByMakerSalt + CancelByMakerCounter + ExpireOrders
│   │   ├── event.go             # InsertEvent (ON CONFLICT DO NOTHING) + EventExists + GetLastSyncedBlock + UpdateLastSyncedBlock
│   │   └── collection.go        # Upsert collection + nft_metadata, FindAll, GetStale
│   ├── watcher/watcher.go       # subscribeLoop (WS) + pollLoop (30s fallback) + processLoop (6-block confirm)
│   ├── rpc/client.go            # ethclient wrapper: BlockNumber, FilterLogs, SubscribeLogs, CallContract
│   ├── ws/hub.go                # Client pool, per-collection broadcast, Upgrade gorilla conn
│   └── middleware/
│       ├── ratelimit.go         # Token bucket per IP, GC stale entries
│       └── cors.go              # Allow all origins for v1
├── migrations/
│   ├── 001_initial.up.sql       # tables: orders, events, collections, nft_metadata, sync_cursor + indexes
│   └── 001_initial.down.sql     # DROP all tables
├── config/config.yaml
├── Dockerfile                    # Multi-stage: go build → alpine
├── docker-compose.yaml           # api + postgres + redis
└── .env.example
```

---

## Tasks

### Task 1: Project Scaffold & Dependencies

**Files:** `go.mod`, directory tree

- [ ] `go mod init nft-market-backend`
- [ ] Create all directories under `cmd/` and `internal/`
- [ ] Install deps: `gin`, `gorilla/websocket`, `go-ethereum`, `lib/pq`, `golang-migrate`, `viper`, `go-redis/v9`, `gqlgen`

**Verify:** `go build ./cmd/api` compiles (with placeholder main.go).

---

### Task 2: Domain Models

**Files:** `internal/domain/order.go`, `event.go`, `collection.go`

- [ ] Order struct with all 17 EIP-712 fields + status/signature/timestamps, enums (OrderSide, OrderKind, AssetType, OrderStatus), OrderFilter, OrderResponse (JSON-friendly), SubmitOrderRequest, ErrorResponse, `currentPrice()` method for Dutch auction
- [ ] ContractEvent struct + event type constants (OrderFulfilled, OrderCancelled, CounterIncremented, CollectionUpdated, etc.) + per-event data structs
- [ ] Collection, NFTMetadata, CollectionDetail, GlobalStats structs

**Key interfaces:**
```go
type Order struct { ID int64; OrderHash string; Maker string; ... }
func (o *Order) CurrentPrice() *big.Int  // FixedPrice = price; DutchAuction = linear decay
func (o *Order) IsExpired() bool
```

**Verify:** `go test ./internal/domain/` — test `CurrentPrice` for fixed price and Dutch auction midpoint.

---

### Task 3: Config

**Files:** `internal/config/config.go`, `config/config.yaml`

- [ ] Config struct (Server, Database, Redis, Ethereum sections)
- [ ] `Load(path string) (*Config, error)` — Viper reads yaml, env var override
- [ ] Config yaml with all fields from spec section 11
- [ ] `DatabaseConfig.DSN()` helper

**Verify:** `go build ./internal/config/`

---

### Task 4: Database Migration

**Files:** `migrations/001_initial.up.sql`, `001_initial.down.sql`

- [ ] Create tables: `sync_cursor`, `collections`, `nft_metadata`, `orders` (all 17 order fields + signature + status + timestamps), `events`
- [ ] Create all indexes from spec section 3 (partial indexes `WHERE status = 0`, unique `(block_number, tx_index, log_index)`, unique `(maker, salt) WHERE status = 0`)

**Verify:** SQL syntax review. Migration runs via `golang-migrate` wired in Task 21 (main.go).

---

### Task 5: RPC Client

**Files:** `internal/rpc/client.go`

- [ ] `Client` wraps `ethclient.Client`
- [ ] `NewClient(httpURL string, chainID int64)` — dial RPC
- [ ] `SetContractAddresses(exchange, protocolManager, collectionManager, royaltyManager common.Address)` — set filter addresses
- [ ] `BlockNumber(ctx) (uint64, error)`
- [ ] `FilterLogs(ctx, fromBlock, toBlock uint64) ([]types.Log, error)` — `eth_getLogs` for registered addresses
- [ ] `SubscribeLogs(ctx, ch chan<- types.Log) (ethereum.Subscription, error)` — `eth_subscribe`
- [ ] `CallContract(ctx, to, data) ([]byte, error)` — `eth_call` for tokenURI/name/symbol
- [ ] `Close()`

**Verify:** `go build ./internal/rpc/`

---

### Task 6: Repository Layer

**Files:** `internal/repository/order.go`, `event.go`, `collection.go`

- [ ] **OrderRepo**: `Insert(o *Order) error`, `Find(filter OrderFilter) ([]Order, int64, error)`, `FindByHash(hash string) (*Order, error)`, `UpdateStatus(hash, status) error`, `CancelByMakerSalt(maker, salt) error`, `CancelByMakerCounter(maker, minCounter) error`, `ExpireOrders() (int64, error)`, `CancelByCollection(collection) error`, `GetActiveMakerCount() (int64, error)`, `GetBestPrice(collection, side) (*big.Int, error)`, `GetListedCount(collection) (int64, error)`
- [ ] **EventRepo**: `InsertEvent(e) error` (ON CONFLICT DO NOTHING), `EventExists(block, txIdx, logIdx) (bool, error)`, `MarkRemoved(block, txIdx, logIdx) error`, `GetLastSyncedBlock(chainID) (uint64, error)`, `UpdateLastSyncedBlock(chainID, block) error`, `GetUserActivity(maker, limit) ([]ContractEvent, error)`, `DeleteEventsAboveBlock(block) error`
- [ ] **CollectionRepo**: `Upsert(c) error`, `FindAll(search, page, pageSize) ([]Collection, int64, error)`, `FindByAddress(addr) (*Collection, error)`, `UpsertNFTMetadata(m) error`, `GetNFTMetadata(collection, tokenID) (*NFTMetadata, error)`, `GetStaleMetadata(limit) ([]NFTMetadata, error)`, `GetStaleCollections(limit) ([]Collection, error)`
- [ ] Helper: `scanOrders(rows)` — decode BYTEA fields to hex strings, NUMERIC to *big.Int

**Key detail:** BYTEA ↔ hex string conversion at repo boundary. All addresses stored as BYTEA, converted to "0x..." strings for domain layer.

**Verify:** `go build ./internal/repository/`

---

### Task 7: WebSocket Hub

**Files:** `internal/ws/hub.go`

- [ ] `Hub` struct with `clients map[*Client]bool`, `byCollection map[string]map[*Client]bool`, register/unregister channels
- [ ] `Run()` — select loop processing register/unregister
- [ ] `Upgrade(w, r, collections []string)` — gorilla upgrader, create Client, register, start readPump + writePump
- [ ] `Broadcast(collection string, msg Message)` — JSON marshal, send to clients subscribed to that collection
- [ ] `Message` struct: `Type string`, `Payload json.RawMessage`

**Verify:** `go build ./internal/ws/`

---

### Task 8: Middleware

**Files:** `internal/middleware/cors.go`, `ratelimit.go`

- [ ] CORS: allow all origins, methods GET/POST/OPTIONS, headers Content-Type/Authorization
- [ ] RateLimit: token bucket per client IP, configurable rate/burst, GC stale entries every 5min

**Verify:** `go build ./internal/middleware/`

---

### Task 9: Cache Layer

**Files:** `internal/service/cache.go`

- [ ] `CacheService` wraps `go-redis`
- [ ] `Get(ctx, key, &dest) error`, `Set(ctx, key, value, ttl) error`, `Del(ctx, keys...) error`
- [ ] `InvalidateOrders(ctx, collection)` — delete `orders:{collection}` key
- [ ] Key conventions: `orders:{collection}` (list cache), `collection:{address}` (metadata), `nft:{collection}:{tokenId}`, `config:fee`, `config:tokens`

**Verify:** `go build ./internal/service/`

---

### Task 10: EIP-712 Signature Verification

**Files:** `internal/service/signature.go`

- [ ] `OrderTypes` — apitypes.Types matching the contract ORDER_TYPEHASH exactly (uint128 for price/startPrice, uint64 for startTime/endTime, bytes32 for extra)
- [ ] `SignatureService` with chainID + verifyingContract
- [ ] `Verify(order *Order, signatureHex string) error` — decode sig, check low-s, EIP-712 typed data hash, ECDSA recover, compare with maker
- [ ] Unit test: valid sig passes, invalid sig fails, low-s enforcement

**Domain:** `apitypes.TypedDataDomain{Name: "NFTMarketExchange", Version: "1"}`
**TypeHash:** must match contract's `keccak256("Order(address maker,address taker,uint8 side,uint8 kind,uint8 assetType,address collection,uint256 tokenId,uint256 amount,address paymentToken,uint128 price,uint128 startPrice,uint64 startTime,uint64 endTime,uint256 salt,uint256 counter,bytes32 extra)")`

**Verify:** `go test ./internal/service/ -run Signature -v`

---

### Task 11: Order Service

**Files:** `internal/service/order.go`

- [ ] `OrderService` — deps: OrderRepo, CollectionRepo, SignatureService, chainID
- [ ] `Submit(req *SubmitOrderRequest) (*Order, error)` — 13 validation rules from spec 5.2, compute orderHash, persist
- [ ] `Find(filter OrderFilter) (*OrderListResponse, error)` — query + paginate
- [ ] `GetByHash(hash) (*OrderResponse, error)` — single order + collection brief + NFT brief
- [ ] `GetBest(collection, side) (*OrderResponse, error)` — top 1 by price
- [ ] `GetUserOrders(maker, status) ([]OrderResponse, error)`
- [ ] `formatResponse(o *Order, detail bool) OrderResponse` — enriches with CollectionBrief and NFTBrief

**Validation order:** signature → maker → taker → side/kind/assetType → collection → amount → price → time window → Dutch auction constraints → counter → salt uniqueness

**Verify:** `go build ./internal/service/`

---

### Task 12: Event Service

**Files:** `internal/service/event.go`

- [ ] `EventService` — deps: OrderRepo, CollectionRepo, CacheService, Hub
- [ ] `Handle(event *ContractEvent) error` — switch on EventName, dispatch to handler
- [ ] OrderFulfilled → `UpdateStatus(orderHash, filled)` → Hub.Broadcast `order:filled`
- [ ] OrderCancelled → `CancelByMakerSalt(maker, salt)` → Hub.Broadcast `order:cancelled`
- [ ] CounterIncremented → `CancelByMakerCounter(maker, newCounter)` → Hub.Broadcast
- [ ] CollectionUpdated → if blocked: `CancelByCollection(collection)` + Hub.Broadcast `collection:updated`
- [ ] Config events → CacheService.Del `config:*`

**Verify:** `go build ./internal/service/`

---

### Task 13: Metadata Service

**Files:** `internal/service/metadata.go`

- [ ] `MetadataService` — deps: CollectionRepo, RPC Client, HTTP client (5s timeout)
- [ ] `Enqueue(collection, tokenID)` — non-blocking queue push
- [ ] `Run(ctx)` — 3 workers processing the queue
- [ ] Worker: check cache → `tokenURI(tokenId)` via RPC → replace ipfs:// with gateway → HTTP GET → parse JSON → `UpsertNFTMetadata`
- [ ] Retry: 3 attempts, exponential backoff (1s, 2s, 4s)
- [ ] `FetchCollection(ctx, address)` — `name()` + `symbol()` via RPC → Upsert
- [ ] `RefreshStale(ctx)` — query stale collections + NFTs, re-fetch

**RPC calls:** `tokenURI(uint256)` selector `0xc87b56dd`, `name()` selector `0x06fdde03`, `symbol()` selector `0x95d89b41`

**Verify:** `go build ./internal/service/`

---

### Task 14: Event Watcher

**Files:** `internal/watcher/watcher.go`

- [ ] `Watcher` struct — deps: RPC Client, EventRepo, EventService, chainID, confirmationBlocks
- [ ] `Run(ctx)` — init cursor (0 → latest-100), start subscribeLoop + pollLoop + processLoop
- [ ] `subscribeLoop` — WS subscribe, reconnect on error, queue received logs
- [ ] `pollLoop` — every 30s, if no activity > 60s → `eth_getLogs(lastSynced+1, latest-6)`
- [ ] `processLoop` — wait 6-block confirm, check idempotent, parse event via ABI, insert event, dispatch to EventService, update cursor
- [ ] Reorg: if `log.Removed` → MarkRemoved + rollback cursor to log.BlockNumber - 12
- [ ] `parseEvent(vLog)` — match topic0 against known event signature hashes, ABI decode data

**Event topic hashes** (computed at startup or hardcoded from contract ABI):
- OrderFulfilled, OrderCancelled, CounterIncremented (from Exchange/NonceManager)
- CollectionUpdated (from CollectionManager)
- ProtocolFeeUpdated, PaymentTokenUpdated (from ProtocolManager)

**Verify:** `go build ./internal/watcher/`

---

### Task 15: Scheduled Tasks

**Files:** `internal/service/scheduler.go`

- [ ] `Scheduler` — deps: OrderRepo, CollectionRepo, MetadataService
- [ ] `Run(ctx)` — start expireOrdersLoop + metadataRefreshLoop
- [ ] `expireOrdersLoop` — every 5 min: `UPDATE orders SET status=3 WHERE status=0 AND expired_at <= NOW()`
- [ ] `metadataRefreshLoop` — every 24h: `RefreshStale()`

**Verify:** `go build ./internal/service/`

---

### Task 16: REST Handlers — Orders

**Files:** `internal/handler/order.go`

- [ ] `OrderHandler` — deps: OrderService, MetadataService
- [ ] `POST /api/v1/orders` → `Submit` → 201 `{orderHash, status:"active"}` | 400 error
- [ ] `GET /api/v1/orders` → parse query params (collection, side, kind, maker, paymentToken, minPrice, maxPrice, sortBy, page, pageSize) → `Find` → 200
- [ ] `GET /api/v1/orders/:hash` → `GetByHash` → 200 | 404
- [ ] `GET /api/v1/orders/best?collection=X&side=0` → `GetBest` → 200 | 404
- [ ] `GET /api/v1/users/:address/orders?status=0` → `GetUserOrders` → 200
- [ ] `extractErrorCode(errMsg)` — parse service error prefix for API error code

**Verify:** `go build ./internal/handler/`

---

### Task 17: REST Handlers — Collections & Stats

**Files:** `internal/handler/collection.go`

- [ ] `CollectionHandler` — deps: CollectionRepo, OrderRepo
- [ ] `GET /api/v1/collections?search=&page=&pageSize=` → `FindAll` → 200
- [ ] `GET /api/v1/collections/:address` → `FindByAddress` + enrich floor/bestBid/listed → 200
- [ ] `StatsHandler` — deps: OrderRepo, CollectionRepo
- [ ] `GET /api/v1/stats` → global counts → 200
- [ ] `GET /api/v1/stats/:collection` → floor/bestBid/listed → 200

**Verify:** `go build ./internal/handler/`

---

### Task 18: WebSocket Handler

**Files:** `internal/handler/ws.go`

- [ ] `GET /ws/orders?collections=0x...,0x...` → parse collections, call `hub.Upgrade`
- [ ] Client receives: `order:new`, `order:filled`, `order:cancelled`, `order:expired`, `collection:updated`

**Verify:** `go build ./internal/handler/`

---

### Task 19: GraphQL Handler

**Files:** `internal/handler/graphql.go`

- [ ] `POST /api/v1/graphql` — accept query, return stub for v1 (REST covers all core cases)
- [ ] GraphQL schema and resolvers deferred to post-v1

**Verify:** `go build ./internal/handler/`

---

### Task 20: Main Assembly

**Files:** `cmd/api/main.go`

- [ ] Load config from `config/config.yaml`
- [ ] Open PostgreSQL connection, ping
- [ ] Run migrations via `golang-migrate` (`file://migrations`)
- [ ] Initialize RPC client, set contract addresses
- [ ] Create all repos (order, event, collection)
- [ ] Create cache service, ping Redis
- [ ] Create WebSocket hub, start `go hub.Run()`
- [ ] Create services (signature, order, event, metadata, scheduler)
- [ ] Create watcher
- [ ] Create handlers (order, collection, stats, ws, graphql)
- [ ] Setup Gin router: middleware (recovery, CORS, rate limit), route groups
- [ ] Start goroutines: watcher.Run(ctx), metadataSvc.Run(ctx), scheduler.Run(ctx)
- [ ] Start HTTP server on :8080
- [ ] Graceful shutdown on SIGINT/SIGTERM

**Route table:**
```
POST   /api/v1/orders              → orderH.Submit
GET    /api/v1/orders               → orderH.List
GET    /api/v1/orders/best          → orderH.Best
GET    /api/v1/orders/:hash         → orderH.Get
GET    /api/v1/collections          → collectionH.List
GET    /api/v1/collections/:address → collectionH.Get
GET    /api/v1/users/:address/orders   → orderH.UserOrders
GET    /api/v1/users/:address/activity → (stub: empty list)
GET    /api/v1/stats                → statsH.Global
GET    /api/v1/stats/:collection    → statsH.CollectionStats
POST   /api/v1/graphql              → graphqlH.Handle
GET    /ws/orders                   → wsH.Handle
```

**Verify:** `go build -o bin/api ./cmd/api`

---

### Task 21: Docker Compose & Dockerfile

**Files:** `Dockerfile`, `docker-compose.yaml`, `.env.example`

- [ ] Dockerfile: multi-stage, `golang:1.22-alpine` → `alpine:3.19`, copy binary + config + migrations
- [ ] docker-compose: api (port 8080), postgres:15 (healthcheck, volume), redis:7-alpine
- [ ] `.env.example` with DB_PASSWORD, ETH_RPC_URL, ETH_WS_URL, contract addresses

**Verify:** `docker compose up` starts all 3 services, api connects to postgres.

---

### Task 22: Build & Verify

- [ ] `go build -o bin/api ./cmd/api` — clean compile
- [ ] `go vet ./...` — no issues
- [ ] `go test ./...` — all tests pass
- [ ] `docker compose up -d && docker compose ps` — all services healthy
