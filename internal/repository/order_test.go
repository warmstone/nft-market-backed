//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"nft-market-backend/internal/domain"

	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupTestDB(t *testing.T) (*OrderRepo, func()) {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("nft_market_test"),
		postgres.WithUsername("app"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	migration, err := os.ReadFile("../../migrations/001_initial.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := db.ExecContext(ctx, string(migration)); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	repo := NewOrderRepo(db)
	repo.ChainID = 31337

	cleanup := func() {
		db.Close()
		pgContainer.Terminate(ctx)
	}
	return repo, cleanup
}

// makeTestOrder creates an order with fields derived from salt for uniqueness.
// The order hash is derived from the salt value, and the signature is a valid
// 130-hex-char placeholder (65 bytes: 32-byte r + 32-byte s + 1-byte v).
func makeTestOrder(salt int64) *domain.Order {
	saltStr := fmt.Sprintf("%064x", salt)
	return &domain.Order{
		OrderHash:    "0x" + saltStr,
		Maker:        "0x" + strings.Repeat("1a", 20),
		Taker:        "0x0000000000000000000000000000000000000000",
		Side:         domain.Sell,
		Kind:         domain.FixedPrice,
		AssetType:    domain.ERC721,
		Collection:   "0x" + strings.Repeat("2b", 20),
		TokenID:      domain.NewBigInt(big.NewInt(salt)),
		Amount:       domain.NewBigInt(big.NewInt(1)),
		PaymentToken: "0x0000000000000000000000000000000000000000",
		Price:        domain.NewBigInt(big.NewInt(salt * 100)),
		StartPrice:   domain.NewBigInt(big.NewInt(0)),
		StartTime:    0,
		EndTime:      0,
		Salt:         domain.NewBigInt(big.NewInt(salt)),
		Counter:      domain.NewBigInt(big.NewInt(0)),
		Extra:        "0x0000000000000000000000000000000000000000000000000000000000000000",
		Status:       domain.Active,
		// 130 hex chars = 65 bytes (r=32 + s=32 + v=1)
		Signature: "0x" + strings.Repeat("c", 130),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func TestOrderRepo_Insert(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	order := makeTestOrder(1)
	err := repo.Insert(order)
	if err != nil {
		t.Fatalf("insert order: %v", err)
	}

	found, err := repo.FindByHash(order.OrderHash)
	if err != nil {
		t.Fatalf("find by hash: %v", err)
	}
	if found == nil {
		t.Fatal("expected order not found")
	}
	if found.Maker != order.Maker {
		t.Errorf("maker mismatch: got %s want %s", found.Maker, order.Maker)
	}
	if found.Side != order.Side {
		t.Errorf("side mismatch: got %d want %d", found.Side, order.Side)
	}
}

func TestOrderRepo_Find_Filters(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	o1 := makeTestOrder(1)
	o1.Collection = "0x" + strings.Repeat("aa", 20)
	o1.Side = domain.Sell
	o1.Price = domain.NewBigInt(big.NewInt(100))

	o2 := makeTestOrder(2)
	o2.Collection = "0x" + strings.Repeat("bb", 20)
	o2.Side = domain.Buy
	o2.Price = domain.NewBigInt(big.NewInt(200))

	o3 := makeTestOrder(3)
	o3.Collection = "0x" + strings.Repeat("aa", 20)
	o3.Side = domain.Sell
	o3.Price = domain.NewBigInt(big.NewInt(150))

	for _, o := range []*domain.Order{o1, o2, o3} {
		if err := repo.Insert(o); err != nil {
			t.Fatalf("insert order: %v", err)
		}
	}

	// Filter by collection "aa" and side Sell.
	sellSide := domain.Sell
	orders, total, err := repo.Find(domain.OrderFilter{
		Collection: "0x" + strings.Repeat("aa", 20),
		Side:       &sellSide,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("find orders: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2 orders, got %d", total)
	}
	if len(orders) != 2 {
		t.Errorf("expected 2 orders, got %d", len(orders))
	}
}

func TestOrderRepo_UpdateStatus(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	order := makeTestOrder(10)
	if err := repo.Insert(order); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := repo.UpdateStatus(order.OrderHash, domain.Filled); err != nil {
		t.Fatalf("update status: %v", err)
	}

	found, err := repo.FindByHash(order.OrderHash)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.Status != domain.Filled {
		t.Errorf("expected Filled, got %d", found.Status)
	}

	// Second update should be a no-op since only Active->X transitions work.
	if err := repo.UpdateStatus(order.OrderHash, domain.Cancelled); err != nil {
		t.Fatalf("second update: %v", err)
	}
	found, err = repo.FindByHash(order.OrderHash)
	if err != nil {
		t.Fatalf("find after second update: %v", err)
	}
	if found.Status != domain.Filled {
		t.Errorf("expected still Filled after second update, got %d", found.Status)
	}
}

func TestOrderRepo_ExpireOrders(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	order := makeTestOrder(20)
	order.EndTime = uint64(time.Now().Add(-1 * time.Hour).Unix()) // expired 1 hour ago
	if err := repo.Insert(order); err != nil {
		t.Fatalf("insert: %v", err)
	}

	n, err := repo.ExpireOrders()
	if err != nil {
		t.Fatalf("expire orders: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 expired, got %d", n)
	}

	found, err := repo.FindByHash(order.OrderHash)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.Status != domain.Expired {
		t.Errorf("expected Expired status, got %d", found.Status)
	}
}

func TestOrderRepo_GetBestPrice(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	collection := "0x" + strings.Repeat("dd", 20)

	// Insert two sell orders with different prices.
	o1 := makeTestOrder(100)
	o1.Collection = collection
	o1.Side = domain.Sell
	o1.Price = domain.NewBigInt(big.NewInt(500))

	o2 := makeTestOrder(101)
	o2.Collection = collection
	o2.Side = domain.Sell
	o2.Price = domain.NewBigInt(big.NewInt(300))

	for _, o := range []*domain.Order{o1, o2} {
		if err := repo.Insert(o); err != nil {
			t.Fatalf("insert order: %v", err)
		}
	}

	// Best sell price = lowest price = 300.
	bestPrice, err := repo.GetBestPrice(collection, domain.Sell)
	if err != nil {
		t.Fatalf("get best price: %v", err)
	}
	if bestPrice == nil {
		t.Fatal("expected best price not nil")
	}
	if bestPrice.Int64() != 300 {
		t.Errorf("expected best sell price 300, got %d", bestPrice.Int64())
	}

	// Best buy price for this collection = none (no buy orders).
	bestBuy, err := repo.GetBestPrice(collection, domain.Buy)
	if err != nil {
		t.Fatalf("get best price: %v", err)
	}
	if bestBuy != nil {
		t.Errorf("expected nil best buy price, got %d", bestBuy.Int64())
	}
}

func TestOrderRepo_GetListedCount(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	collection := "0x" + strings.Repeat("ee", 20)

	// Sell order -> counted.
	o1 := makeTestOrder(200)
	o1.Collection = collection
	o1.Side = domain.Sell

	// Buy order -> not counted.
	o2 := makeTestOrder(201)
	o2.Collection = collection
	o2.Side = domain.Buy

	for _, o := range []*domain.Order{o1, o2} {
		if err := repo.Insert(o); err != nil {
			t.Fatalf("insert order: %v", err)
		}
	}

	count, err := repo.GetListedCount(collection)
	if err != nil {
		t.Fatalf("get listed count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected listed count 1, got %d", count)
	}
}

func TestOrderRepo_GetActiveMakerCount(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	// Two orders from same maker should count as 1.
	o1 := makeTestOrder(300)
	o1.Maker = "0x" + strings.Repeat("ff", 20)

	o2 := makeTestOrder(301)
	o2.Maker = "0x" + strings.Repeat("ff", 20)

	for _, o := range []*domain.Order{o1, o2} {
		if err := repo.Insert(o); err != nil {
			t.Fatalf("insert order: %v", err)
		}
	}

	count, err := repo.GetActiveMakerCount()
	if err != nil {
		t.Fatalf("get active maker count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected active maker count 1, got %d", count)
	}
}

func TestOrderRepo_FindByHash_NotFound(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	found, err := repo.FindByHash("0x" + strings.Repeat("00", 32))
	if err != nil {
		t.Fatalf("find by hash: %v", err)
	}
	if found != nil {
		t.Errorf("expected nil for non-existing order, got %+v", found)
	}
}

func TestOrderRepo_CancelByMakerSalt(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	order := makeTestOrder(400)
	if err := repo.Insert(order); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := repo.CancelByMakerSalt(order.Maker, order.Salt.Int); err != nil {
		t.Fatalf("cancel by maker+salt: %v", err)
	}

	found, err := repo.FindByHash(order.OrderHash)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.Status != domain.Cancelled {
		t.Errorf("expected Cancelled, got %d", found.Status)
	}
}

func TestOrderRepo_CancelByMakerCounter(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	// Order with counter 0.
	o1 := makeTestOrder(500)
	o1.Counter = domain.NewBigInt(big.NewInt(0))

	// Order with counter 10.
	o2 := makeTestOrder(501)
	o2.Counter = domain.NewBigInt(big.NewInt(10))

	for _, o := range []*domain.Order{o1, o2} {
		if err := repo.Insert(o); err != nil {
			t.Fatalf("insert order: %v", err)
		}
	}

	// Cancel orders with counter < 5.
	if err := repo.CancelByMakerCounter(o1.Maker, big.NewInt(5)); err != nil {
		t.Fatalf("cancel by maker+counter: %v", err)
	}

	// o1 (counter 0) should be cancelled.
	found1, _ := repo.FindByHash(o1.OrderHash)
	if found1.Status != domain.Cancelled {
		t.Errorf("o1 expected Cancelled, got %d", found1.Status)
	}

	// o2 (counter 10) should still be active.
	found2, _ := repo.FindByHash(o2.OrderHash)
	if found2.Status != domain.Active {
		t.Errorf("o2 expected Active, got %d", found2.Status)
	}
}

func TestOrderRepo_CancelByCollection(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	collection := "0x" + strings.Repeat("gg", 20)

	o1 := makeTestOrder(600)
	o1.Collection = collection

	o2 := makeTestOrder(601)
	o2.Collection = "0x" + strings.Repeat("hh", 20)

	for _, o := range []*domain.Order{o1, o2} {
		if err := repo.Insert(o); err != nil {
			t.Fatalf("insert order: %v", err)
		}
	}

	if err := repo.CancelByCollection(collection); err != nil {
		t.Fatalf("cancel by collection: %v", err)
	}

	// o1 should be cancelled.
	found1, _ := repo.FindByHash(o1.OrderHash)
	if found1.Status != domain.Cancelled {
		t.Errorf("o1 expected Cancelled, got %d", found1.Status)
	}

	// o2 should still be active.
	found2, _ := repo.FindByHash(o2.OrderHash)
	if found2.Status != domain.Active {
		t.Errorf("o2 expected Active, got %d", found2.Status)
	}
}
