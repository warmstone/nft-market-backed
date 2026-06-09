package service

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"nft-market-backend/internal/domain"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// OrderTypes defines the EIP-712 typed data structure matching the Solidity
// contract's ORDER_TYPEHASH exactly. uint128 and uint64 are used as distinct
// type names — do not replace them with uint256.
var OrderTypes = apitypes.Types{
	"EIP712Domain": {
		{Name: "name", Type: "string"},
		{Name: "version", Type: "string"},
		{Name: "chainId", Type: "uint256"},
		{Name: "verifyingContract", Type: "address"},
	},
	"Order": {
		{Name: "maker", Type: "address"},
		{Name: "taker", Type: "address"},
		{Name: "side", Type: "uint8"},
		{Name: "kind", Type: "uint8"},
		{Name: "assetType", Type: "uint8"},
		{Name: "collection", Type: "address"},
		{Name: "tokenId", Type: "uint256"},
		{Name: "amount", Type: "uint256"},
		{Name: "paymentToken", Type: "address"},
		{Name: "price", Type: "uint128"},
		{Name: "startPrice", Type: "uint128"},
		{Name: "startTime", Type: "uint64"},
		{Name: "endTime", Type: "uint64"},
		{Name: "salt", Type: "uint256"},
		{Name: "counter", Type: "uint256"},
		{Name: "extra", Type: "bytes32"},
	},
}

var orderTypeHash = crypto.Keccak256Hash([]byte(
	"Order(address maker,address taker,uint8 side,uint8 kind,uint8 assetType,address collection,uint256 tokenId,uint256 amount,address paymentToken,uint128 price,uint128 startPrice,uint64 startTime,uint64 endTime,uint256 salt,uint256 counter,bytes32 extra)",
))

// ErrInvalidSignature is returned when ECDSA recovery doesn't match the maker.
var ErrInvalidSignature = fmt.Errorf("invalid signature: recovered signer does not match maker")

// SignatureService verifies EIP-712 typed data signatures.
type SignatureService struct {
	ChainID           *big.Int
	VerifyingContract common.Address
}

// NewSignatureService creates a SignatureService.
func NewSignatureService(chainID int64, verifyingContract string) *SignatureService {
	return &SignatureService{
		ChainID:           big.NewInt(chainID),
		VerifyingContract: common.HexToAddress(verifyingContract),
	}
}

// Verify checks that the EIP-712 signature on an order was produced by the
// order's maker. It enforces low-s (ECDSA malleability prevention) and
// recovers the signer via ECDSA.
func (s *SignatureService) Verify(order *domain.Order) error {
	// Decode the 0x-prefixed hex signature.
	sig, err := hex.DecodeString(strings.TrimPrefix(order.Signature, "0x"))
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if len(sig) != 65 {
		return fmt.Errorf("signature length %d, expected 65", len(sig))
	}

	recoveryID, err := normalizeRecoveryID(sig[64])
	if err != nil {
		return err
	}
	if !crypto.ValidateSignatureValues(recoveryID, new(big.Int).SetBytes(sig[0:32]), new(big.Int).SetBytes(sig[32:64]), false) {
		return fmt.Errorf("signature s value is not in low half")
	}

	hash, err := s.TypedDataHash(order)
	if err != nil {
		return fmt.Errorf("typed data hash: %w", err)
	}

	sig[64] = recoveryID

	pubKey, err := crypto.Ecrecover(hash, sig)
	if err != nil {
		return fmt.Errorf("ecrecover: %w", err)
	}

	recoveredAddr, err := crypto.UnmarshalPubkey(pubKey)
	if err != nil {
		return fmt.Errorf("unmarshal pubkey: %w", err)
	}

	maker := common.HexToAddress(order.Maker)
	if crypto.PubkeyToAddress(*recoveredAddr) != maker {
		return ErrInvalidSignature
	}

	return nil
}

// TypedDataHash returns the EIP-712 digest that wallets sign and the contract
// verifies: keccak256("\x19\x01" || domainSeparator || LibOrder.hash(order)).
func (s *SignatureService) TypedDataHash(order *domain.Order) ([]byte, error) {
	structHash, err := OrderStructHash(order)
	if err != nil {
		return nil, err
	}

	domainSeparator := crypto.Keccak256(
		bytes32(crypto.Keccak256Hash([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))),
		bytes32(crypto.Keccak256Hash([]byte("NFTMarketExchange"))),
		bytes32(crypto.Keccak256Hash([]byte("1"))),
		uint256Bytes(s.ChainID),
		addressBytes(s.VerifyingContract),
	)

	return crypto.Keccak256(
		[]byte{0x19, 0x01},
		domainSeparator,
		structHash.Bytes(),
	), nil
}

// OrderStructHash mirrors Solidity LibOrder.hash(order).
func OrderStructHash(order *domain.Order) (common.Hash, error) {
	addressT, _ := abi.NewType("address", "", nil)
	uint8T, _ := abi.NewType("uint8", "", nil)
	uint64T, _ := abi.NewType("uint64", "", nil)
	uint128T, _ := abi.NewType("uint128", "", nil)
	uint256T, _ := abi.NewType("uint256", "", nil)
	bytes32T, _ := abi.NewType("bytes32", "", nil)

	args := abi.Arguments{
		{Type: bytes32T},
		{Type: addressT},
		{Type: addressT},
		{Type: uint8T},
		{Type: uint8T},
		{Type: uint8T},
		{Type: addressT},
		{Type: uint256T},
		{Type: uint256T},
		{Type: addressT},
		{Type: uint128T},
		{Type: uint128T},
		{Type: uint64T},
		{Type: uint64T},
		{Type: uint256T},
		{Type: uint256T},
		{Type: bytes32T},
	}

	extra, err := hexToBytes32(defaultExtra(order.Extra))
	if err != nil {
		return common.Hash{}, fmt.Errorf("extra: %w", err)
	}

	packed, err := args.Pack(
		orderTypeHash,
		common.HexToAddress(order.Maker),
		common.HexToAddress(defaultAddress(order.Taker)),
		uint8(order.Side),
		uint8(order.Kind),
		uint8(order.AssetType),
		common.HexToAddress(order.Collection),
		bigIntOrZero(order.TokenID),
		bigIntOrZero(order.Amount),
		common.HexToAddress(defaultAddress(order.PaymentToken)),
		bigIntOrZero(order.Price),
		bigIntOrZero(order.StartPrice),
		order.StartTime,
		order.EndTime,
		bigIntOrZero(order.Salt),
		bigIntOrZero(order.Counter),
		extra,
	)
	if err != nil {
		return common.Hash{}, err
	}

	return crypto.Keccak256Hash(packed), nil
}

// buildMessage converts a domain.Order into the TypedDataMessage expected by go-ethereum.
func (s *SignatureService) buildMessage(order *domain.Order) apitypes.TypedDataMessage {
	taker := order.Taker
	if taker == "" {
		taker = "0x0000000000000000000000000000000000000000"
	}
	paymentToken := order.PaymentToken
	if paymentToken == "" {
		paymentToken = "0x0000000000000000000000000000000000000000"
	}
	extra := order.Extra
	if extra == "" {
		extra = "0x0000000000000000000000000000000000000000000000000000000000000000"
	}

	return apitypes.TypedDataMessage{
		"maker":        order.Maker,
		"taker":        taker,
		"side":         fmt.Sprintf("%d", order.Side),
		"kind":         fmt.Sprintf("%d", order.Kind),
		"assetType":    fmt.Sprintf("%d", order.AssetType),
		"collection":   order.Collection,
		"tokenId":      order.TokenID.Int,
		"amount":       order.Amount.Int,
		"paymentToken": paymentToken,
		"price":        order.Price.Int,
		"startPrice":   order.StartPrice.Int,
		"startTime":    fmt.Sprintf("%d", order.StartTime),
		"endTime":      fmt.Sprintf("%d", order.EndTime),
		"salt":         order.Salt.Int,
		"counter":      order.Counter.Int,
		"extra":        extra,
	}
}

func normalizeRecoveryID(v byte) (byte, error) {
	switch v {
	case 0, 1:
		return v, nil
	case 27, 28:
		return v - 27, nil
	default:
		return 0, fmt.Errorf("invalid recovery id v: %d", v)
	}
}

func defaultAddress(s string) string {
	if s == "" {
		return "0x0000000000000000000000000000000000000000"
	}
	return s
}

func defaultExtra(s string) string {
	if s == "" {
		return "0x0000000000000000000000000000000000000000000000000000000000000000"
	}
	return s
}

func bigIntOrZero(b *domain.BigInt) *big.Int {
	if b == nil || b.Int == nil {
		return new(big.Int)
	}
	return b.Int
}

func hexToBytes32(s string) ([32]byte, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		return [32]byte{}, err
	}
	if len(raw) != 32 {
		return [32]byte{}, fmt.Errorf("expected 32 bytes, got %d", len(raw))
	}
	var out [32]byte
	copy(out[:], raw)
	return out, nil
}

func bytes32(h common.Hash) []byte {
	return h.Bytes()
}

func uint256Bytes(v *big.Int) []byte {
	return common.LeftPadBytes(v.Bytes(), 32)
}

func addressBytes(a common.Address) []byte {
	return common.LeftPadBytes(a.Bytes(), 32)
}
