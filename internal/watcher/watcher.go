package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"nft-market-backend/internal/domain"
	logpkg "nft-market-backend/internal/log"
	"nft-market-backend/internal/repository"
	"nft-market-backend/internal/service"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

// Event signatures must match the canonical Solidity form:
//
//	event OrderFulfilled(bytes32 indexed, uint256 indexed, address indexed,
//	    address, address, address, uint8, uint8, address, uint256, uint256, address, uint128, uint256, uint256);
//	event OrderCancelled(address indexed maker, uint256 indexed salt);
//	event CounterIncremented(address indexed maker, uint256 newCounter);
//	event CollectionUpdated(address indexed collection, bool allowed, bool blocked);
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
	sem          chan struct{}
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
		sem:                make(chan struct{}, 64),
	}
}

// Run starts the watcher goroutines.
func (w *Watcher) Run(ctx context.Context) {
	cursor, err := w.eventRepo.GetLastSyncedBlock(w.chainID)
	if err != nil {
		logpkg.Logger.Error("watcher: get cursor failed", zap.Error(err))
	}
	if cursor == 0 {
		latest, err := w.rpc.BlockNumber(ctx)
		if err != nil {
			logpkg.Logger.Error("watcher: get latest block failed", zap.Error(err))
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
			logpkg.Logger.Warn("watcher: subscribe failed, retrying", zap.Error(err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		logpkg.Logger.Info("watcher: subscribed", zap.Int64("chain_id", w.chainID))

	inner:
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-sub.Err():
				logpkg.Logger.Warn("watcher: subscription error, reconnecting", zap.Error(err))
				break inner
			case vLog := <-logCh:
				w.mu.Lock()
				w.lastActivity = time.Now()
				w.mu.Unlock()

				select {
				case w.sem <- struct{}{}:
					go func() {
						defer func() { <-w.sem }()
						w.handleLog(ctx, vLog)
					}()
				default:
					logpkg.Logger.Warn("watcher: dropping log, handler saturated",
						zap.Uint64("block", uint64(vLog.BlockNumber)))
				}
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
					logpkg.Logger.Error("watcher: poll failed", zap.Error(err))
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
				logpkg.Logger.Error("watcher: save cursor failed", zap.Error(err))
			}
		}
	}
}

func (w *Watcher) handleLog(ctx context.Context, vLog types.Log) {
	event, err := w.parseEvent(vLog)
	if err != nil {
		logpkg.Logger.Error("watcher: parse event failed", zap.Error(err))
		return
	}
	if event == nil {
		return
	}

	exists, err := w.eventRepo.EventExists(uint64(vLog.BlockNumber), uint(vLog.TxIndex), uint(vLog.Index))
	if err != nil {
		logpkg.Logger.Error("watcher: check exists failed", zap.Error(err))
		return
	}
	if exists {
		return
	}

	if err := w.waitConfirmations(ctx, uint64(vLog.BlockNumber)); err != nil {
		logpkg.Logger.Error("watcher: wait confirmations failed", zap.Error(err))
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
		logpkg.Logger.Error("watcher: insert event failed", zap.Error(err))
		return
	}

	if err := w.eventSvc.Handle(event); err != nil {
		logpkg.Logger.Error("watcher: handle event failed", zap.Error(err))
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
	// Event layout (3 indexed topics + 12 non-indexed data fields = 384 bytes):
	//   topic1 = orderHash (bytes32)
	//   topic2 = salt (uint256)
	//   topic3 = maker (address)
	//   data[0:32]    = taker (address)
	//   data[32:64]   = seller (address)
	//   data[64:96]   = buyer (address)
	//   data[96:128]  = side (uint8)
	//   data[128:160] = kind (uint8)
	//   data[160:192] = collection (address)
	//   data[192:224] = tokenId (uint256)
	//   data[224:256] = amount (uint256)
	//   data[256:288] = paymentToken (address)
	//   data[288:320] = finalPrice (uint128)
	//   data[320:352] = protocolFee (uint256)
	//   data[352:384] = royaltyFee (uint256)
	var data domain.OrderFulfilledData
	data.OrderHash = vLog.Topics[1].Hex()
	data.Maker = common.BytesToAddress(vLog.Topics[3].Bytes()).Hex()
	if len(vLog.Data) >= 384 {
		data.Taker = common.BytesToAddress(vLog.Data[0:32]).Hex()
		data.Seller = common.BytesToAddress(vLog.Data[32:64]).Hex()
		data.Buyer = common.BytesToAddress(vLog.Data[64:96]).Hex()
		data.Collection = common.BytesToAddress(vLog.Data[160:192]).Hex()
		data.TokenID = new(big.Int).SetBytes(vLog.Data[192:224]).String()
		data.Amount = new(big.Int).SetBytes(vLog.Data[224:256]).String()
		data.PaymentToken = common.BytesToAddress(vLog.Data[256:288]).Hex()
		data.Price = new(big.Int).SetBytes(vLog.Data[288:320]).String()
		data.ProtocolFee = new(big.Int).SetBytes(vLog.Data[320:352]).String()
		data.RoyaltyFee = new(big.Int).SetBytes(vLog.Data[352:384]).String()
	}
	return data, nil
}

func parseOrderCancelled(vLog types.Log) (domain.OrderCancelledData, error) {
	// Event layout: OrderCancelled(address indexed maker, uint256 indexed salt)
	//   topic1 = maker (address)
	//   topic2 = salt (uint256)
	//   data = (empty)
	var data domain.OrderCancelledData
	data.Maker = common.BytesToAddress(vLog.Topics[1].Bytes()).Hex()
	data.Salt = new(big.Int).SetBytes(vLog.Topics[2].Bytes()).String()
	return data, nil
}

func parseCounterIncremented(vLog types.Log) (domain.CounterIncrementedData, error) {
	// Event layout: CounterIncremented(address indexed maker, uint256 newCounter)
	//   topic1 = maker (address)
	//   data[0:32] = newCounter (uint256)
	var data domain.CounterIncrementedData
	data.Maker = common.BytesToAddress(vLog.Topics[1].Bytes()).Hex()
	if len(vLog.Data) >= 32 {
		data.Counter = new(big.Int).SetBytes(vLog.Data[0:32]).String()
	}
	return data, nil
}

func parseCollectionUpdated(vLog types.Log) (struct {
	Collection string `json:"collection"`
	Allowed    bool   `json:"allowed"`
	Blocked    bool   `json:"blocked"`
}, error) {
	// Event layout: CollectionUpdated(address indexed collection, bool allowed, bool blocked)
	//   topic1 = collection (address)
	//   data[0:32]  = allowed (bool, padded to 32 bytes)
	//   data[32:64] = blocked (bool, padded to 32 bytes)
	var data struct {
		Collection string `json:"collection"`
		Allowed    bool   `json:"allowed"`
		Blocked    bool   `json:"blocked"`
	}
	data.Collection = common.BytesToAddress(vLog.Topics[1].Bytes()).Hex()
	if len(vLog.Data) >= 64 {
		data.Allowed = new(big.Int).SetBytes(vLog.Data[0:32]).Cmp(big.NewInt(1)) == 0
		data.Blocked = new(big.Int).SetBytes(vLog.Data[32:64]).Cmp(big.NewInt(1)) == 0
	}
	return data, nil
}
