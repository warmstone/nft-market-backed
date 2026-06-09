package domain

import (
	"encoding/json"
	"math/big"
	"time"
)

// Collection represents a registered NFT collection tracked by the backend.
type Collection struct {
	Address  string          `json:"address"`
	ChainID  int64           `json:"chainId"`
	Name     string          `json:"name"`
	Symbol   string          `json:"symbol"`
	ImageURL string          `json:"imageUrl"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
	SyncedAt time.Time       `json:"syncedAt"`
}

// NFTMetadata stores off-chain metadata for an individual NFT token.
type NFTMetadata struct {
	Collection  string          `json:"collection"`
	TokenID     *BigInt         `json:"tokenId"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	ImageURL    string          `json:"imageUrl"`
	Attributes  json.RawMessage `json:"attributes,omitempty"`
	SyncedAt    time.Time       `json:"syncedAt"`
}

// AssetDetail is the marketplace-facing view of a single NFT.
type AssetDetail struct {
	Collection *Collection  `json:"collection"`
	Metadata   *NFTMetadata `json:"metadata"`
	TokenID    *BigInt      `json:"tokenId"`
	Listings   []Order      `json:"listings"`
	Offers     []Order      `json:"offers"`
	Activity   []Order      `json:"activity"`
}

// CollectionDetail extends Collection with market-level aggregates.
type CollectionDetail struct {
	Collection
	FloorPrice *big.Int `json:"floorPrice"`
	BestBid    *big.Int `json:"bestBid"`
	Listed     int64    `json:"listed"`
}

// GlobalStats holds aggregate platform-level statistics.
type GlobalStats struct {
	TotalOrders      int64 `json:"totalOrders"`
	TotalCollections int64 `json:"totalCollections"`
	TotalTraders     int64 `json:"totalTraders"`
}
