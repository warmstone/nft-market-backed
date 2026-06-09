package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nft-market-backend/internal/domain"

	"github.com/gin-gonic/gin"
)

// ---- mock implementations ----

type mockOrderService struct {
	submitFn      func(req *domain.SubmitOrderRequest) (*domain.Order, error)
	findFn        func(filter domain.OrderFilter) ([]domain.Order, int64, error)
	getByHashFn   func(hash string) (*domain.Order, error)
	getBestFn     func(collection string, side domain.OrderSide) (*domain.Order, error)
	userOrdersFn  func(maker string, status *domain.OrderStatus) ([]domain.Order, error)
}

func (m *mockOrderService) Submit(req *domain.SubmitOrderRequest) (*domain.Order, error) {
	return m.submitFn(req)
}

func (m *mockOrderService) Find(filter domain.OrderFilter) ([]domain.Order, int64, error) {
	return m.findFn(filter)
}

func (m *mockOrderService) GetByHash(hash string) (*domain.Order, error) {
	return m.getByHashFn(hash)
}

func (m *mockOrderService) GetBest(collection string, side domain.OrderSide) (*domain.Order, error) {
	return m.getBestFn(collection, side)
}

func (m *mockOrderService) GetUserOrders(maker string, status *domain.OrderStatus) ([]domain.Order, error) {
	return m.userOrdersFn(maker, status)
}

type mockMetadataService struct{}

func (m *mockMetadataService) Enqueue(collection, tokenID string) {}

// ---- helpers ----

func setupTestRouter(handler *OrderHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/orders", handler.Submit)
	r.GET("/orders", handler.List)
	r.GET("/orders/best", handler.Best)
	r.GET("/orders/:hash", handler.Get)
	r.GET("/users/:address/orders", handler.UserOrders)
	return r
}

func validSubmitBody() string {
	// Note: Gin's binding:"required" rejects zero values for uint types,
	// so we use non-zero enum values (1 = Buy, 1 = DutchAuction, 1 = ERC1155).
	return `{"maker":"0x1111111111111111111111111111111111111111","side":1,"kind":1,"assetType":1,"collection":"0x2222222222222222222222222222222222222222","tokenId":"1","amount":"1","price":"1000000000000000000","startTime":1,"salt":"12345","signature":"0x` + strings.Repeat("ab", 65) + `"}`
}

// ---- Submit tests ----

func TestOrderHandler_Submit_Success(t *testing.T) {
	mockSvc := &mockOrderService{
		submitFn: func(req *domain.SubmitOrderRequest) (*domain.Order, error) {
			return &domain.Order{
				OrderHash:  "0xabc123",
				Collection: "0x2222222222222222222222222222222222222222",
				TokenID:    domain.NewBigInt(nil),
				Status:     domain.Active,
			}, nil
		},
	}
	handler := &OrderHandler{
		orderSvc:    mockSvc,
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("POST", "/orders", strings.NewReader(validSubmitBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["orderHash"] != "0xabc123" {
		t.Errorf("expected orderHash 0xabc123, got %v", resp["orderHash"])
	}
	if resp["status"] != "active" {
		t.Errorf("expected status active, got %v", resp["status"])
	}
}

func TestOrderHandler_Submit_InvalidJSON(t *testing.T) {
	handler := &OrderHandler{
		orderSvc:    &mockOrderService{},
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("POST", "/orders", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestOrderHandler_Submit_ServiceAppError(t *testing.T) {
	mockSvc := &mockOrderService{
		submitFn: func(req *domain.SubmitOrderRequest) (*domain.Order, error) {
			return nil, domain.NewAppError("ORDER_SIGNATURE_INVALID", "bad signature", nil)
		},
	}
	handler := &OrderHandler{
		orderSvc:    mockSvc,
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("POST", "/orders", strings.NewReader(validSubmitBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	var resp domain.ErrorResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error != "ORDER_SIGNATURE_INVALID" {
		t.Errorf("expected ORDER_SIGNATURE_INVALID, got %s", resp.Error)
	}
}

func TestOrderHandler_Submit_ServiceGenericError(t *testing.T) {
	mockSvc := &mockOrderService{
		submitFn: func(req *domain.SubmitOrderRequest) (*domain.Order, error) {
			return nil, errors.New("internal error")
		},
	}
	handler := &OrderHandler{
		orderSvc:    mockSvc,
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("POST", "/orders", strings.NewReader(validSubmitBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	var resp domain.ErrorResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error != "INTERNAL_ERROR" {
		t.Errorf("expected INTERNAL_ERROR, got %s", resp.Error)
	}
}

func TestOrderHandler_Submit_ValidationError(t *testing.T) {
	handler := &OrderHandler{
		orderSvc:    &mockOrderService{},
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	// Missing required fields.
	body := `{"side":0}`
	req, _ := http.NewRequest("POST", "/orders", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ---- Get tests ----

func TestOrderHandler_Get_NotFound(t *testing.T) {
	mockSvc := &mockOrderService{
		getByHashFn: func(hash string) (*domain.Order, error) {
			return nil, nil
		},
	}
	handler := &OrderHandler{
		orderSvc:    mockSvc,
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/orders/0xnonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestOrderHandler_Get_Success(t *testing.T) {
	mockSvc := &mockOrderService{
		getByHashFn: func(hash string) (*domain.Order, error) {
			return &domain.Order{
				OrderHash: "0xabc123",
				Status:    domain.Active,
			}, nil
		},
	}
	handler := &OrderHandler{
		orderSvc:    mockSvc,
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/orders/0xabc123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestOrderHandler_Get_ServiceError(t *testing.T) {
	mockSvc := &mockOrderService{
		getByHashFn: func(hash string) (*domain.Order, error) {
			return nil, errors.New("internal error")
		},
	}
	handler := &OrderHandler{
		orderSvc:    mockSvc,
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/orders/0xabc123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// ---- List tests ----

func TestOrderHandler_List_InvalidFilter(t *testing.T) {
	mockSvc := &mockOrderService{
		findFn: func(filter domain.OrderFilter) ([]domain.Order, int64, error) {
			return nil, 0, nil
		},
	}
	handler := &OrderHandler{
		orderSvc:    mockSvc,
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/orders?kind=99", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestOrderHandler_List_Success(t *testing.T) {
	mockSvc := &mockOrderService{
		findFn: func(filter domain.OrderFilter) ([]domain.Order, int64, error) {
			return []domain.Order{
				{OrderHash: "0xabc123", Status: domain.Active},
			}, 1, nil
		},
	}
	handler := &OrderHandler{
		orderSvc:    mockSvc,
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/orders?collection=0x2222222222222222222222222222222222222222", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestOrderHandler_List_ServiceError(t *testing.T) {
	mockSvc := &mockOrderService{
		findFn: func(filter domain.OrderFilter) ([]domain.Order, int64, error) {
			return nil, 0, errors.New("internal error")
		},
	}
	handler := &OrderHandler{
		orderSvc:    mockSvc,
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/orders", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestOrderHandler_List_InvalidSide(t *testing.T) {
	handler := &OrderHandler{
		orderSvc:    &mockOrderService{},
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/orders?side=99", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestOrderHandler_List_InvalidStatus(t *testing.T) {
	handler := &OrderHandler{
		orderSvc:    &mockOrderService{},
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/orders?status=99", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestOrderHandler_List_InvalidMinPrice(t *testing.T) {
	handler := &OrderHandler{
		orderSvc:    &mockOrderService{},
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/orders?minPrice=notanumber", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ---- Best tests ----

func TestOrderHandler_Best_MissingCollection(t *testing.T) {
	handler := &OrderHandler{
		orderSvc:    &mockOrderService{},
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/orders/best", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestOrderHandler_Best_Success(t *testing.T) {
	mockSvc := &mockOrderService{
		getBestFn: func(collection string, side domain.OrderSide) (*domain.Order, error) {
			return &domain.Order{
				OrderHash: "0xbest001",
				Status:    domain.Active,
				Price:     domain.NewBigInt(nil),
			}, nil
		},
	}
	handler := &OrderHandler{
		orderSvc:    mockSvc,
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/orders/best?collection=0x2222222222222222222222222222222222222222", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestOrderHandler_Best_NotFound(t *testing.T) {
	mockSvc := &mockOrderService{
		getBestFn: func(collection string, side domain.OrderSide) (*domain.Order, error) {
			return nil, nil
		},
	}
	handler := &OrderHandler{
		orderSvc:    mockSvc,
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/orders/best?collection=0x2222222222222222222222222222222222222222", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestOrderHandler_Best_ServiceError(t *testing.T) {
	mockSvc := &mockOrderService{
		getBestFn: func(collection string, side domain.OrderSide) (*domain.Order, error) {
			return nil, errors.New("internal error")
		},
	}
	handler := &OrderHandler{
		orderSvc:    mockSvc,
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/orders/best?collection=0x2222222222222222222222222222222222222222", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// ---- UserOrders tests ----

func TestOrderHandler_UserOrders_Success(t *testing.T) {
	mockSvc := &mockOrderService{
		userOrdersFn: func(maker string, status *domain.OrderStatus) ([]domain.Order, error) {
			return []domain.Order{
				{OrderHash: "0xuser001", Status: domain.Active},
			}, nil
		},
	}
	handler := &OrderHandler{
		orderSvc:    mockSvc,
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/users/0xuser/orders", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestOrderHandler_UserOrders_InvalidStatus(t *testing.T) {
	handler := &OrderHandler{
		orderSvc:    &mockOrderService{},
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/users/0xuser/orders?status=99", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestOrderHandler_UserOrders_ServiceError(t *testing.T) {
	mockSvc := &mockOrderService{
		userOrdersFn: func(maker string, status *domain.OrderStatus) ([]domain.Order, error) {
			return nil, errors.New("internal error")
		},
	}
	handler := &OrderHandler{
		orderSvc:    mockSvc,
		metadataSvc: &mockMetadataService{},
	}
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/users/0xuser/orders", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}
