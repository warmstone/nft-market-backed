# NFT Signed Order DEX 后端设计规范

日期: 2026-06-09 | 版本: 1.0

## 1. 概述

后端为 EIP-712 链下签名订单的 NFT 交易所提供订单簿服务。Maker 链下签名订单，提交到后端存储；Taker 通过 API 浏览订单并在链上成交。后端负责订单簿索引、链上事件同步、元数据聚合，不持有私钥、不执行链上交易、不做资金托管。

### 1.1 与合约的分工

| 职责 | 合约 | 后端 |
|---|---|---|
| EIP-712 签名验证 | 执行时验证 | 提交时验证（提前过滤无效订单） |
| 防重放 | cancelledSalt/counter/filled | 同步链上事件更新状态 |
| 撮合结算 | safeTransferFrom + 费用分配 | 不参与 |
| 订单簿检索 | 不提供 | 提供全功能搜索/排序/过滤 |
| 元数据聚合 | 不提供 | 聚合 tokenURI/IPFS/链上名称 |

### 1.2 约束与规模

- 探索期：< 1K DAU，< 500 日成交，< 10K 活跃订单
- 单链起步，chain_id 字段预留多链
- Windows WSL2 单机 Docker Compose 部署

---

## 2. 架构

### 2.1 选择：单体 Go 二进制

```
main.go 启动后注册 4 个 goroutine:
├── HTTP Server (Gin)         — REST + GraphQL + WebSocket
├── Event Watcher             — WS 订阅链上事件 + 轮询兜底
├── Metadata Fetcher          — 异步抓取 tokenURI / IPFS
└── Order Janitor             — 过期订单定时清理
```

各 goroutine 通过共享 `repository/` 和 `ws.Hub` 通信，不直接耦合。

### 2.2 项目结构

```
nft-market-backend/
├── cmd/api/main.go
├── internal/
│   ├── config/config.go
│   ├── domain/                   # 纯数据模型，零依赖
│   │   ├── order.go
│   │   ├── event.go
│   │   └── collection.go
│   ├── handler/
│   │   ├── order.go
│   │   ├── collection.go
│   │   ├── stats.go
│   │   └── graphql.go
│   ├── service/
│   │   ├── order.go              # EIP-712 签名验证、订单校验
│   │   ├── event.go              # 事件消费、状态转换
│   │   └── metadata.go           # tokenURI / IPFS 抓取
│   ├── repository/
│   │   ├── order.go
│   │   ├── event.go
│   │   └── collection.go
│   ├── watcher/watcher.go        # WS 订阅 + 轮询兜底 + reorg
│   ├── rpc/client.go             # ethclient 封装
│   ├── ws/hub.go                 # WebSocket 连接管理 + 广播
│   └── middleware/
│       ├── ratelimit.go
│       └── cors.go
├── migrations/
│   ├── 001_initial.up.sql
│   └── 001_initial.down.sql
├── config/config.yaml
├── Dockerfile
├── docker-compose.yaml
├── go.mod
```

### 2.3 层依赖规则

```
handler → service → repository → database
                ↘ rpc → chain
watcher → repository + rpc + ws.Hub
domain ← 所有层
```

- `domain/` 不依赖任何包
- `handler/` 只做参数绑定、调用 service、返回 JSON
- `service/` 含业务逻辑，依赖 `repository/` 和 `rpc/`
- `watcher/` 写 repository，读 rpc，通过 ws.Hub 推送

### 2.4 部署拓扑

```
docker-compose:
  api (Go binary, port 8080)
  postgres (15+)
  redis (7+)
```

---

## 3. 数据库设计

### 3.1 orders — 活跃订单簿

```sql
CREATE TABLE orders (
    id              BIGSERIAL PRIMARY KEY,
    order_hash      BYTEA NOT NULL UNIQUE,
    chain_id        INTEGER NOT NULL,
    maker           BYTEA NOT NULL,
    taker           BYTEA,
    side            SMALLINT NOT NULL,          -- 0=Sell, 1=Buy
    kind            SMALLINT NOT NULL DEFAULT 0,
    asset_type      SMALLINT NOT NULL DEFAULT 0,
    collection      BYTEA NOT NULL,
    token_id        NUMERIC(78,0) NOT NULL,
    amount          NUMERIC(78,0) NOT NULL DEFAULT 1,
    payment_token   BYTEA,
    price           NUMERIC(78,0) NOT NULL,
    start_price     NUMERIC(78,0) NOT NULL,
    start_time      BIGINT NOT NULL DEFAULT 0,
    end_time        BIGINT NOT NULL DEFAULT 0,
    salt            NUMERIC(78,0) NOT NULL,
    counter         NUMERIC(78,0) NOT NULL DEFAULT 0,
    extra           BYTEA,
    signature_r     BYTEA NOT NULL,
    signature_s     BYTEA NOT NULL,
    signature_v     SMALLINT NOT NULL,
    status          SMALLINT NOT NULL DEFAULT 0, -- 0=active,1=filled,2=cancelled,3=expired
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expired_at      TIMESTAMPTZ
);

-- 所有查询索引均为部分索引，仅覆盖活跃订单
CREATE INDEX idx_orders_active_collection_price ON orders (status, collection, price ASC)
    WHERE status = 0;
CREATE INDEX idx_orders_active_maker ON orders (status, maker) WHERE status = 0;
CREATE INDEX idx_orders_active_collection_token ON orders (status, collection, token_id) WHERE status = 0;
CREATE INDEX idx_orders_active_side ON orders (status, side) WHERE status = 0;
CREATE INDEX idx_orders_active_payment ON orders (status, payment_token) WHERE status = 0;

-- 提交防重：同一 maker 不能复用 salt
CREATE UNIQUE INDEX idx_orders_maker_salt_active ON orders (maker, salt) WHERE status = 0;

-- counter 快速查询（校验提交的 counter >= 当前最大值）
CREATE INDEX idx_orders_maker_counter ON orders (maker, counter DESC) WHERE status = 0;
```

### 3.2 events — 链上事件记账

```sql
CREATE TABLE events (
    id              BIGSERIAL PRIMARY KEY,
    block_number    BIGINT NOT NULL,
    tx_hash         BYTEA NOT NULL,
    tx_index        INTEGER NOT NULL,
    log_index       INTEGER NOT NULL,
    event_name      TEXT NOT NULL,
    event_data      JSONB NOT NULL,
    removed         BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_events_unique ON events (block_number, tx_index, log_index);
```

### 3.3 collections — Collection 元数据

```sql
CREATE TABLE collections (
    address     BYTEA PRIMARY KEY,
    chain_id    INTEGER NOT NULL,
    name        TEXT,
    symbol      TEXT,
    image_url   TEXT,
    metadata    JSONB,
    synced_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### 3.4 nft_metadata — NFT 元数据

```sql
CREATE TABLE nft_metadata (
    collection      BYTEA NOT NULL,
    token_id        NUMERIC(78,0) NOT NULL,
    name            TEXT,
    description     TEXT,
    image_url       TEXT,
    attributes      JSONB,
    synced_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (collection, token_id)
);
```

### 3.5 sync_cursor — 事件同步游标

```sql
CREATE TABLE sync_cursor (
    chain_id            INTEGER PRIMARY KEY,
    last_synced_block   BIGINT NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### 3.6 设计决策

- **不要外键、不要触发器** — 应用层维护一致性
- **部分索引** (`WHERE status = 0`) — 活跃订单 < 10K，索引体积极小
- **唯一约束防重** — `maker+salt` 唯一索引兜底，应用层也做校验
- **events 幂等** — `(block_number, tx_index, log_index)` 唯一，`ON CONFLICT DO NOTHING`

---

## 4. 事件同步

### 4.1 监听的事件

| 事件 | 来源合约 | 处理 |
|---|---|---|
| `OrderFulfilled` | Exchange | `UPDATE orders SET status=1, updated_at=NOW() WHERE order_hash=? AND status=0` |
| `OrderCancelled` | NonceManager | `UPDATE orders SET status=2 WHERE maker=? AND salt=? AND status=0` |
| `CounterIncremented` | NonceManager | `UPDATE orders SET status=2 WHERE maker=? AND counter < ? AND status=0` |
| `CollectionUpdated` | CollectionManager | UPSERT collections；blocked 时下架相关订单 |
| `ProtocolFeeUpdated` | ProtocolManager | 更新 Redis 缓存 `config:fee` |
| `PaymentTokenUpdated` | ProtocolManager | 更新 Redis 缓存 `config:tokens` |
| `RoyaltySet` | RoyaltyManager | 更新 Redis 缓存 `royalty:<collection>` |

### 4.2 同步策略：WS 订阅 + 轮询兜底

```
启动:
  1. 读 sync_cursor.last_synced_block
  2. 若 = 0（首次），从 latest - 100 开始（覆盖漏块）
  3. 启动 WS 订阅 eth_subscribe("logs", filter)
  4. 启动兜底定时器

正常: WS → 收到 log → 入队 → 等 6 块确认 → 写 events → 更新 orders → ws.Hub 推送

兜底触发 (任一):
  - WS 断线超过 10s
  - 超过 60s 未收到新事件
  → eth_getLogs(lastSynced+1, latest-6) 批量拉取

reorg:
  - log.removed == true → 标记 event.removed = true
  - 回退 sync_cursor 到 reorg 块 - 12
  - 重新同步该区间
```

### 4.3 幂等性

```go
// 写入事件
INSERT INTO events (...) VALUES (...)
ON CONFLICT (block_number, tx_index, log_index) DO NOTHING

// 更新订单（带前置条件，防重复处理）
UPDATE orders SET status = 1, updated_at = NOW()
WHERE order_hash = $1 AND status = 0
// rowsAffected == 0 → 已处理，跳过
```

### 4.4 代码结构

```go
type Watcher struct {
    rpc        *rpc.Client
    eventRepo  *repository.EventRepo
    orderRepo  *repository.OrderRepo
    hub        *ws.Hub
    cursor     int64   // 内存 + DB
}

func (w *Watcher) Run(ctx context.Context) {
    go w.subscribeLoop(ctx)   // WS 主通道
    go w.pollLoop(ctx)        // 30s 兜底
    go w.processLoop(ctx)     // 6 块确认 → 处理
}
```

---

## 5. API 设计

### 5.1 REST 接口

**订单**

```
POST   /api/v1/orders           提交签名订单
GET    /api/v1/orders            订单列表 (分页+排序+过滤)
GET    /api/v1/orders/:hash      订单详情
GET    /api/v1/orders/best       最优报价

过滤参数: collection, side, kind, maker, paymentToken, tokenId, status,
          minPrice, maxPrice, priceRange
排序: price_asc, price_desc, created_at_desc, end_time_asc
分页: page (default 1), pageSize (default 20, max 50)
```

**Collection**

```
GET    /api/v1/collections             列表
GET    /api/v1/collections/:address    详情 (名称、地板价、挂单数)
```

**用户**

```
GET    /api/v1/users/:address/orders     活跃挂单
GET    /api/v1/users/:address/activity   交易历史
```

**统计**

```
GET    /api/v1/stats              全局
GET    /api/v1/stats/:collection  单 collection
```

### 5.2 POST /orders 校验流程

```
1. EIP-712 签名恢复 → recoveredSigner == order.maker
2. maker ≠ address(0)
3. taker == address(0) 或 有效地址
4. side ∈ {0, 1}, kind ∈ {0..4}, assetType ∈ {0, 1}
5. collection 在白名单 (若 CollectionManager 启用)
6. paymentToken 在白名单 (若 ProtocolManager 启用)
7. amount ≥ 1 (ERC721 必须 == 1)
8. price > 0 (FixedPrice)
9. startTime ∈ [now-5min, now+30days]
10. endTime == 0 或 endTime > now
11. maker.salt 未在活跃订单中重复 (DB 唯一约束兜底)
12. maker.counter >= 该 maker 活跃订单中最大 counter
13. 签名 (r,s,v) 格式合法, s 值在低半区 (防 ECDSA 可锻造性)

荷兰拍额外:
  startPrice > price
  endTime > startTime
```

### 5.3 GraphQL — 复杂查询

端点：`POST /api/v1/graphql`

覆盖场景：collection + 订单 + token 嵌套查询、用户仪表盘聚合。REST 负责高频 CRUD，GraphQL 负责嵌套/聚合，不重合。

### 5.4 WebSocket

```
GET /ws/orders?collections=0x...,0x...
```

客户端订阅指定 collection。推送事件：

```json
{"type":"order:new",      "payload":{...完整 order 对象...}}
{"type":"order:filled",   "payload":{"orderHash":"0x...","txHash":"0x...","finalPrice":"..."}}
{"type":"order:cancelled","payload":{"maker":"0x...","salt":"..."}}
{"type":"order:expired",  "payload":{"orderHash":"0x..."}}
```

### 5.5 错误格式

```json
{
    "error": "ORDER_SIGNATURE_INVALID",
    "message": "ECDSA recovery does not match maker"
}
```

错误码与合约 error 对齐：`ORDER_EXPIRED`、`ORDER_COUNTER_TOO_LOW`、`COLLECTION_BLOCKED`、`UNSUPPORTED_PAYMENT_TOKEN`、`ORDER_ALREADY_FILLED`。

### 5.6 响应格式

```json
{
    "orderHash": "0x...",
    "maker": "0x...",
    "taker": "0x...",
    "side": 0,
    "kind": 0,
    "assetType": 0,
    "collection": {"address": "0x...", "name": "Bored Ape", "symbol": "BAYC"},
    "tokenId": "123",
    "price": "1000000000000000000",
    "paymentToken": "0x...",
    "startTime": 1700000000,
    "endTime": 1700086400,
    "salt": "12345",
    "counter": "5",
    "status": 0,
    "nft": {"name": "#123", "imageUrl": "https://...", "attributes": [...]},
    "currentPrice": "1000000000000000000",
    "createdAt": "2026-06-09T10:00:00Z"
}
```

订单列表返回同结构但 nft 嵌套只含 imageUrl，详情返回完整 nft 对象。

---

## 6. EIP-712 签名校验

### 6.1 TypeHash 对齐

合约 `ORDER_TYPEHASH`:
```solidity
keccak256("Order(address maker,address taker,uint8 side,uint8 kind,uint8 assetType,address collection,uint256 tokenId,uint256 amount,address paymentToken,uint128 price,uint128 startPrice,uint64 startTime,uint64 endTime,uint256 salt,uint256 counter,bytes32 extra)")
```

Go 端使用 `go-ethereum/signer/core/apitypes`，类型名必须与合约完全一致 —— `uint128` 和 `uint64` 是独立类型名，不能用 `uint256` 替代。

### 6.2 Domain 参数

```go
domain := apitypes.TypedDataDomain{
    Name:              "NFTMarketExchange",
    Version:           "1",
    ChainId:           math.NewHexOrDecimal256(chainId),
    VerifyingContract: exchangeAddress,
}
```

### 6.3 ECDSA 验签

```go
// 1. EIP-712 typed data hash
typedData := apitypes.TypedData{Types: OrderTypes, Domain: domain, Message: msg}
hash, _, _ := apitypes.TypedDataAndHash(typedData)

// 2. ECDSA recover
sig := make([]byte, 65)
sig[0] = order.SignatureR... // ...etc
pubKey, _ := crypto.Ecrecover(hash, sig)
recoveredAddr := crypto.PubkeyToAddress(pubKey)

// 3. 比对
if recoveredAddr != order.Maker {
    return ErrInvalidSignature
}
```

---

## 7. 元数据抓取

### 7.1 触发时机

- 新订单提交时，如果 `nft_metadata` 表中不存在该 (collection, tokenId)，加入抓取队列
- 新 collection 首次出现时，抓取 collection 元数据

### 7.2 抓取流程

```
tokenURI = erc721.tokenURI(tokenId)
  → 如果是 ipfs:// → 走 IPFS 网关 (ipfs.io 或本地节点)
  → 如果是 https:// → 直接 GET
  → 解析 JSON → 写入 nft_metadata 表
```

### 7.3 容错

- 抓取超时 5s，失败重试最多 3 次（指数退避）
- 抓取失败时，nft_metadata 不写入，API 返回时 nft 字段为 null
- 不阻塞订单提交

---

## 8. 缓存策略

| 数据 | 介质 | TTL |
|---|---|---|
| Collection 元数据 | Redis | 1h |
| 热门 collection 订单列表 (前 50) | Redis | 30s |
| NFT 元数据 | Redis | 1h |
| 合约配置 (费率/代币白名单) | Redis | 5min |

**缓存更新**：订单状态变更时 (new/filled/cancelled) 失效对应 collection 的订单列表缓存。合约配置由事件同步更新。

---

## 9. 定时任务

| 任务 | 频率 | 动作 |
|---|---|---|
| 过期订单清理 | 5min | `UPDATE orders SET status=3 WHERE status=0 AND expired_at <= NOW()` |
| 元数据刷新 | 24h | 重抓 `synced_at` > 24h 的 collection 和 nft_metadata |
| 内存 counter 同步 | 30s | 从 DB 刷新活跃 maker 的最大 counter 到内存 map |

---

## 10. 技术选型

| 组件 | 选型 |
|---|---|
| 语言 | Go 1.22+ |
| HTTP 框架 | Gin |
| WebSocket | gorilla/websocket |
| GraphQL | gqlgen |
| 数据库 | PostgreSQL 15+ |
| 缓存 | Redis 7+ |
| 链上 RPC | go-ethereum (ethclient) |
| 数据库迁移 | golang-migrate |
| 配置 | Viper (yaml) |

---

## 11. 配置

```yaml
server:
  port: 8080

database:
  host: postgres
  port: 5432
  name: nft_market
  user: app
  password: ${DB_PASSWORD}

redis:
  addr: redis:6379

ethereum:
  rpc_url: ${ETH_RPC_URL}
  ws_url: ${ETH_WS_URL}
  chain_id: 1
  exchange_address: "0x..."
  protocol_manager_address: "0x..."
  collection_manager_address: "0x..."
  royalty_manager_address: "0x..."
  confirmation_blocks: 6
```

---

## 12. Docker Compose

```yaml
services:
  api:
    build: .
    ports: ["8080:8080"]
    depends_on: [postgres, redis]
    environment:
      DB_PASSWORD: ${DB_PASSWORD}
      ETH_RPC_URL: ${ETH_RPC_URL}
      ETH_WS_URL: ${ETH_WS_URL}

  postgres:
    image: postgres:15
    volumes: ["pgdata:/var/lib/postgresql/data"]
    environment:
      POSTGRES_DB: nft_market
      POSTGRES_USER: app
      POSTGRES_PASSWORD: ${DB_PASSWORD}

  redis:
    image: redis:7

volumes:
  pgdata:
```

---

## 13. 演进预留

- `chain_id` 字段已在 orders 表，多链时 Watcher 按 chain 独立 sync_cursor
- Watcher 内部通过 `interface` 与 repository/ws 解耦，未来可拆为独立 binary
- REST + GraphQL 双通道，新查询类型加 GraphQL field 不影响现有 REST 接口
- 部分索引策略在活跃订单 < 500K 时不需调整
