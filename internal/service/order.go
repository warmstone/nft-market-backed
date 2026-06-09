package service

import (
	"fmt"
	"math/big"
	"time"

	"nft-market-backend/internal/domain"
	"nft-market-backend/internal/repository"

	"github.com/ethereum/go-ethereum/common"
)

// OrderService handles order submission, querying, and response formatting.
type OrderService struct {
	orderRepo      *repository.OrderRepo
	collectionRepo *repository.CollectionRepo
	sigSvc         *SignatureService
	chainID        int64
}

// NewOrderService creates an OrderService.
func NewOrderService(
	orderRepo *repository.OrderRepo,
	collectionRepo *repository.CollectionRepo,
	sigSvc *SignatureService,
	chainID int64,
) *OrderService {
	return &OrderService{
		orderRepo:      orderRepo,
		collectionRepo: collectionRepo,
		sigSvc:         sigSvc,
		chainID:        chainID,
	}
}

// Submit validates and persists a new signed order.
func (s *OrderService) Submit(req *domain.SubmitOrderRequest) (*domain.Order, error) {
	now := time.Now()

	// 1. Validate signature.
	order := requestToOrder(req, s.chainID)
	if err := s.sigSvc.Verify(order); err != nil {
		return nil, fmt.Errorf("ORDER_SIGNATURE_INVALID: %w", err)
	}

	// 2. Validate maker.
	if !common.IsHexAddress(req.Maker) || req.Maker == "0x0000000000000000000000000000000000000000" {
		return nil, fmt.Errorf("INVALID_MAKER: maker must be a non-zero address")
	}

	// 3. Validate taker (address(0) or valid).
	if req.Taker != "" && !common.IsHexAddress(req.Taker) {
		return nil, fmt.Errorf("INVALID_TAKER: taker must be a valid address or empty")
	}

	// 4. Validate side, kind, assetType.
	if req.Side > 1 {
		return nil, fmt.Errorf("INVALID_SIDE: must be 0 or 1")
	}
	if req.Kind > 4 {
		return nil, fmt.Errorf("INVALID_KIND: must be 0-4")
	}
	if req.AssetType > 1 {
		return nil, fmt.Errorf("INVALID_ASSET_TYPE: must be 0 or 1")
	}

	// 5. Validate amount.
	amount := new(big.Int)
	amount.SetString(req.Amount, 10)
	if amount.Sign() <= 0 {
		return nil, fmt.Errorf("INVALID_AMOUNT: amount must be >= 1")
	}
	if req.AssetType == domain.ERC721 && amount.Cmp(big.NewInt(1)) != 0 {
		return nil, fmt.Errorf("INVALID_AMOUNT: ERC721 amount must be 1")
	}

	// 6. Validate price.
	price := new(big.Int)
	price.SetString(req.Price, 10)
	if price.Sign() <= 0 {
		return nil, fmt.Errorf("INVALID_PRICE: price must be > 0")
	}

	// 7. Validate time window.
	nowUnix := now.Unix()
	if req.StartTime > 0 {
		start := int64(req.StartTime)
		if start < nowUnix-300 {
			return nil, fmt.Errorf("ORDER_EXPIRED: startTime too far in the past")
		}
		if start > nowUnix+30*24*3600 {
			return nil, fmt.Errorf("INVALID_START_TIME: startTime too far in the future")
		}
	}
	if req.EndTime > 0 && int64(req.EndTime) <= nowUnix {
		return nil, fmt.Errorf("ORDER_EXPIRED: endTime is in the past")
	}

	// 8. Dutch auction constraints.
	if req.Kind == domain.DutchAuction {
		startPrice := new(big.Int)
		startPrice.SetString(req.StartPrice, 10)
		if startPrice.Cmp(price) <= 0 {
			return nil, fmt.Errorf("INVALID_DUTCH_AUCTION: startPrice must be > price")
		}
		if req.EndTime <= req.StartTime {
			return nil, fmt.Errorf("INVALID_DUTCH_AUCTION: endTime must be > startTime")
		}
	}

	// 9. Compute the same struct hash Solidity uses in LibOrder.hash(order).
	orderHash, err := OrderStructHash(order)
	if err != nil {
		return nil, fmt.Errorf("ORDER_HASH_FAILED: %w", err)
	}
	order.OrderHash = orderHash.Hex()

	// 10. Persist.
	if err := s.orderRepo.Insert(order); err != nil {
		return nil, fmt.Errorf("ORDER_PERSIST_FAILED: %w", err)
	}

	return order, nil
}

// Find queries orders with the given filters.
func (s *OrderService) Find(filter domain.OrderFilter) ([]domain.Order, int64, error) {
	return s.orderRepo.Find(filter)
}

// GetByHash returns a single order by its hash.
func (s *OrderService) GetByHash(hash string) (*domain.Order, error) {
	return s.orderRepo.FindByHash(hash)
}

// GetBest returns the best-priced active order for a collection and side.
func (s *OrderService) GetBest(collection string, side domain.OrderSide) (*domain.Order, error) {
	price, err := s.orderRepo.GetBestPrice(collection, side)
	if err != nil {
		return nil, err
	}
	if price == nil {
		return nil, nil
	}

	// Fetch all orders at that price and return the first.
	filter := domain.OrderFilter{
		Collection: collection,
		Status:     statusPtr(domain.Active),
	}
	orders, _, err := s.orderRepo.Find(filter)
	if err != nil {
		return nil, err
	}
	for i := range orders {
		if orders[i].CurrentPrice(time.Now()).Cmp(price) == 0 {
			return &orders[i], nil
		}
	}
	return nil, nil
}

// GetUserOrders returns orders for a given maker address.
func (s *OrderService) GetUserOrders(maker string, status *domain.OrderStatus) ([]domain.Order, error) {
	filter := domain.OrderFilter{
		Maker:  maker,
		Status: status,
		Limit:  50,
	}
	orders, _, err := s.orderRepo.Find(filter)
	return orders, err
}

func statusPtr(s domain.OrderStatus) *domain.OrderStatus {
	return &s
}

func requestToOrder(req *domain.SubmitOrderRequest, chainID int64) *domain.Order {
	tokenID := parseBigInt(req.TokenID)
	amount := parseBigInt(req.Amount)
	price := parseBigInt(req.Price)
	startPrice := parseBigInt(req.StartPrice)
	if startPrice.Sign() == 0 {
		startPrice = new(big.Int).Set(price)
	}
	salt := parseBigInt(req.Salt)

	taker := req.Taker
	if taker == "" {
		taker = "0x0000000000000000000000000000000000000000"
	}
	paymentToken := req.PaymentToken
	if paymentToken == "" {
		paymentToken = "0x0000000000000000000000000000000000000000"
	}
	extra := req.Extra
	if extra == "" {
		extra = "0x0000000000000000000000000000000000000000000000000000000000000000"
	}

	counter := int64(0) // Server fills this from DB on persistence if needed.

	return &domain.Order{
		Maker:        req.Maker,
		Taker:        taker,
		Side:         req.Side,
		Kind:         req.Kind,
		AssetType:    req.AssetType,
		Collection:   req.Collection,
		TokenID:      &domain.BigInt{Int: tokenID},
		Amount:       &domain.BigInt{Int: amount},
		PaymentToken: paymentToken,
		Price:        &domain.BigInt{Int: price},
		StartPrice:   &domain.BigInt{Int: startPrice},
		StartTime:    req.StartTime,
		EndTime:      req.EndTime,
		Salt:         &domain.BigInt{Int: salt},
		Counter:      &domain.BigInt{Int: big.NewInt(counter)},
		Extra:        extra,
		Status:       domain.Active,
		Signature:    req.Signature,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
}

func parseBigInt(s string) *big.Int {
	i := new(big.Int)
	if s != "" {
		i.SetString(s, 10)
	}
	return i
}
