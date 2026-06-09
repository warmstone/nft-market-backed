package domain

import "encoding/json"

// Event name constants for known contract events.
const (
	EventOrderFulfilled      = "OrderFulfilled"
	EventOrderCancelled      = "OrderCancelled"
	EventCounterIncremented  = "CounterIncremented"
	EventCollectionUpdated   = "CollectionUpdated"
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
	Maker        string `json:"maker"`
	Taker        string `json:"taker"`
	Seller       string `json:"seller"`
	Buyer        string `json:"buyer"`
	Collection   string `json:"collection"`
	TokenID      string `json:"tokenId"`
	Amount       string `json:"amount"`
	PaymentToken string `json:"paymentToken"`
	Price        string `json:"price"`
	ProtocolFee  string `json:"protocolFee"`
	RoyaltyFee   string `json:"royaltyFee"`
}

// OrderCancelledData is the event data emitted when an order is cancelled by its maker.
// The contract emits (maker, salt) — there is no orderHash in the on-chain event.
type OrderCancelledData struct {
	Maker string `json:"maker"`
	Salt  string `json:"salt"`
}

// CounterIncrementedData is the event data emitted when a maker increments their nonce/counter.
type CounterIncrementedData struct {
	Maker   string `json:"maker"`
	Counter string `json:"counter"` // decimal string (*big.Int)
}
