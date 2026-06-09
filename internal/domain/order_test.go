package domain

import (
	"math/big"
	"testing"
	"time"
)

func bigFromStr(s string) *BigInt {
	i := new(big.Int)
	i.SetString(s, 10)
	return &BigInt{Int: i}
}

// TestCurrentPrice_FixedPrice verifies that FixedPrice orders always return
// the static price regardless of time elapsed.
func TestCurrentPrice_FixedPrice(t *testing.T) {
	order := &Order{
		Kind:  FixedPrice,
		Price: bigFromStr("1000000000000000000"), // 1 ETH in wei
	}

	price := order.CurrentPrice(time.Now())
	if price.Cmp(order.Price.Int) != 0 {
		t.Errorf("FixedPrice order: expected %s, got %s", order.Price.Int.String(), price.String())
	}
}

// TestCurrentPrice_DutchAuction_Midpoint verifies linear decay reaches the
// midpoint between startPrice and price when half the duration has elapsed.
func TestCurrentPrice_DutchAuction_Midpoint(t *testing.T) {
	now := time.Now()
	startTime := uint64(now.Unix() - 3600) // 1 hour ago
	endTime := uint64(now.Unix() + 3600)   // 1 hour from now

	order := &Order{
		Kind:       DutchAuction,
		StartPrice: bigFromStr("2000000000000000000"), // 2 ETH
		Price:      bigFromStr("1000000000000000000"), // 1 ETH
		StartTime:  startTime,
		EndTime:    endTime,
	}

	price := order.CurrentPrice(now)

	// At midpoint, price should be 1.5 ETH = 1500000000000000000
	expected := bigFromStr("1500000000000000000")
	if price.Cmp(expected.Int) != 0 {
		t.Errorf("DutchAuction midpoint: expected %s, got %s", expected.Int.String(), price.String())
	}
}

// TestCurrentPrice_DutchAuction_End verifies the price equals the floor price
// when the auction has fully elapsed.
func TestCurrentPrice_DutchAuction_End(t *testing.T) {
	now := time.Now()
	startTime := uint64(now.Unix() - 7200) // 2 hours ago
	endTime := uint64(now.Unix() - 3600)  // 1 hour ago (ended)

	order := &Order{
		Kind:       DutchAuction,
		StartPrice: bigFromStr("5000000000000000000"), // 5 ETH
		Price:      bigFromStr("1000000000000000000"), // 1 ETH
		StartTime:  startTime,
		EndTime:    endTime,
	}

	price := order.CurrentPrice(now)

	// Auction has ended, should equal floor price
	if price.Cmp(order.Price.Int) != 0 {
		t.Errorf("DutchAuction at end: expected floor %s, got %s", order.Price.Int.String(), price.String())
	}
}

// TestCurrentPrice_DutchAuction_BeforeStart verifies that before the auction
// begins, the start price is returned.
func TestCurrentPrice_DutchAuction_BeforeStart(t *testing.T) {
	now := time.Now()
	order := &Order{
		Kind:       DutchAuction,
		StartPrice: bigFromStr("2000000000000000000"),
		Price:      bigFromStr("1000000000000000000"),
		StartTime:  uint64(now.Unix() + 3600), // 1 hour in future
		EndTime:    uint64(now.Unix() + 7200), // 2 hours in future
	}

	price := order.CurrentPrice(now)
	if price.Cmp(order.StartPrice.Int) != 0 {
		t.Errorf("DutchAuction before start: expected startPrice %s, got %s",
			order.StartPrice.Int.String(), price.String())
	}
}

// TestIsExpired_NeverExpires verifies that EndTime=0 is treated as "never expires".
func TestIsExpired_NeverExpires(t *testing.T) {
	order := &Order{
		EndTime: 0, // never expires
	}

	if order.IsExpired(time.Now()) {
		t.Error("Order with EndTime=0 should never be expired")
	}
}

// TestIsExpired_Future verifies a future EndTime is not expired.
func TestIsExpired_Future(t *testing.T) {
	order := &Order{
		EndTime: uint64(time.Now().Unix() + 3600), // 1 hour in future
	}

	if order.IsExpired(time.Now()) {
		t.Error("Order with future EndTime should not be expired")
	}
}

// TestIsExpired_Past verifies a past EndTime is expired.
func TestIsExpired_Past(t *testing.T) {
	order := &Order{
		EndTime: uint64(time.Now().Unix() - 3600), // 1 hour ago
	}

	if !order.IsExpired(time.Now()) {
		t.Error("Order with past EndTime should be expired")
	}
}

// TestBigIntJSON_Roundtrip verifies that BigInt marshals as a decimal string
// and unmarshals back correctly.
func TestBigIntJSON_Roundtrip(t *testing.T) {
	tests := []string{
		"0",
		"1000000000000000000",
		"115792089237316195423570985008687907853269984665640564039457584007913129639935",
	}

	for _, s := range tests {
		b := new(BigInt)
		if err := b.UnmarshalJSON([]byte(`"` + s + `"`)); err != nil {
			t.Fatalf("unmarshal %q: %v", s, err)
		}
		data, err := b.MarshalJSON()
		if err != nil {
			t.Fatalf("marshal %q: %v", s, err)
		}
		// MarshalJSON produces `"<decimal>"`; strip the quotes for comparison.
		got := string(data)
		want := `"` + s + `"`
		if got != want {
			t.Errorf("roundtrip %q: got %s, want %s", s, got, want)
		}
	}
}
