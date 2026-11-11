package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kyambuthia/kikapu/internal/platform/httpx"
)

type Handler struct {
	productRepo ProductRepository
	stockRepo   StockRepository
	orderStore  OrderLister
}

type OrderLister interface {
	List(ctx context.Context, opts OrderListOptions) ([]Order, string, error)
	Get(ctx context.Context, id string) (*Order, error)
}

type Order struct {
	ID                string    `json:"id"`
	Number            string    `json:"number"`
	Status            string    `json:"status"`
	PaymentStatus     string    `json:"payment_status"`
	FulfillmentStatus string    `json:"fulfillment_status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type OrderListOptions struct {
	Status string
	From   time.Time
	To     time.Time
	Limit  int
	Cursor string
}

func NewHandler(productRepo ProductRepository, stockRepo StockRepository) *Handler {
	return &Handler{
		productRepo: productRepo,
		stockRepo:   stockRepo,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/admin/products", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.listProducts(w, r)
		case http.MethodPost:
			h.createProduct(w, r)
		default:
			httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", httpx.RequestID(r))
		}
	})
	mux.HandleFunc("/v1/admin/products/", h.handleProduct)
	mux.HandleFunc("/v1/admin/orders", h.listOrders)
	mux.HandleFunc("/v1/admin/orders/", h.getOrder)
}

func (h *Handler) listProducts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", httpx.RequestID(r))
		return
	}

	ctx := r.Context()

	status := ProductStatus(r.URL.Query().Get("status"))
	if status != "" && status != StatusDraft && status != StatusPublished && status != StatusArchived {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_status", "status must be draft, published, or archived", httpx.RequestID(r))
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := DefaultLimit
	if limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be a valid integer", httpx.RequestID(r))
			return
		}
		limit = parsedLimit
	}

	if limit < 1 || limit > MaxLimit {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 100", httpx.RequestID(r))
		return
	}

	cursor := r.URL.Query().Get("cursor")

	opts := ProductListOptions{
		Status: status,
		Limit:  limit,
		Cursor: cursor,
	}

	products, nextCursor, err := h.productRepo.List(ctx, opts)
	if err != nil {
		if errors.Is(err, ErrInvalidCursor) {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_cursor", "invalid cursor", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	response := map[string]interface{}{
		"data":        products,
		"next_cursor": nextCursor,
	}

	httpx.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) handleProduct(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if !strings.HasPrefix(path, "/v1/admin/products/") {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "not found", httpx.RequestID(r))
		return
	}

	id := strings.TrimPrefix(path, "/v1/admin/products/")
	if id == "" {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "not found", httpx.RequestID(r))
		return
	}

	parts := strings.SplitN(id, "/", 2)
	if len(parts) == 2 {
		if parts[1] == "stock" {
			h.updateStock(w, r, parts[0])
			return
		}
		httpx.WriteError(w, http.StatusNotFound, "not_found", "not found", httpx.RequestID(r))
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getProduct(w, r, id)
	case http.MethodPatch:
		h.updateProduct(w, r, id)
	case http.MethodDelete:
		h.deleteProduct(w, r, id)
	default:
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", httpx.RequestID(r))
	}
}

func (h *Handler) getProduct(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()

	product, err := h.productRepo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrProductNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "product_not_found", "product not found", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, product)
}

func (h *Handler) createProduct(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key header is required", httpx.RequestID(r))
		return
	}

	var input ProductInput
	if err := httpx.ReadJSON(r, &input); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body", httpx.RequestID(r))
		return
	}

	validationErr := ValidateProductInput(input)
	if validationErr != nil {
		h.writeValidationError(w, r, validationErr)
		return
	}

	ctx := r.Context()
	product, err := h.productRepo.Create(ctx, input, idempotencyKey)
	if err != nil {
		if errors.Is(err, ErrIdempotencyKeyRequired) {
			httpx.WriteError(w, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key header is required", httpx.RequestID(r))
			return
		}

		var validationErr *ValidationError
		if errors.As(err, &validationErr) {
			h.writeValidationError(w, r, validationErr)
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, product)
}

func (h *Handler) updateProduct(w http.ResponseWriter, r *http.Request, id string) {
	var input ProductInput
	if err := httpx.ReadJSON(r, &input); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body", httpx.RequestID(r))
		return
	}

	validationErr := ValidateProductInput(input)
	if validationErr != nil {
		h.writeValidationError(w, r, validationErr)
		return
	}

	ctx := r.Context()
	product, err := h.productRepo.Update(ctx, id, input)
	if err != nil {
		if errors.Is(err, ErrProductNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "product_not_found", "product not found", httpx.RequestID(r))
			return
		}

		var validationErr *ValidationError
		if errors.As(err, &validationErr) {
			h.writeValidationError(w, r, validationErr)
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, product)
}

func (h *Handler) deleteProduct(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()

	err := h.productRepo.Delete(ctx, id)
	if err != nil {
		if errors.Is(err, ErrProductNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "product_not_found", "product not found", httpx.RequestID(r))
			return
		}

		if errors.Is(err, ErrProductInUse) {
			httpx.WriteError(w, http.StatusConflict, "product_in_use", "cannot delete product with existing orders", httpx.RequestID(r))
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) updateStock(w http.ResponseWriter, r *http.Request, variantID string) {
	if r.Method != http.MethodPut {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", httpx.RequestID(r))
		return
	}

	var input struct {
		LocationID string `json:"location_id"`
		OnHand     int    `json:"on_hand"`
	}

	if err := httpx.ReadJSON(r, &input); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body", httpx.RequestID(r))
		return
	}

	if input.LocationID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "validation_error", "validation failed", httpx.RequestID(r))
		return
	}

	if input.OnHand < 0 {
		validationErr := &ValidationError{
			Message: "validation failed",
			Fields:  map[string]string{"on_hand": "on_hand cannot be negative"},
		}
		h.writeValidationError(w, r, validationErr)
		return
	}

	ctx := r.Context()

	currentLevel, err := h.stockRepo.GetLevel(ctx, variantID, input.LocationID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	if currentLevel != nil && input.OnHand < currentLevel.Reserved {
		httpx.WriteError(w, http.StatusBadRequest, "insufficient_stock",
			fmt.Sprintf("on_hand (%d) cannot be less than reserved (%d)", input.OnHand, currentLevel.Reserved),
			httpx.RequestID(r))
		return
	}

	update := StockUpdate{
		VariantID:  variantID,
		LocationID: input.LocationID,
		OnHand:     input.OnHand,
	}

	level, err := h.stockRepo.SetLevel(ctx, update)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, level)
}

func (h *Handler) listOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", httpx.RequestID(r))
		return
	}

	if h.orderStore == nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "order store not configured", httpx.RequestID(r))
		return
	}

	ctx := r.Context()

	opts := OrderListOptions{
		Status: r.URL.Query().Get("status"),
	}

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		from, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_from", "from must be a valid RFC3339 timestamp", httpx.RequestID(r))
			return
		}
		opts.From = from
	}

	if toStr := r.URL.Query().Get("to"); toStr != "" {
		to, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_to", "to must be a valid RFC3339 timestamp", httpx.RequestID(r))
			return
		}
		opts.To = to
	}

	limitStr := r.URL.Query().Get("limit")
	if limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be a valid integer", httpx.RequestID(r))
			return
		}
		opts.Limit = limit
	}

	opts.Cursor = r.URL.Query().Get("cursor")

	orders, nextCursor, err := h.orderStore.List(ctx, opts)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	response := map[string]interface{}{
		"data":        orders,
		"next_cursor": nextCursor,
	}

	httpx.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) getOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", httpx.RequestID(r))
		return
	}

	if h.orderStore == nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "order store not configured", httpx.RequestID(r))
		return
	}

	path := r.URL.Path
	if !strings.HasPrefix(path, "/v1/admin/orders/") {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "not found", httpx.RequestID(r))
		return
	}

	id := strings.TrimPrefix(path, "/v1/admin/orders/")
	if id == "" {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "not found", httpx.RequestID(r))
		return
	}

	ctx := r.Context()
	order, err := h.orderStore.Get(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteError(w, http.StatusNotFound, "order_not_found", "order not found", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, order)
}

func (h *Handler) writeValidationError(w http.ResponseWriter, r *http.Request, validationErr *ValidationError) {
	envelope := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    "validation_error",
			"message": validationErr.Message,
			"fields":  validationErr.Fields,
		},
	}

	httpx.WriteJSON(w, http.StatusBadRequest, envelope)
}
