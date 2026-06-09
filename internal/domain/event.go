package domain

import "encoding/json"

// Event name constants for known contract events.
const (
	EventOrderFulfilled       = "OrderFulfilled"
	EventOrderCancelled       = "OrderCancelled"
	EventCounterIncremented   = "CounterIncremented"
	EventCollectionUpdated    = "CollectionUpdated"
	EventOwnershipTransferred = "OwnershipTransferred"
)

// ContractEvent represents a parsed log event emitted by the NFT market contract.
type ContractEvent struct {
	ID          int64           `json:"id"`
	BlockNumber uint64          `json:"blockNumber"`
	TxHash      string          `json:"txHash"`
	TxIndex     uint            `json:"txIndex"`
	LogIndex    uint            `json:"logIndex"`
	EventName   string          `json:"eventName"`
	EventData   json.RawMessage `json:"eventData"`
	Removed     bool            `json:"removed"`
}

// OrderFulfilledData is the event data emitted when an order is matched and filled.
type OrderFulfilledData struct {
	OrderHash    string `json:"orderHash"`
	Salt         string `json:"salt"` // decimal string (*big.Int)
	Maker        string `json:"maker"`
	Taker        string `json:"taker"`
	Seller       string `json:"seller"`
	Buyer        string `json:"buyer"`
	Side         uint8  `json:"side"`
	Kind         uint8  `json:"kind"`
	Collection   string `json:"collection"`
	TokenID      string `json:"tokenId"` // decimal string (*big.Int)
	Amount       string `json:"amount"`  // decimal string (*big.Int)
	PaymentToken string `json:"paymentToken"`
	Price        string `json:"price"` // decimal string (*big.Int)
	ProtocolFee  string `json:"protocolFee"`
	RoyaltyFee   string `json:"royaltyFee"`
}

// OrderCancelledData is the event data emitted when an order is cancelled by its maker.
type OrderCancelledData struct {
	Maker string `json:"maker"`
	Salt  string `json:"salt"` // decimal string (*big.Int)
}

// CounterIncrementedData is the event data emitted when a maker increments their nonce/counter.
type CounterIncrementedData struct {
	Maker   string `json:"maker"`
	Counter string `json:"counter"` // decimal string (*big.Int)
}
