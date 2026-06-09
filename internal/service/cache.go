package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// CacheService wraps go-redis and provides typed get/set/invalidate helpers.
type CacheService struct {
	rdb *redis.Client
}

// NewCacheService creates a CacheService connected to the given Redis address.
func NewCacheService(addr string) (*CacheService, error) {
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &CacheService{rdb: rdb}, nil
}

// Client returns the underlying Redis client.
func (c *CacheService) Client() *redis.Client {
	return c.rdb
}

// Get unmarshals a cached value into dest. Returns redis.Nil if key doesn't exist.
func (c *CacheService) Get(ctx context.Context, key string, dest interface{}) error {
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// Set marshals value to JSON and caches it with the given TTL.
func (c *CacheService) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cache marshal: %w", err)
	}
	return c.rdb.Set(ctx, key, data, ttl).Err()
}

// Del removes one or more keys.
func (c *CacheService) Del(ctx context.Context, keys ...string) error {
	return c.rdb.Del(ctx, keys...).Err()
}

// InvalidateOrders drops the cached order list for a given collection.
func (c *CacheService) InvalidateOrders(ctx context.Context, collection string) error {
	return c.rdb.Del(ctx, "orders:"+collection).Err()
}

// OrderListKey returns the Redis key for a collection's cached order list.
func OrderListKey(collection string) string { return "orders:" + collection }

// CollectionKey returns the Redis key for a collection's metadata cache.
func CollectionKey(address string) string { return "collection:" + address }

// NFTKey returns the Redis key for an NFT's metadata cache.
func NFTKey(collection string, tokenID string) string {
	return "nft:" + collection + ":" + tokenID
}
