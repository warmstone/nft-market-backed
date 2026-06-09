package service

import (
	"encoding/hex"
	"math/big"
	"testing"

	"nft-market-backend/internal/domain"

	"github.com/ethereum/go-ethereum/crypto"
)

func testBigInt(s string) *domain.BigInt {
	i := new(big.Int)
	i.SetString(s, 10)
	return &domain.BigInt{Int: i}
}

func testOrder(maker string) *domain.Order {
	return &domain.Order{
		Maker:        maker,
		Taker:        "0x0000000000000000000000000000000000000000",
		Side:         domain.Sell,
		Kind:         domain.FixedPrice,
		AssetType:    domain.ERC721,
		Collection:   "0x2222222222222222222222222222222222222222",
		TokenID:      testBigInt("42"),
		Amount:       testBigInt("1"),
		PaymentToken: "0x0000000000000000000000000000000000000000",
		Price:        testBigInt("1000000000000000000"),
		StartPrice:   testBigInt("1000000000000000000"),
		StartTime:    1710000000,
		EndTime:      0,
		Salt:         testBigInt("123456789"),
		Counter:      testBigInt("0"),
		Extra:        "0x0000000000000000000000000000000000000000000000000000000000000000",
	}
}

func TestSignatureServiceVerifyAcceptsCanonicalAndEthereumV(t *testing.T) {
	key, err := crypto.HexToECDSA("4f3edf983ac636a65a842ce7c78d9aa706d3b113bce036f653d21f5077a9f8c9")
	if err != nil {
		t.Fatalf("key: %v", err)
	}

	svc := NewSignatureService(31337, "0x1111111111111111111111111111111111111111")
	order := testOrder(crypto.PubkeyToAddress(key.PublicKey).Hex())
	digest, err := svc.TypedDataHash(order)
	if err != nil {
		t.Fatalf("typed data hash: %v", err)
	}

	sig, err := crypto.Sign(digest, key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	order.Signature = "0x" + hex.EncodeToString(sig)
	if err := svc.Verify(order); err != nil {
		t.Fatalf("verify canonical v: %v", err)
	}

	sig[64] += 27
	order.Signature = "0x" + hex.EncodeToString(sig)
	if err := svc.Verify(order); err != nil {
		t.Fatalf("verify ethereum v: %v", err)
	}
}

func TestOrderStructHashStable(t *testing.T) {
	order := testOrder("0xa5Ad3E0385d6506CC27dFCd8631F1D08A0717b88")
	hash, err := OrderStructHash(order)
	if err != nil {
		t.Fatalf("order struct hash: %v", err)
	}

	const want = "0x8d001e4999b650c4066b96c67bcd48ce73c8b62ea2c16d30f1bb75b0bc04192d"
	if hash.Hex() != want {
		t.Fatalf("hash mismatch: got %s want %s", hash.Hex(), want)
	}
}
