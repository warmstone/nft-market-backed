package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"strings"
	"sync"
	"time"

	"nft-market-backend/internal/domain"
	"nft-market-backend/internal/repository"
	"nft-market-backend/internal/service"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	topicOrderFulfilled     = eventSigHash("OrderFulfilled(bytes32,uint256,address,address,address,address,uint8,uint8,address,uint256,uint256,address,uint128,uint256,uint256)")
	topicOrderCancelled     = eventSigHash("OrderCancelled(address,uint256)")
	topicCounterIncremented = eventSigHash("CounterIncremented(address,uint256)")
	topicCollectionUpdated  = eventSigHash("CollectionUpdated(address,bool,bool)")
)

func eventSigHash(s string) string {
	return crypto.Keccak256Hash([]byte(s)).Hex()
}

// RPCClient is the interface the Watcher needs from the RPC layer.
type RPCClient interface {
	BlockNumber(ctx context.Context) (uint64, error)
	FilterLogsQuery(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
	SubscribeLogs(ctx context.Context, ch chan<- types.Log) (ethereum.Subscription, error)
	ChainID(ctx context.Context) (*big.Int, error)
}

// Watcher syncs on-chain events via WebSocket subscription with polling fallback.
type Watcher struct {
	rpc                RPCClient
	eventRepo          *repository.EventRepo
	eventSvc           *service.EventService
	chainID            int64
	confirmationBlocks uint64

	mu           sync.Mutex
	lastActivity time.Time
	cursor       uint64
}

// NewWatcher creates a Watcher.
func NewWatcher(
	rpc RPCClient,
	eventRepo *repository.EventRepo,
	eventSvc *service.EventService,
	chainID int64,
	confirmationBlocks uint64,
) *Watcher {
	return &Watcher{
		rpc:                rpc,
		eventRepo:          eventRepo,
		eventSvc:           eventSvc,
		chainID:            chainID,
		confirmationBlocks: confirmationBlocks,
	}
}

// Run starts the watcher goroutines.
func (w *Watcher) Run(ctx context.Context) {
	cursor, err := w.eventRepo.GetLastSyncedBlock(w.chainID)
	if err != nil {
		log.Printf("watcher: get cursor: %v", err)
	}
	if cursor == 0 {
		latest, err := w.rpc.BlockNumber(ctx)
		if err != nil {
			log.Printf("watcher: get latest block: %v", err)
			latest = 0
		}
		if latest > 100 {
			cursor = latest - 100
		}
	}
	w.cursor = cursor

	go w.subscribeLoop(ctx)
	go w.pollLoop(ctx)
	go w.processLoop(ctx)
}

func (w *Watcher) subscribeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		logCh := make(chan types.Log, 100)
		sub, err := w.rpc.SubscribeLogs(ctx, logCh)
		if err != nil {
			log.Printf("watcher: subscribe: %v, retrying in 5s", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		log.Printf("watcher: subscribed to chain %d", w.chainID)

	inner:
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-sub.Err():
				log.Printf("watcher: subscription error: %v, reconnecting", err)
				break inner
			case vLog := <-logCh:
				w.mu.Lock()
				w.lastActivity = time.Now()
				w.mu.Unlock()

				go w.handleLog(ctx, vLog)
			}
		}
	}
}

func (w *Watcher) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.mu.Lock()
			idle := time.Since(w.lastActivity) > 60*time.Second
			w.mu.Unlock()

			if idle {
				if err := w.poll(ctx); err != nil {
					log.Printf("watcher: poll: %v", err)
				}
			}
		}
	}
}

func (w *Watcher) poll(ctx context.Context) error {
	latest, err := w.rpc.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("get block number: %w", err)
	}

	w.mu.Lock()
	fromBlock := w.cursor + 1
	w.mu.Unlock()

	if fromBlock+w.confirmationBlocks > latest {
		return nil
	}
	toBlock := latest - w.confirmationBlocks

	query := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(fromBlock),
		ToBlock:   new(big.Int).SetUint64(toBlock),
	}

	logs, err := w.rpc.FilterLogsQuery(ctx, query)
	if err != nil {
		return fmt.Errorf("filter logs: %w", err)
	}

	for _, vLog := range logs {
		w.handleLog(ctx, vLog)
	}

	w.mu.Lock()
	if toBlock > w.cursor {
		w.cursor = toBlock
	}
	w.mu.Unlock()

	return nil
}

func (w *Watcher) processLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.mu.Lock()
			cursor := w.cursor
			w.mu.Unlock()

			if err := w.eventRepo.UpdateLastSyncedBlock(w.chainID, cursor); err != nil {
				log.Printf("watcher: save cursor: %v", err)
			}
		}
	}
}

func (w *Watcher) handleLog(ctx context.Context, vLog types.Log) {
	event, err := w.parseEvent(vLog)
	if err != nil {
		log.Printf("watcher: parse event: %v", err)
		return
	}
	if event == nil {
		return
	}

	exists, err := w.eventRepo.EventExists(uint64(vLog.BlockNumber), uint(vLog.TxIndex), uint(vLog.Index))
	if err != nil {
		log.Printf("watcher: check exists: %v", err)
		return
	}
	if exists {
		return
	}

	if err := w.waitConfirmations(ctx, uint64(vLog.BlockNumber)); err != nil {
		log.Printf("watcher: wait confirmations: %v", err)
		return
	}

	if vLog.Removed {
		_ = w.eventRepo.MarkRemoved(uint64(vLog.BlockNumber), uint(vLog.TxIndex), uint(vLog.Index))
		reorgBlock := uint64(vLog.BlockNumber)
		if reorgBlock > 12 {
			reorgBlock -= 12
		} else {
			reorgBlock = 0
		}
		w.mu.Lock()
		w.cursor = reorgBlock
		w.mu.Unlock()
		_ = w.eventRepo.DeleteEventsAboveBlock(reorgBlock)
		return
	}

	if err := w.eventRepo.InsertEvent(event); err != nil {
		log.Printf("watcher: insert event: %v", err)
		return
	}

	if err := w.eventSvc.Handle(event); err != nil {
		log.Printf("watcher: handle event: %v", err)
	}

	w.mu.Lock()
	if uint64(vLog.BlockNumber) > w.cursor {
		w.cursor = uint64(vLog.BlockNumber)
	}
	w.mu.Unlock()
}

func (w *Watcher) parseEvent(vLog types.Log) (*domain.ContractEvent, error) {
	if len(vLog.Topics) == 0 {
		return nil, nil
	}

	topic0 := vLog.Topics[0].Hex()
	var eventName string
	var eventData interface{}

	switch strings.ToLower(topic0) {
	case strings.ToLower(topicOrderFulfilled):
		eventName = domain.EventOrderFulfilled
		data, err := parseOrderFulfilled(vLog)
		if err != nil {
			return nil, err
		}
		eventData = data
	case strings.ToLower(topicOrderCancelled):
		eventName = domain.EventOrderCancelled
		data, err := parseOrderCancelled(vLog)
		if err != nil {
			return nil, err
		}
		eventData = data
	case strings.ToLower(topicCounterIncremented):
		eventName = domain.EventCounterIncremented
		data, err := parseCounterIncremented(vLog)
		if err != nil {
			return nil, err
		}
		eventData = data
	case strings.ToLower(topicCollectionUpdated):
		eventName = domain.EventCollectionUpdated
		data, err := parseCollectionUpdated(vLog)
		if err != nil {
			return nil, err
		}
		eventData = data
	default:
		return nil, nil
	}

	dataJSON, err := json.Marshal(eventData)
	if err != nil {
		return nil, fmt.Errorf("marshal event data: %w", err)
	}

	return &domain.ContractEvent{
		BlockNumber: uint64(vLog.BlockNumber),
		TxHash:      vLog.TxHash.Hex(),
		TxIndex:     uint(vLog.TxIndex),
		LogIndex:    uint(vLog.Index),
		EventName:   eventName,
		EventData:   dataJSON,
		Removed:     vLog.Removed,
	}, nil
}

func (w *Watcher) waitConfirmations(ctx context.Context, blockNum uint64) error {
	for {
		latest, err := w.rpc.BlockNumber(ctx)
		if err != nil {
			return fmt.Errorf("get block number: %w", err)
		}
		if latest >= blockNum+w.confirmationBlocks {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func parseOrderFulfilled(vLog types.Log) (domain.OrderFulfilledData, error) {
	var data domain.OrderFulfilledData
	if len(vLog.Topics) < 4 {
		return data, fmt.Errorf("OrderFulfilled topics: expected at least 4, got %d", len(vLog.Topics))
	}
	data.OrderHash = vLog.Topics[1].Hex()
	data.Salt = new(big.Int).SetBytes(vLog.Topics[2].Bytes()).String()
	data.Maker = common.BytesToAddress(vLog.Topics[3].Bytes()).Hex()

	values, err := orderFulfilledABIArgs().Unpack(vLog.Data)
	if err != nil {
		return data, fmt.Errorf("unpack OrderFulfilled: %w", err)
	}
	data.Taker = values[0].(common.Address).Hex()
	data.Seller = values[1].(common.Address).Hex()
	data.Buyer = values[2].(common.Address).Hex()
	data.Side = values[3].(uint8)
	data.Kind = values[4].(uint8)
	data.Collection = values[5].(common.Address).Hex()
	data.TokenID = abiUintString(values[6])
	data.Amount = abiUintString(values[7])
	data.PaymentToken = values[8].(common.Address).Hex()
	data.Price = abiUintString(values[9])
	data.ProtocolFee = abiUintString(values[10])
	data.RoyaltyFee = abiUintString(values[11])
	return data, nil
}

func parseOrderCancelled(vLog types.Log) (domain.OrderCancelledData, error) {
	var data domain.OrderCancelledData
	if len(vLog.Topics) < 3 {
		return data, fmt.Errorf("OrderCancelled topics: expected at least 3, got %d", len(vLog.Topics))
	}
	data.Maker = common.BytesToAddress(vLog.Topics[1].Bytes()).Hex()
	data.Salt = new(big.Int).SetBytes(vLog.Topics[2].Bytes()).String()
	return data, nil
}

func parseCounterIncremented(vLog types.Log) (domain.CounterIncrementedData, error) {
	var data domain.CounterIncrementedData
	if len(vLog.Topics) < 2 {
		return data, fmt.Errorf("CounterIncremented topics: expected at least 2, got %d", len(vLog.Topics))
	}
	data.Maker = common.BytesToAddress(vLog.Topics[1].Bytes()).Hex()
	values, err := singleUint256ABIArgs().Unpack(vLog.Data)
	if err != nil {
		return data, fmt.Errorf("unpack CounterIncremented: %w", err)
	}
	data.Counter = abiUintString(values[0])
	return data, nil
}

func parseCollectionUpdated(vLog types.Log) (struct {
	Collection string `json:"collection"`
	Allowed    bool   `json:"allowed"`
	Blocked    bool   `json:"blocked"`
}, error) {
	var data struct {
		Collection string `json:"collection"`
		Allowed    bool   `json:"allowed"`
		Blocked    bool   `json:"blocked"`
	}
	if len(vLog.Topics) < 2 {
		return data, fmt.Errorf("CollectionUpdated topics: expected at least 2, got %d", len(vLog.Topics))
	}
	data.Collection = common.BytesToAddress(vLog.Topics[1].Bytes()).Hex()
	values, err := collectionUpdatedABIArgs().Unpack(vLog.Data)
	if err != nil {
		return data, fmt.Errorf("unpack CollectionUpdated: %w", err)
	}
	data.Allowed = values[0].(bool)
	data.Blocked = values[1].(bool)
	return data, nil
}

func orderFulfilledABIArgs() abi.Arguments {
	addressT, _ := abi.NewType("address", "", nil)
	uint8T, _ := abi.NewType("uint8", "", nil)
	uint128T, _ := abi.NewType("uint128", "", nil)
	uint256T, _ := abi.NewType("uint256", "", nil)
	return abi.Arguments{
		{Name: "taker", Type: addressT},
		{Name: "seller", Type: addressT},
		{Name: "buyer", Type: addressT},
		{Name: "side", Type: uint8T},
		{Name: "kind", Type: uint8T},
		{Name: "collection", Type: addressT},
		{Name: "tokenId", Type: uint256T},
		{Name: "amount", Type: uint256T},
		{Name: "paymentToken", Type: addressT},
		{Name: "finalPrice", Type: uint128T},
		{Name: "protocolFee", Type: uint256T},
		{Name: "royaltyFee", Type: uint256T},
	}
}

func singleUint256ABIArgs() abi.Arguments {
	uint256T, _ := abi.NewType("uint256", "", nil)
	return abi.Arguments{{Type: uint256T}}
}

func collectionUpdatedABIArgs() abi.Arguments {
	boolT, _ := abi.NewType("bool", "", nil)
	return abi.Arguments{{Name: "allowed", Type: boolT}, {Name: "blocked", Type: boolT}}
}

func abiUintString(v interface{}) string {
	switch n := v.(type) {
	case *big.Int:
		return n.String()
	case uint8:
		return new(big.Int).SetUint64(uint64(n)).String()
	case uint64:
		return new(big.Int).SetUint64(n).String()
	default:
		return fmt.Sprintf("%v", n)
	}
}
