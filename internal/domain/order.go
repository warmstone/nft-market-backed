package domain

import (
	"encoding/json"
	"math/big"
	"time"
)

// OrderSide indicates whether the order is a listing (Sell) or an offer (Buy).
type OrderSide uint8

const (
	Sell OrderSide = 0
	Buy  OrderSide = 1
)

func (s OrderSide) String() string {
	switch s {
	case Sell:
		return "Sell"
	case Buy:
		return "Buy"
	default:
		return "Unknown"
	}
}

// OrderKind classifies the pricing mechanism of the order.
type OrderKind uint8

const (
	FixedPrice    OrderKind = 0
	DutchAuction  OrderKind = 1
	CollectionBid OrderKind = 2
	TraitBid      OrderKind = 3
	Bundle        OrderKind = 4
)

func (k OrderKind) String() string {
	switch k {
	case FixedPrice:
		return "FixedPrice"
	case DutchAuction:
		return "DutchAuction"
	case CollectionBid:
		return "CollectionBid"
	case TraitBid:
		return "TraitBid"
	case Bundle:
		return "Bundle"
	default:
		return "Unknown"
	}
}

// AssetType distinguishes between ERC721 and ERC1155 token standards.
type AssetType uint8

const (
	ERC721  AssetType = 0
	ERC1155 AssetType = 1
)

func (a AssetType) String() string {
	switch a {
	case ERC721:
		return "ERC721"
	case ERC1155:
		return "ERC1155"
	default:
		return "Unknown"
	}
}

// OrderStatus tracks the lifecycle state of an order on-chain.
type OrderStatus uint8

const (
	Active    OrderStatus = 0
	Filled    OrderStatus = 1
	Cancelled OrderStatus = 2
	Expired   OrderStatus = 3
)

func (os OrderStatus) String() string {
	switch os {
	case Active:
		return "Active"
	case Filled:
		return "Filled"
	case Cancelled:
		return "Cancelled"
	case Expired:
		return "Expired"
	default:
		return "Unknown"
	}
}

// BigInt is a wrapper around *big.Int that marshals to a decimal string in JSON.
type BigInt struct {
	*big.Int
}

func NewBigInt(i *big.Int) *BigInt {
	if i == nil {
		return &BigInt{new(big.Int)}
	}
	return &BigInt{i}
}

func (b *BigInt) MarshalJSON() ([]byte, error) {
	if b.Int == nil {
		return json.Marshal("0")
	}
	return json.Marshal(b.Int.String())
}

func (b *BigInt) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	b.Int = new(big.Int)
	_, ok := b.Int.SetString(s, 10)
	if !ok {
		return &json.UnmarshalTypeError{}
	}
	return nil
}

// Order is the core domain model matching the EIP-712 Order struct from the Solidity contract.
// All 17 canonical fields are preserved exactly plus server-side metadata.
type Order struct {
	// EIP-712 canonical fields
	Maker        string   `json:"maker"`
	Taker        string   `json:"taker"`        // address(0) = public order
	Side         OrderSide `json:"side"`        // 0=Sell, 1=Buy
	Kind         OrderKind `json:"kind"`        // 0=FixedPrice, 1=DutchAuction, 2=CollectionBid, 3=TraitBid, 4=Bundle
	AssetType    AssetType `json:"assetType"`   // 0=ERC721, 1=ERC1155
	Collection   string   `json:"collection"`
	TokenID      *BigInt  `json:"tokenId"`
	Amount       *BigInt  `json:"amount"`       // ERC721 = 1
	PaymentToken string   `json:"paymentToken"` // address(0) = ETH
	Price        *BigInt  `json:"price"`
	StartPrice   *BigInt  `json:"startPrice"`   // Dutch auction start price
	StartTime    uint64   `json:"startTime"`
	EndTime      uint64   `json:"endTime"`      // 0 = never expires
	Salt         *BigInt  `json:"salt"`
	Counter      *BigInt  `json:"counter"`
	Extra        string   `json:"extra"`        // bytes32

	// Server-side metadata
	ID        int64       `json:"id"`
	OrderHash string      `json:"orderHash"`
	Status    OrderStatus `json:"status"`
	Signature string      `json:"signature"`
	CreatedAt time.Time   `json:"createdAt"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

// CurrentPrice returns the effective price of the order at the current time.
//
// FixedPrice: always returns the price field unchanged.
//
// DutchAuction: performs linear decay from startPrice down to price over the
// duration (endTime - startTime). Before startTime returns startPrice; after
// endTime returns price. The formula is:
//
//	currentPrice = startPrice - (startPrice - price) * elapsed / duration
func (o *Order) CurrentPrice(now time.Time) *big.Int {
	if o.Kind != DutchAuction {
		// FixedPrice, CollectionBid, TraitBid, Bundle all use the fixed price.
		return new(big.Int).Set(o.Price.Int)
	}

	start := int64(o.StartTime)
	end := int64(o.EndTime)

	// Never expires or invalid duration — return the end price.
	if end == 0 || end <= start {
		return new(big.Int).Set(o.Price.Int)
	}

	ts := now.Unix()

	// Auction hasn't started yet.
	if ts <= start {
		return new(big.Int).Set(o.StartPrice.Int)
	}

	// Auction has ended.
	if ts >= end {
		return new(big.Int).Set(o.Price.Int)
	}

	// Linear decay: startPrice - (startPrice - price) * elapsed / duration
	elapsed := big.NewInt(ts - start)
	duration := big.NewInt(end - start)

	// delta = startPrice - price
	delta := new(big.Int).Sub(o.StartPrice.Int, o.Price.Int)

	// decay = delta * elapsed / duration
	decay := new(big.Int).Mul(delta, elapsed)
	decay.Div(decay, duration)

	// result = startPrice - decay
	result := new(big.Int).Sub(o.StartPrice.Int, decay)
	return result
}

// IsExpired returns true when EndTime is non-zero and in the past.
// An EndTime of 0 means the order never expires.
func (o *Order) IsExpired(now time.Time) bool {
	if o.EndTime == 0 {
		return false
	}
	return now.Unix() >= int64(o.EndTime)
}

// OrderFilter groups optional query parameters for listing/searching orders.
type OrderFilter struct {
	Maker        string       `json:"maker,omitempty"`
	Side         *OrderSide   `json:"side,omitempty"`
	Kind         *OrderKind   `json:"kind,omitempty"`
	AssetType    *AssetType   `json:"assetType,omitempty"`
	Collection   string       `json:"collection,omitempty"`
	TokenID      *BigInt      `json:"tokenId,omitempty"`
	PaymentToken string       `json:"paymentToken,omitempty"`
	Status       *OrderStatus `json:"status,omitempty"`
	MinPrice     *BigInt      `json:"minPrice,omitempty"`
	MaxPrice     *BigInt      `json:"maxPrice,omitempty"`
	Limit        int          `json:"limit,omitempty"`
	Offset       int          `json:"offset,omitempty"`
}

// OrderResponse is a JSON-friendly order representation for API responses.
type OrderResponse struct {
	Order       *Order `json:"order"`
	CurrentPrice string `json:"currentPrice"` // decimal string of CurrentPrice()
	IsExpired   bool   `json:"isExpired"`
}

// SubmitOrderRequest is the payload for creating a new order via the API.
type SubmitOrderRequest struct {
	Maker        string    `json:"maker" binding:"required"`
	Taker        string    `json:"taker"`
	Side         OrderSide `json:"side" binding:"gte=0,lte=1"`
	Kind         OrderKind `json:"kind" binding:"gte=0,lte=4"`
	AssetType    AssetType `json:"assetType" binding:"gte=0,lte=1"`
	Collection   string    `json:"collection" binding:"required"`
	TokenID      string    `json:"tokenId" binding:"required"`      // decimal string
	Amount       string    `json:"amount" binding:"required"`       // decimal string
	PaymentToken string    `json:"paymentToken"`
	Price        string    `json:"price" binding:"required"`        // decimal string
	StartPrice   string    `json:"startPrice,omitempty"`            // decimal string
	StartTime    uint64    `json:"startTime" binding:"required"`
	EndTime      uint64    `json:"endTime"`
	Salt         string    `json:"salt" binding:"required"`         // decimal string
	Extra        string    `json:"extra"`
	Signature    string    `json:"signature" binding:"required"`
}

// ErrorResponse is a simple error payload for API responses.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}
