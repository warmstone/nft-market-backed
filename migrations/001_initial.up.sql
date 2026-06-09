CREATE TABLE sync_cursor (
    chain_id            INTEGER PRIMARY KEY,
    last_synced_block   BIGINT NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE collections (
    address     BYTEA PRIMARY KEY,
    chain_id    INTEGER NOT NULL,
    name        TEXT,
    symbol      TEXT,
    image_url   TEXT,
    metadata    JSONB,
    synced_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

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

CREATE TABLE orders (
    id              BIGSERIAL PRIMARY KEY,
    order_hash      BYTEA NOT NULL UNIQUE,
    chain_id        INTEGER NOT NULL,
    maker           BYTEA NOT NULL,
    taker           BYTEA,
    side            SMALLINT NOT NULL,
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
    status          SMALLINT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expired_at      TIMESTAMPTZ
);

-- Query indexes (partial - only active orders)
CREATE INDEX idx_orders_active_collection_price ON orders (status, collection, price ASC) WHERE status = 0;
CREATE INDEX idx_orders_active_maker ON orders (status, maker) WHERE status = 0;
CREATE INDEX idx_orders_active_collection_token ON orders (status, collection, token_id) WHERE status = 0;
CREATE INDEX idx_orders_active_side ON orders (status, side) WHERE status = 0;
CREATE INDEX idx_orders_active_payment ON orders (status, payment_token) WHERE status = 0;

-- Dedup: same maker cannot reuse salt while order is active
CREATE UNIQUE INDEX idx_orders_maker_salt_active ON orders (maker, salt) WHERE status = 0;

-- Counter validation query
CREATE INDEX idx_orders_maker_counter ON orders (maker, counter DESC) WHERE status = 0;

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
