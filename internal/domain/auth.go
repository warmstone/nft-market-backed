package domain

type AuthChallenge struct {
	Challenge string `json:"challenge"`
	Nonce     string `json:"nonce"`
	IssuedAt  string `json:"issuedAt"`
}

type LoginRequest struct {
	Address   string `json:"address" binding:"required"`
	Signature string `json:"signature" binding:"required"`
}

type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}
