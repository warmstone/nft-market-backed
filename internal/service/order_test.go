package service

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"nft-market-backend/internal/domain"
)

// ---------- mocks ----------

// mockOrderRepo implements OrderRepository with configurable behavior.
type mockOrderRepo struct {
	insertFn     func(o *domain.Order) error
	findFn       func(filter domain.OrderFilter) ([]domain.Order, int64, error)
	findHashFn   func(hash string) (*domain.Order, error)
	bestPriceFn  func(collection string, side domain.OrderSide) (*big.Int, error)
	insertCalled bool
	lastInserted *domain.Order
}

func (m *mockOrderRepo) Insert(o *domain.Order) error {
	m.insertCalled = true
	m.lastInserted = o
	if m.insertFn != nil {
		return m.insertFn(o)
	}
	return nil
}

func (m *mockOrderRepo) Find(filter domain.OrderFilter) ([]domain.Order, int64, error) {
	if m.findFn != nil {
		return m.findFn(filter)
	}
	return nil, 0, nil
}

func (m *mockOrderRepo) FindByHash(hash string) (*domain.Order, error) {
	if m.findHashFn != nil {
		return m.findHashFn(hash)
	}
	return nil, nil
}

func (m *mockOrderRepo) GetBestPrice(collection string, side domain.OrderSide) (*big.Int, error) {
	if m.bestPriceFn != nil {
		return m.bestPriceFn(collection, side)
	}
	return nil, nil
}

// mockSigVerifier implements SignatureVerifier with configurable behavior.
type mockSigVerifier struct {
	verifyFn func(order *domain.Order) error
}

func (m *mockSigVerifier) Verify(order *domain.Order) error {
	if m.verifyFn != nil {
		return m.verifyFn(order)
	}
	return nil
}

// ---------- helpers ----------

func newTestService(repo *mockOrderRepo, sig *mockSigVerifier) *OrderService {
	return &OrderService{
		orderRepo:      repo,
		collectionRepo: nil,
		sigSvc:         sig,
		chainID:        1,
	}
}

// validFixedPriceRequest returns a valid sell order request with FixedPrice kind.
func validFixedPriceRequest() *domain.SubmitOrderRequest {
	now := time.Now()
	return &domain.SubmitOrderRequest{
		Maker:        "0x1234567890123456789012345678901234567890",
		Taker:        "",
		Side:         domain.Sell,
		Kind:         domain.FixedPrice,
		AssetType:    domain.ERC721,
		Collection:   "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		TokenID:      "1",
		Amount:       "1",
		PaymentToken: "",
		Price:        "1000000000000000000", // 1 ETH
		StartPrice:   "0",
		StartTime:    uint64(now.Unix()),
		EndTime:      uint64(now.Add(24 * time.Hour).Unix()),
		Salt:         "12345",
		Extra:        "",
		Signature:    "0x" + string(make([]byte, 130)), // placeholder
	}
}

// ---------- Submit tests ----------

func TestSubmit_Success_FixedPrice(t *testing.T) {
	repo := &mockOrderRepo{}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	req := validFixedPriceRequest()
	order, err := svc.Submit(req)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !repo.insertCalled {
		t.Fatal("expected Insert to be called")
	}
	if order.OrderHash == "" {
		t.Fatal("expected order hash to be computed")
	}
	if order.Status != domain.Active {
		t.Fatalf("expected status Active, got %d", order.Status)
	}
	if order.Kind != domain.FixedPrice {
		t.Fatalf("expected kind FixedPrice, got %d", order.Kind)
	}
}

func TestSubmit_Success_DutchAuction(t *testing.T) {
	repo := &mockOrderRepo{}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	now := time.Now()
	req := &domain.SubmitOrderRequest{
		Maker:        "0x1234567890123456789012345678901234567890",
		Taker:        "",
		Side:         domain.Sell,
		Kind:         domain.DutchAuction,
		AssetType:    domain.ERC721,
		Collection:   "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		TokenID:      "1",
		Amount:       "1",
		PaymentToken: "",
		Price:        "500000000000000000",   // 0.5 ETH end price
		StartPrice:   "1000000000000000000",  // 1 ETH start
		StartTime:    uint64(now.Unix()),
		EndTime:      uint64(now.Add(24 * time.Hour).Unix()),
		Salt:         "12345",
		Extra:        "",
		Signature:    "0x" + string(make([]byte, 130)),
	}

	order, err := svc.Submit(req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if order.Kind != domain.DutchAuction {
		t.Fatalf("expected kind DutchAuction, got %d", order.Kind)
	}
}

func TestSubmit_InvalidSignature(t *testing.T) {
	repo := &mockOrderRepo{}
	sig := &mockSigVerifier{
		verifyFn: func(order *domain.Order) error {
			return fmt.Errorf("invalid signature")
		},
	}
	svc := newTestService(repo, sig)

	req := validFixedPriceRequest()
	_, err := svc.Submit(req)

	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Code != "ORDER_SIGNATURE_INVALID" {
		t.Fatalf("expected ORDER_SIGNATURE_INVALID, got %s", appErr.Code)
	}
	if repo.insertCalled {
		t.Fatal("expected Insert NOT to be called on signature error")
	}
}

func TestSubmit_InvalidSide(t *testing.T) {
	repo := &mockOrderRepo{}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	req := validFixedPriceRequest()
	req.Side = 2 // invalid

	_, err := svc.Submit(req)
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Code != "INVALID_SIDE" {
		t.Fatalf("expected INVALID_SIDE, got %s", appErr.Code)
	}
}

func TestSubmit_InvalidKind(t *testing.T) {
	repo := &mockOrderRepo{}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	req := validFixedPriceRequest()
	req.Kind = 5 // invalid

	_, err := svc.Submit(req)
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Code != "INVALID_KIND" {
		t.Fatalf("expected INVALID_KIND, got %s", appErr.Code)
	}
}

func TestSubmit_Expired_EndTime(t *testing.T) {
	repo := &mockOrderRepo{}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	req := validFixedPriceRequest()
	req.EndTime = uint64(time.Now().Add(-1 * time.Hour).Unix()) // 1 hour in the past

	_, err := svc.Submit(req)
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Code != "ORDER_EXPIRED" {
		t.Fatalf("expected ORDER_EXPIRED, got %s", appErr.Code)
	}
}

func TestSubmit_Expired_StartTime(t *testing.T) {
	repo := &mockOrderRepo{}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	req := validFixedPriceRequest()
	req.StartTime = uint64(time.Now().Add(-10 * time.Minute).Unix()) // 10 minutes ago (> 5 min)

	_, err := svc.Submit(req)
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Code != "ORDER_EXPIRED" {
		t.Fatalf("expected ORDER_EXPIRED, got %s", appErr.Code)
	}
}

func TestSubmit_InvalidDutchAuction_StartPrice(t *testing.T) {
	repo := &mockOrderRepo{}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	now := time.Now()
	req := &domain.SubmitOrderRequest{
		Maker:        "0x1234567890123456789012345678901234567890",
		Taker:        "",
		Side:         domain.Sell,
		Kind:         domain.DutchAuction,
		AssetType:    domain.ERC721,
		Collection:   "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		TokenID:      "1",
		Amount:       "1",
		PaymentToken: "",
		Price:        "1000000000000000000", // 1 ETH
		StartPrice:   "500000000000000000",  // 0.5 ETH — startPrice <= price!
		StartTime:    uint64(now.Unix()),
		EndTime:      uint64(now.Add(24 * time.Hour).Unix()),
		Salt:         "12345",
		Extra:        "",
		Signature:    "0x" + string(make([]byte, 130)),
	}

	_, err := svc.Submit(req)
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Code != "INVALID_DUTCH_AUCTION" {
		t.Fatalf("expected INVALID_DUTCH_AUCTION, got %s", appErr.Code)
	}
}

func TestSubmit_InvalidDutchAuction_Time(t *testing.T) {
	repo := &mockOrderRepo{}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	now := time.Now()
	req := &domain.SubmitOrderRequest{
		Maker:        "0x1234567890123456789012345678901234567890",
		Taker:        "",
		Side:         domain.Sell,
		Kind:         domain.DutchAuction,
		AssetType:    domain.ERC721,
		Collection:   "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		TokenID:      "1",
		Amount:       "1",
		PaymentToken: "",
		Price:        "500000000000000000",
		StartPrice:   "1000000000000000000",
		StartTime:    uint64(now.Add(1 * time.Hour).Unix()),   // future
		EndTime:      uint64(now.Add(30 * time.Minute).Unix()), // endTime <= startTime
		Salt:         "12345",
		Extra:        "",
		Signature:    "0x" + string(make([]byte, 130)),
	}

	_, err := svc.Submit(req)
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Code != "INVALID_DUTCH_AUCTION" {
		t.Fatalf("expected INVALID_DUTCH_AUCTION, got %s", appErr.Code)
	}
}

func TestSubmit_InvalidAmount(t *testing.T) {
	repo := &mockOrderRepo{}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	req := validFixedPriceRequest()
	req.Amount = "0"

	_, err := svc.Submit(req)
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Code != "INVALID_AMOUNT" {
		t.Fatalf("expected INVALID_AMOUNT, got %s", appErr.Code)
	}
}

func TestSubmit_InvalidPrice(t *testing.T) {
	repo := &mockOrderRepo{}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	req := validFixedPriceRequest()
	req.Price = "0"

	_, err := svc.Submit(req)
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Code != "INVALID_PRICE" {
		t.Fatalf("expected INVALID_PRICE, got %s", appErr.Code)
	}
}

func TestSubmit_InvalidMaker(t *testing.T) {
	repo := &mockOrderRepo{}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	req := validFixedPriceRequest()
	req.Maker = "0x0000000000000000000000000000000000000000"

	_, err := svc.Submit(req)
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Code != "INVALID_MAKER" {
		t.Fatalf("expected INVALID_MAKER, got %s", appErr.Code)
	}
}

func TestSubmit_InvalidTaker(t *testing.T) {
	repo := &mockOrderRepo{}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	req := validFixedPriceRequest()
	req.Taker = "not-an-address"

	_, err := svc.Submit(req)
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Code != "INVALID_TAKER" {
		t.Fatalf("expected INVALID_TAKER, got %s", appErr.Code)
	}
}

func TestSubmit_Inventory_InsertError(t *testing.T) {
	repo := &mockOrderRepo{
		insertFn: func(o *domain.Order) error {
			return fmt.Errorf("db error")
		},
	}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	req := validFixedPriceRequest()
	_, err := svc.Submit(req)
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Code != "ORDER_PERSIST_FAILED" {
		t.Fatalf("expected ORDER_PERSIST_FAILED, got %s", appErr.Code)
	}
}

// ---------- Find tests ----------

func TestFind_Success(t *testing.T) {
	expectedOrders := []domain.Order{
		{ID: 1, OrderHash: "0xabc"},
		{ID: 2, OrderHash: "0xdef"},
	}
	repo := &mockOrderRepo{
		findFn: func(filter domain.OrderFilter) ([]domain.Order, int64, error) {
			return expectedOrders, 2, nil
		},
	}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	orders, count, err := svc.Find(domain.OrderFilter{Collection: "0xabcdef"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected count 2, got %d", count)
	}
	if len(orders) != 2 {
		t.Fatalf("expected 2 orders, got %d", len(orders))
	}
	if orders[0].ID != 1 {
		t.Fatalf("expected first order ID 1, got %d", orders[0].ID)
	}
}

func TestFind_RepoError(t *testing.T) {
	repo := &mockOrderRepo{
		findFn: func(filter domain.OrderFilter) ([]domain.Order, int64, error) {
			return nil, 0, fmt.Errorf("db error")
		},
	}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	_, _, err := svc.Find(domain.OrderFilter{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------- GetByHash tests ----------

func TestGetByHash_Found(t *testing.T) {
	expected := &domain.Order{ID: 1, OrderHash: "0xabc"}
	repo := &mockOrderRepo{
		findHashFn: func(hash string) (*domain.Order, error) {
			return expected, nil
		},
	}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	order, err := svc.GetByHash("0xabc")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if order.ID != 1 {
		t.Fatalf("expected ID 1, got %d", order.ID)
	}
}

func TestGetByHash_NotFound(t *testing.T) {
	repo := &mockOrderRepo{
		findHashFn: func(hash string) (*domain.Order, error) {
			return nil, nil
		},
	}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	order, err := svc.GetByHash("0xnotfound")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if order != nil {
		t.Fatal("expected nil order for not found")
	}
}

// ---------- GetBest tests ----------

func TestGetBest_Success(t *testing.T) {
	price := big.NewInt(1000000000000000000)
	repo := &mockOrderRepo{
		bestPriceFn: func(collection string, side domain.OrderSide) (*big.Int, error) {
			return price, nil
		},
		findFn: func(filter domain.OrderFilter) ([]domain.Order, int64, error) {
			return []domain.Order{
				{
					ID:    1,
					Kind:  domain.FixedPrice,
					Price: &domain.BigInt{Int: price},
				},
			}, 1, nil
		},
	}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	order, err := svc.GetBest("0xabcdef", domain.Sell)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if order == nil {
		t.Fatal("expected order, got nil")
	}
	if order.ID != 1 {
		t.Fatalf("expected ID 1, got %d", order.ID)
	}
}

func TestGetBest_NoOrders(t *testing.T) {
	repo := &mockOrderRepo{
		bestPriceFn: func(collection string, side domain.OrderSide) (*big.Int, error) {
			return nil, nil
		},
	}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	order, err := svc.GetBest("0xabcdef", domain.Sell)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if order != nil {
		t.Fatal("expected nil when no orders exist")
	}
}

func TestGetBest_GetBestPriceError(t *testing.T) {
	repo := &mockOrderRepo{
		bestPriceFn: func(collection string, side domain.OrderSide) (*big.Int, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	_, err := svc.GetBest("0xabcdef", domain.Sell)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------- GetUserOrders tests ----------

func TestGetUserOrders_Success(t *testing.T) {
	expectedOrders := []domain.Order{
		{ID: 1, Maker: "0x1234567890123456789012345678901234567890"},
	}
	repo := &mockOrderRepo{
		findFn: func(filter domain.OrderFilter) ([]domain.Order, int64, error) {
			if filter.Maker != "0x1234567890123456789012345678901234567890" {
				return nil, 0, fmt.Errorf("unexpected maker")
			}
			if filter.Limit != 50 {
				return nil, 0, fmt.Errorf("expected limit 50")
			}
			return expectedOrders, 1, nil
		},
	}
	sig := &mockSigVerifier{}
	svc := newTestService(repo, sig)

	orders, err := svc.GetUserOrders("0x1234567890123456789012345678901234567890", nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(orders))
	}
}
