package handler

import (
	"net/http"

	"nft-market-backend/internal/domain"
	"nft-market-backend/internal/service"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	authSvc *service.AuthService
}

func NewAuthHandler(authSvc *service.AuthService) *AuthHandler {
	return &AuthHandler{authSvc: authSvc}
}

func (h *AuthHandler) Challenge(c *gin.Context) {
	address := c.Query("address")
	if address == "" {
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{
			Error:   "MISSING_PARAM",
			Message: "address query parameter is required",
		})
		return
	}

	challenge, err := h.authSvc.GenerateChallenge(c.Request.Context(), address)
	if err != nil {
		code := extractErrorCode(err.Error())
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{Error: code, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, challenge)
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req domain.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{
			Error:   "INVALID_REQUEST",
			Message: err.Error(),
		})
		return
	}

	resp, err := h.authSvc.Login(c.Request.Context(), req.Address, req.Signature)
	if err != nil {
		code := extractErrorCode(err.Error())
		status := http.StatusBadRequest
		if code == "SIGNATURE_INVALID" || code == "SIGNATURE_MISMATCH" {
			status = http.StatusUnauthorized
		}
		c.JSON(status, domain.ErrorResponse{Error: code, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}
