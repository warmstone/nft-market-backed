package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"nft-market-backend/internal/domain"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang-jwt/jwt/v5"
)

type AuthService struct {
	jwtSecret    []byte
	jwtExpiry    time.Duration
	challengeTTL time.Duration
	cache        *CacheService
}

func NewAuthService(cache *CacheService, jwtSecret string, jwtExpiry, challengeTTL time.Duration) *AuthService {
	return &AuthService{
		jwtSecret:    []byte(jwtSecret),
		jwtExpiry:    jwtExpiry,
		challengeTTL: challengeTTL,
		cache:        cache,
	}
}

type authNonceData struct {
	Nonce    string `json:"nonce"`
	IssuedAt string `json:"issuedAt"`
}

func (s *AuthService) GenerateChallenge(ctx context.Context, address string) (*domain.AuthChallenge, error) {
	if !common.IsHexAddress(address) {
		return nil, domain.NewAppError("INVALID_ADDRESS", "not a valid ethereum address", nil)
	}

	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	issuedAt := time.Now().UTC().Format(time.RFC3339)

	challenge := fmt.Sprintf("I am signing in to NFT Market.\n\nNonce: %s\nIssued At: %s", nonce, issuedAt)

	key := "auth:nonce:" + strings.ToLower(address)
	data := authNonceData{Nonce: nonce, IssuedAt: issuedAt}
	if err := s.cache.Set(ctx, key, &data, s.challengeTTL); err != nil {
		return nil, fmt.Errorf("store nonce: %w", err)
	}

	return &domain.AuthChallenge{
		Challenge: challenge,
		Nonce:     nonce,
		IssuedAt:  issuedAt,
	}, nil
}

func (s *AuthService) Login(ctx context.Context, address string, signature string) (*domain.LoginResponse, error) {
	if !common.IsHexAddress(address) {
		return nil, domain.NewAppError("INVALID_ADDRESS", "not a valid ethereum address", nil)
	}

	key := "auth:nonce:" + strings.ToLower(address)
	var data authNonceData
	if err := s.cache.Get(ctx, key, &data); err != nil {
		return nil, domain.NewAppError("INVALID_CHALLENGE", "nonce not found or expired", nil)
	}

	challenge := fmt.Sprintf("I am signing in to NFT Market.\n\nNonce: %s\nIssued At: %s", data.Nonce, data.IssuedAt)

	recovered, err := recoverPersonalSign(challenge, signature)
	if err != nil {
		return nil, domain.NewAppError("SIGNATURE_INVALID", "signature verification failed", err)
	}
	if !strings.EqualFold(recovered, address) {
		return nil, domain.NewAppError("SIGNATURE_MISMATCH", "recovered signer does not match", nil)
	}

	_ = s.cache.Del(ctx, key)

	now := time.Now()
	expiresAt := now.Add(s.jwtExpiry)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": strings.ToLower(address),
		"iat": now.Unix(),
		"exp": expiresAt.Unix(),
	})
	tokenStr, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("sign token: %w", err)
	}

	return &domain.LoginResponse{
		Token:     tokenStr,
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	}, nil
}

func (s *AuthService) ValidateToken(tokenStr string) (string, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return "", fmt.Errorf("parse token: %w", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", fmt.Errorf("invalid token")
	}
	sub, _ := claims["sub"].(string)
	return sub, nil
}

func recoverPersonalSign(message, sigHex string) (string, error) {
	sig, err := hex.DecodeString(strings.TrimPrefix(sigHex, "0x"))
	if err != nil {
		return "", fmt.Errorf("decode signature: %w", err)
	}
	if len(sig) != 65 {
		return "", fmt.Errorf("signature length %d, expected 65", len(sig))
	}

	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(message))
	hash := crypto.Keccak256Hash([]byte(prefix + message))

	sig[64] -= 27
	pubKey, err := crypto.Ecrecover(hash.Bytes(), sig)
	if err != nil {
		return "", fmt.Errorf("ecrecover: %w", err)
	}
	recoveredPub, err := crypto.UnmarshalPubkey(pubKey)
	if err != nil {
		return "", fmt.Errorf("unmarshal pubkey: %w", err)
	}
	return crypto.PubkeyToAddress(*recoveredPub).Hex(), nil
}
