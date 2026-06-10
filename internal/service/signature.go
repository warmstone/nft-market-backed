package service

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"nft-market-backend/internal/domain"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
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

// ErrInvalidSignature is returned when ECDSA recovery doesn't match the maker.
var ErrInvalidSignature = fmt.Errorf("invalid signature: recovered signer does not match maker")

// SignatureService verifies EIP-712 typed data signatures.
type SignatureService struct {
	ChainID            *big.Int
	VerifyingContract  common.Address
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
// recovers the signer via ECDSA. Returns the EIP-712 typed data hash on success.
func (s *SignatureService) Verify(order *domain.Order) (string, error) {
	// Decode the 0x-prefixed hex signature.
	sig, err := hex.DecodeString(strings.TrimPrefix(order.Signature, "0x"))
	if err != nil {
		return "", fmt.Errorf("decode signature: %w", err)
	}
	if len(sig) != 65 {
		return "", fmt.Errorf("signature length %d, expected 65", len(sig))
	}

	// Normalize V: some signers (viem, ethers v5) return [27,28]
	// while go-ethereum's Ecrecover expects [0,1].
	if sig[64] >= 27 {
		sig[64] -= 27
	}

	// Enforce low-s (EIP-2).
	if sig[64] > 1 {
		return "", fmt.Errorf("invalid recovery id v: %d", sig[64])
	}
	if !crypto.ValidateSignatureValues(sig[64], new(big.Int).SetBytes(sig[0:32]), new(big.Int).SetBytes(sig[32:64]), false) {
		return "", fmt.Errorf("signature s value is not in low half")
	}

	domain := apitypes.TypedDataDomain{
		Name:              "NFTMarketExchange",
		Version:           "1",
		ChainId:           math.NewHexOrDecimal256(s.ChainID.Int64()),
		VerifyingContract: s.VerifyingContract.Hex(),
	}

	msg := s.buildMessage(order)
	typedData := apitypes.TypedData{
		Types:       OrderTypes,
		PrimaryType: "Order",
		Domain:      domain,
		Message:     msg,
	}

	hash, _, err := apitypes.TypedDataAndHash(typedData)
	if err != nil {
		return "", fmt.Errorf("typed data hash: %w", err)
	}

	// V has already been normalized to [0,1] above.

	pubKey, err := crypto.Ecrecover(hash, sig)
	if err != nil {
		return "", fmt.Errorf("ecrecover: %w", err)
	}

	recoveredAddr, err := crypto.UnmarshalPubkey(pubKey)
	if err != nil {
		return "", fmt.Errorf("unmarshal pubkey: %w", err)
	}

	maker := common.HexToAddress(order.Maker)
	if crypto.PubkeyToAddress(*recoveredAddr) != maker {
		return "", ErrInvalidSignature
	}

	return fmt.Sprintf("0x%x", hash), nil
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
