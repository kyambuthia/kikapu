package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/order"
	"github.com/kyambuthia/kikapu/internal/platform/httpx"
)

type Handler struct {
	products    ProductRepository
	inventory   InventoryRepository
	orders      OrderRepository
	idempotency IdempotencyStore
}

func NewHandler(products ProductRepository, inventory InventoryRepository, orders OrderRepository) *Handler {
	return &Handler{
		products:    products,
		inventory:   inventory,
		orders:      orders,
		idempotency: NewInMemoryIdempotencyStore(),
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/admin/products", h.listProducts)
	mux.HandleFunc("/v1/admin/products/", h.handleProduct)
	mux.HandleFunc("/v1/admin/orders", h.listOrders)
	mux.HandleFunc("/v1/admin/orders/", h.handleOrder)
}

func (h *Handler) listProducts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", httpx.RequestID(r))
		return
	}

	if r.Method == http.MethodPost {
		h.createProduct(w, r)
		return
	}

	ctx := r.Context()

	status := r.URL.Query().Get("status")
	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be a valid integer", httpx.RequestID(r))
			return
		}
		limit = parsedLimit
	}

	if limit < 1 || limit > 100 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 100", httpx.RequestID(r))
		return
	}

	products, nextCursor, err := h.products.List(ctx, status, cursor, limit)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	data := make([]ProductResponse, len(products))
	for i, p := range products {
		data[i] = toProductResponse(p)
	}

	httpx.WriteJSON(w, http.StatusOK, ProductListResponse{
		Data:       data,
		NextCursor: nextCursor,
	})
}

func (h *Handler) createProduct(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key header is required", httpx.RequestID(r))
		return
	}

	if cached, ok := h.idempotency.Get(idempotencyKey); ok {
		httpx.WriteJSON(w, http.StatusCreated, cached)
		return
	}

	var input ProductInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid request body", httpx.RequestID(r))
		return
	}

	if err := ValidateProductInput(input); err != nil {
		WriteValidationError(w, r, err)
		return
	}

	ctx := r.Context()

	existing, _ := h.products.GetBySlug(ctx, input.Slug)
	if existing != nil {
		httpx.WriteError(w, http.StatusConflict, "duplicate_slug", "product with this slug already exists", httpx.RequestID(r))
		return
	}

	product := ToProduct(input, generateProductID())
	if err := h.products.Create(ctx, product); err != nil {
		if errors.Is(err, ErrDuplicateSlug) {
			httpx.WriteError(w, http.StatusConflict, "duplicate_slug", "product with this slug already exists", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create product", httpx.RequestID(r))
		return
	}

	resp := toProductResponse(*product)
	h.idempotency.Set(idempotencyKey, resp)
	httpx.WriteJSON(w, http.StatusCreated, resp)
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
	productID := parts[0]

	switch r.Method {
	case http.MethodGet:
		h.getProduct(w, r, productID)
	case http.MethodPatch:
		h.updateProduct(w, r, productID)
	case http.MethodDelete:
		h.deleteProduct(w, r, productID)
	case http.MethodPut:
		if len(parts) == 2 && parts[1] == "stock" {
			h.updateStock(w, r, productID)
		} else {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "not found", httpx.RequestID(r))
		}
	default:
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", httpx.RequestID(r))
	}
}

func (h *Handler) getProduct(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()

	product, err := h.products.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrProductNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "product_not_found", "product not found", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toProductResponse(*product))
}

func (h *Handler) updateProduct(w http.ResponseWriter, r *http.Request, id string) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key header is required", httpx.RequestID(r))
		return
	}

	ctx := r.Context()

	existing, err := h.products.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrProductNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "product_not_found", "product not found", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	var input ProductInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid request body", httpx.RequestID(r))
		return
	}

	if input.Name != "" {
		existing.Name = input.Name
	}
	if input.Slug != "" {
		if err := ValidateSlug(input.Slug); err != nil {
			WriteValidationError(w, r, err)
			return
		}
		existing.Slug = input.Slug
	}
	if input.Status != "" {
		if err := ValidateStatus(input.Status); err != nil {
			WriteValidationError(w, r, err)
			return
		}
		existing.Status = input.Status
	}
	if len(input.Variants) > 0 {
		for _, v := range input.Variants {
			if err := ValidatePrice(v.Price); err != nil {
				WriteValidationError(w, r, err)
				return
			}
			if err := ValidateCurrency(v.Currency); err != nil {
				WriteValidationError(w, r, err)
				return
			}
		}
		existing.Variants = make([]catalog.Variant, len(input.Variants))
		for i, v := range input.Variants {
			existing.Variants[i] = catalog.Variant{
				ID:       generateVariantID(),
				SKU:      v.SKU,
				Price:    existing.Variants[0].Price,
				Currency: v.Currency,
			}
			existing.Variants[i].Price.Value = v.Price
		}
	}

	if err := h.products.Update(ctx, existing); err != nil {
		if errors.Is(err, ErrDuplicateSlug) {
			httpx.WriteError(w, http.StatusConflict, "duplicate_slug", "product with this slug already exists", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to update product", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toProductResponse(*existing))
}

func (h *Handler) deleteProduct(w http.ResponseWriter, r *http.Request, id string) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key header is required", httpx.RequestID(r))
		return
	}

	ctx := r.Context()

	hasOrders, err := h.products.HasOrders(ctx, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to check product orders", httpx.RequestID(r))
		return
	}

	if hasOrders {
		httpx.WriteError(w, http.StatusConflict, "product_in_use", "cannot delete product with existing orders", httpx.RequestID(r))
		return
	}

	if err := h.products.Delete(ctx, id); err != nil {
		if errors.Is(err, ErrProductNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "product_not_found", "product not found", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to delete product", httpx.RequestID(r))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) updateStock(w http.ResponseWriter, r *http.Request, productID string) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key header is required", httpx.RequestID(r))
		return
	}

	ctx := r.Context()

	product, err := h.products.GetByID(ctx, productID)
	if err != nil {
		if errors.Is(err, ErrProductNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "product_not_found", "product not found", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	var input struct {
		VariantID  string `json:"variant_id"`
		LocationID string `json:"location_id"`
		OnHand     int    `json:"on_hand"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid request body", httpx.RequestID(r))
		return
	}

	if input.VariantID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_variant_id", "variant_id is required", httpx.RequestID(r))
		return
	}

	if err := ValidateStockUpdate(StockUpdate{LocationID: input.LocationID, OnHand: input.OnHand}); err != nil {
		WriteValidationError(w, r, err)
		return
	}

	var found bool
	for _, v := range product.Variants {
		if v.ID == input.VariantID {
			found = true
			break
		}
	}

	if !found {
		httpx.WriteError(w, http.StatusNotFound, "variant_not_found", "variant not found in product", httpx.RequestID(r))
		return
	}

	if err := h.inventory.SetLevel(ctx, input.VariantID, input.LocationID, input.OnHand); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to update stock", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"variant_id":  input.VariantID,
		"location_id": input.LocationID,
		"on_hand":     input.OnHand,
	})
}

type ProductListResponse struct {
	Data       []ProductResponse `json:"data"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

type ProductResponse struct {
	ID       string            `json:"id"`
	Slug     string            `json:"slug"`
	Name     string            `json:"name"`
	Status   string            `json:"status"`
	Variants []VariantResponse `json:"variants"`
}

type VariantResponse struct {
	ID       string `json:"id"`
	SKU      string `json:"sku"`
	Price    int64  `json:"price"`
	Currency string `json:"currency"`
}

func toProductResponse(p catalog.Product) ProductResponse {
	variants := make([]VariantResponse, len(p.Variants))
	for i, v := range p.Variants {
		variants[i] = VariantResponse{
			ID:       v.ID,
			SKU:      v.SKU,
			Price:    v.Price.Value,
			Currency: v.Price.Currency,
		}
	}
	return ProductResponse{
		ID:       p.ID,
		Slug:     p.Slug,
		Name:     p.Name,
		Status:   p.Status,
		Variants: variants,
	}
}

func WriteValidationError(w http.ResponseWriter, r *http.Request, err *ValidationError) {
	envelope := struct {
		Error struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Fields  map[string]string `json:"fields,omitempty"`
		} `json:"error"`
	}{
		Error: struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Fields  map[string]string `json:"fields,omitempty"`
		}{
			Code:    err.Code,
			Message: err.Message,
			Fields:  err.Fields,
		},
	}
	httpx.WriteJSON(w, http.StatusBadRequest, envelope)
}

func (h *Handler) listOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", httpx.RequestID(r))
		return
	}

	ctx := r.Context()

	status := r.URL.Query().Get("status")
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")

	limit := 20
	if limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be a valid integer", httpx.RequestID(r))
			return
		}
		limit = parsedLimit
	}

	if limit < 1 || limit > 100 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 100", httpx.RequestID(r))
		return
	}

	var from, to time.Time
	var err error
	if fromStr != "" {
		from, err = time.Parse(time.RFC3339, fromStr)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_from", "from must be a valid RFC3339 timestamp", httpx.RequestID(r))
			return
		}
	}
	if toStr != "" {
		to, err = time.Parse(time.RFC3339, toStr)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_to", "to must be a valid RFC3339 timestamp", httpx.RequestID(r))
			return
		}
	}

	orders, nextCursor, err := h.orders.List(ctx, status, from, to, cursor, limit)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error", httpx.RequestID(r))
		return
	}

	data := make([]OrderResponse, len(orders))
	for i, o := range orders {
		data[i] = toOrderResponse(o)
	}

	httpx.WriteJSON(w, http.StatusOK, OrderListResponse{
		Data:       data,
		NextCursor: nextCursor,
	})
}

func (h *Handler) handleOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", httpx.RequestID(r))
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

	order, err := h.orders.Get(ctx, id)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "order_not_found", "order not found", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toOrderResponse(*order))
}

type OrderListResponse struct {
	Data       []OrderResponse `json:"data"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type OrderResponse struct {
	ID                string                `json:"id"`
	Number            string                `json:"number"`
	Status            string                `json:"status"`
	PaymentStatus     string                `json:"payment_status"`
	FulfillmentStatus string                `json:"fulfillment_status"`
	Lines             []OrderLineResponse   `json:"lines"`
	Totals            OrderTotalsResponse   `json:"totals"`
	Refunds           []OrderRefundResponse `json:"refunds,omitempty"`
	CreatedAt         time.Time             `json:"created_at"`
	UpdatedAt         time.Time             `json:"updated_at"`
}

type OrderLineResponse struct {
	VariantID string `json:"variant_id"`
	SKU       string `json:"sku"`
	Quantity  int    `json:"quantity"`
	UnitPrice int64  `json:"unit_price"`
	Currency  string `json:"currency"`
	Discount  int64  `json:"discount"`
	Tax       int64  `json:"tax"`
}

type OrderTotalsResponse struct {
	Subtotal   int64  `json:"subtotal"`
	Discount   int64  `json:"discount"`
	Tax        int64  `json:"tax"`
	GrandTotal int64  `json:"grand_total"`
	Currency   string `json:"currency"`
}

type OrderRefundResponse struct {
	ID        string    `json:"id"`
	Amount    int64     `json:"amount"`
	Currency  string    `json:"currency"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

func toOrderResponse(o order.Order) OrderResponse {
	lines := make([]OrderLineResponse, len(o.Lines))
	for i, l := range o.Lines {
		lines[i] = OrderLineResponse{
			VariantID: l.VariantID,
			SKU:       l.SKU,
			Quantity:  l.Quantity,
			UnitPrice: l.UnitPrice.Value,
			Currency:  l.UnitPrice.Currency,
			Discount:  l.Discount.Value,
			Tax:       l.Tax.Value,
		}
	}

	refunds := make([]OrderRefundResponse, len(o.Refunds))
	for i, r := range o.Refunds {
		refunds[i] = OrderRefundResponse{
			ID:        r.ID,
			Amount:    r.Amount.Value,
			Currency:  r.Amount.Currency,
			Reason:    r.Reason,
			CreatedAt: r.CreatedAt,
		}
	}

	return OrderResponse{
		ID:                o.ID,
		Number:            o.Number,
		Status:            string(o.Status),
		PaymentStatus:     string(o.PaymentStatus),
		FulfillmentStatus: string(o.FulfillmentStatus),
		Lines:             lines,
		Totals: OrderTotalsResponse{
			Subtotal:   o.Totals.Subtotal.Value,
			Discount:   o.Totals.Discount.Value,
			Tax:        o.Totals.Tax.Value,
			GrandTotal: o.Totals.GrandTotal.Value,
			Currency:   o.Totals.GrandTotal.Currency,
		},
		Refunds:   refunds,
		CreatedAt: o.CreatedAt,
		UpdatedAt: o.UpdatedAt,
	}
}

func generateProductID() string {
	return "prod_" + time.Now().Format("20060102150405") + randomSuffix(4)
}
