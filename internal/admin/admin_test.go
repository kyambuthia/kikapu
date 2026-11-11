package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/inventory"
	"github.com/kyambuthia/kikapu/internal/order"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		name    string
		slug    string
		wantErr bool
	}{
		{"valid simple", "product", false},
		{"valid with hyphen", "wireless-mouse", false},
		{"valid multiple hyphens", "wireless-gaming-mouse", false},
		{"empty", "", true},
		{"too long", "this-is-a-very-long-slug-that-exceeds-eighty-characters-limit-and-should-fail-validation-test", true},
		{"starts with hyphen", "-product", true},
		{"ends with hyphen", "product-", true},
		{"uppercase", "Product", true},
		{"spaces", "wireless mouse", true},
		{"special chars", "product!", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSlug(tt.slug)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSlug(%q) error = %v, wantErr %v", tt.slug, err, tt.wantErr)
			}
		})
	}
}

func TestValidateProductInput(t *testing.T) {
	tests := []struct {
		name    string
		input   ProductInput
		wantErr bool
		errCode string
	}{
		{
			name: "valid product",
			input: ProductInput{
				Name:   "Test Product",
				Slug:   "test-product",
				Status: "published",
				Variants: []VariantInput{
					{SKU: "SKU-001", Price: 1000, Currency: "USD"},
				},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			input: ProductInput{
				Slug: "test-product",
			},
			wantErr: true,
			errCode: "invalid_name",
		},
		{
			name: "invalid slug",
			input: ProductInput{
				Name: "Test",
				Slug: "-invalid",
			},
			wantErr: true,
			errCode: "invalid_slug",
		},
		{
			name: "invalid status",
			input: ProductInput{
				Name:   "Test",
				Slug:   "test",
				Status: "invalid",
			},
			wantErr: true,
			errCode: "invalid_status",
		},
		{
			name: "duplicate SKU",
			input: ProductInput{
				Name: "Test",
				Slug: "test",
				Variants: []VariantInput{
					{SKU: "SKU-001", Price: 1000, Currency: "USD"},
					{SKU: "SKU-001", Price: 2000, Currency: "USD"},
				},
			},
			wantErr: true,
			errCode: "duplicate_sku",
		},
		{
			name: "negative price",
			input: ProductInput{
				Name: "Test",
				Slug: "test",
				Variants: []VariantInput{
					{SKU: "SKU-001", Price: -100, Currency: "USD"},
				},
			},
			wantErr: true,
			errCode: "invalid_price",
		},
		{
			name: "unknown currency",
			input: ProductInput{
				Name: "Test",
				Slug: "test",
				Variants: []VariantInput{
					{SKU: "SKU-001", Price: 1000, Currency: "XXX"},
				},
			},
			wantErr: true,
			errCode: "invalid_currency",
		},
		{
			name: "empty currency",
			input: ProductInput{
				Name: "Test",
				Slug: "test",
				Variants: []VariantInput{
					{SKU: "SKU-001", Price: 1000, Currency: ""},
				},
			},
			wantErr: true,
			errCode: "invalid_currency",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProductInput(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateProductInput() expected error, got nil")
					return
				}
				if err.Code != tt.errCode {
					t.Errorf("ValidateProductInput() error code = %s, want %s", err.Code, tt.errCode)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateProductInput() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestCreateProduct(t *testing.T) {
	productRepo := NewInMemoryProductRepository()
	inventoryRepo := NewInMemoryInventoryRepository()
	orderRepo := NewInMemoryOrderRepository()
	h := NewHandler(productRepo, inventoryRepo, orderRepo)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	tests := []struct {
		name       string
		input      ProductInput
		key        string
		wantStatus int
	}{
		{
			name: "create product successfully",
			input: ProductInput{
				Name:   "Test Product",
				Slug:   "test-product",
				Status: "published",
				Variants: []VariantInput{
					{SKU: "SKU-001", Price: 1000, Currency: "USD"},
				},
			},
			key:        "test-key-1",
			wantStatus: http.StatusCreated,
		},
		{
			name: "missing idempotency key",
			input: ProductInput{
				Name: "Test",
				Slug: "test-2",
			},
			key:        "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "duplicate slug",
			input: ProductInput{
				Name: "Test Product 2",
				Slug: "test-product",
			},
			key:        "test-key-2",
			wantStatus: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.input)
			req := httptest.NewRequest(http.MethodPost, "/v1/admin/products", bytes.NewReader(body))
			if tt.key != "" {
				req.Header.Set("Idempotency-Key", tt.key)
			}
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusCreated {
				var resp ProductResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Errorf("failed to unmarshal response: %v", err)
				}
				if resp.Slug != tt.input.Slug {
					t.Errorf("slug = %s, want %s", resp.Slug, tt.input.Slug)
				}
				if resp.Name != tt.input.Name {
					t.Errorf("name = %s, want %s", resp.Name, tt.input.Name)
				}
			}
		})
	}
}

func TestProductRoundTrip(t *testing.T) {
	productRepo := NewInMemoryProductRepository()
	inventoryRepo := NewInMemoryInventoryRepository()
	orderRepo := NewInMemoryOrderRepository()
	h := NewHandler(productRepo, inventoryRepo, orderRepo)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	createInput := ProductInput{
		Name:   "Wireless Mouse",
		Slug:   "wireless-mouse",
		Status: "draft",
		Variants: []VariantInput{
			{SKU: "WM-001", Price: 2999, Currency: "USD"},
			{SKU: "WM-002", Price: 3499, Currency: "USD"},
		},
	}

	body, _ := json.Marshal(createInput)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/products", bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", "create-1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create failed with status %d: %s", w.Code, w.Body.String())
	}

	var created ProductResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created product: %v", err)
	}

	patchInput := map[string]interface{}{
		"name":   "Wireless Gaming Mouse",
		"status": "published",
	}
	body, _ = json.Marshal(patchInput)
	req = httptest.NewRequest(http.MethodPatch, "/v1/admin/products/"+created.ID, bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", "patch-1")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("patch failed with status %d: %s", w.Code, w.Body.String())
	}

	var patched ProductResponse
	if err := json.Unmarshal(w.Body.Bytes(), &patched); err != nil {
		t.Fatalf("unmarshal patched product: %v", err)
	}

	if patched.Name != "Wireless Gaming Mouse" {
		t.Errorf("name = %s, want Wireless Gaming Mouse", patched.Name)
	}
	if patched.Status != "published" {
		t.Errorf("status = %s, want published", patched.Status)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/admin/products", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list failed with status %d: %s", w.Code, w.Body.String())
	}

	var list ProductListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}

	if len(list.Data) != 1 {
		t.Errorf("len(list.Data) = %d, want 1", len(list.Data))
	}
}

func TestIdempotentReplay(t *testing.T) {
	productRepo := NewInMemoryProductRepository()
	inventoryRepo := NewInMemoryInventoryRepository()
	orderRepo := NewInMemoryOrderRepository()
	h := NewHandler(productRepo, inventoryRepo, orderRepo)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	input := ProductInput{
		Name:   "Test Product",
		Slug:   "test-product",
		Status: "published",
		Variants: []VariantInput{
			{SKU: "SKU-001", Price: 1000, Currency: "USD"},
		},
	}

	body, _ := json.Marshal(input)
	req1 := httptest.NewRequest(http.MethodPost, "/v1/admin/products", bytes.NewReader(body))
	req1.Header.Set("Idempotency-Key", "replay-test")
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	mux.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("first request failed with status %d", w1.Code)
	}

	var resp1 ProductResponse
	json.Unmarshal(w1.Body.Bytes(), &resp1)

	req2 := httptest.NewRequest(http.MethodPost, "/v1/admin/products", bytes.NewReader(body))
	req2.Header.Set("Idempotency-Key", "replay-test")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	if w2.Code != w1.Code {
		t.Errorf("status mismatch: first=%d, second=%d", w1.Code, w2.Code)
	}

	var resp2 ProductResponse
	json.Unmarshal(w2.Body.Bytes(), &resp2)

	if resp1.ID != resp2.ID {
		t.Errorf("id mismatch on replay: first=%s, second=%s", resp1.ID, resp2.ID)
	}

	ctx := context.Background()
	products, _, _ := productRepo.List(ctx, "", "", 10)
	if len(products) != 1 {
		t.Errorf("expected 1 product, got %d", len(products))
	}
}

func TestDeleteProduct(t *testing.T) {
	productRepo := NewInMemoryProductRepository()
	inventoryRepo := NewInMemoryInventoryRepository()
	orderRepo := NewInMemoryOrderRepository()
	h := NewHandler(productRepo, inventoryRepo, orderRepo)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	input := ProductInput{
		Name:   "Test Product",
		Slug:   "test-product",
		Status: "published",
	}
	body, _ := json.Marshal(input)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/products", bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", "create-1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var created ProductResponse
	json.Unmarshal(w.Body.Bytes(), &created)

	req = httptest.NewRequest(http.MethodDelete, "/v1/admin/products/"+created.ID, nil)
	req.Header.Set("Idempotency-Key", "delete-1")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("delete status = %d, want %d", w.Code, http.StatusNoContent)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/admin/products/"+created.ID, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var fetched ProductResponse
	json.Unmarshal(w.Body.Bytes(), &fetched)

	if fetched.Status != "archived" {
		t.Errorf("status = %s, want archived", fetched.Status)
	}
}

func TestStockUpdate(t *testing.T) {
	productRepo := NewInMemoryProductRepository()
	inventoryRepo := NewInMemoryInventoryRepository()
	orderRepo := NewInMemoryOrderRepository()
	h := NewHandler(productRepo, inventoryRepo, orderRepo)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	ctx := context.Background()
	product := &catalog.Product{
		ID:     "prod-001",
		Slug:   "test-product",
		Name:   "Test",
		Status: "published",
		Variants: []catalog.Variant{
			{ID: "var-001", SKU: "SKU-001", Price: money.NewAmount(1000, "USD"), Currency: "USD"},
		},
	}
	productRepo.Create(ctx, product)

	stockInput := map[string]interface{}{
		"variant_id":  "var-001",
		"location_id": "warehouse-ke",
		"on_hand":     100,
	}
	body, _ := json.Marshal(stockInput)
	req := httptest.NewRequest(http.MethodPut, "/v1/admin/products/prod-001/stock", bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", "stock-1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("stock update status = %d: %s", w.Code, w.Body.String())
	}

	levels, _ := inventoryRepo.GetLevels(ctx, "var-001")
	if len(levels) == 0 {
		t.Fatal("no inventory levels found")
	}
	if levels[0].OnHand != 100 {
		t.Errorf("on_hand = %d, want 100", levels[0].OnHand)
	}
	if levels[0].Available() != 100 {
		t.Errorf("available = %d, want 100", levels[0].Available())
	}

	stockInput["on_hand"] = -10
	body, _ = json.Marshal(stockInput)
	req = httptest.NewRequest(http.MethodPut, "/v1/admin/products/prod-001/stock", bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", "stock-invalid")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid stock status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestOrderList(t *testing.T) {
	productRepo := NewInMemoryProductRepository()
	inventoryRepo := NewInMemoryInventoryRepository()
	orderRepo := NewInMemoryOrderRepository()

	now := time.Now()
	o := &order.Order{
		ID:                "ord-001",
		Number:            "KIK-001",
		Status:            order.StatusConfirmed,
		PaymentStatus:     order.PaymentStatusCaptured,
		FulfillmentStatus: order.FulfillmentStatusUnfulfilled,
		Lines:             []order.Line{},
		Totals:            order.Totals{},
		RegionID:          "reg-001",
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	orderRepo.AddOrder(o)

	h := NewHandler(productRepo, inventoryRepo, orderRepo)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/orders", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("list orders status = %d: %s", w.Code, w.Body.String())
	}

	var list OrderListResponse
	json.Unmarshal(w.Body.Bytes(), &list)

	if len(list.Data) != 1 {
		t.Errorf("len(list.Data) = %d, want 1", len(list.Data))
	}
	if list.Data[0].ID != "ord-001" {
		t.Errorf("order ID = %s, want ord-001", list.Data[0].ID)
	}
}

func TestValidateStockUpdate(t *testing.T) {
	tests := []struct {
		name    string
		input   StockUpdate
		wantErr bool
		errCode string
	}{
		{
			name:    "valid",
			input:   StockUpdate{LocationID: "loc-001", OnHand: 100},
			wantErr: false,
		},
		{
			name:    "missing location",
			input:   StockUpdate{LocationID: "", OnHand: 100},
			wantErr: true,
			errCode: "invalid_location_id",
		},
		{
			name:    "negative on_hand",
			input:   StockUpdate{LocationID: "loc-001", OnHand: -10},
			wantErr: true,
			errCode: "invalid_on_hand",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStockUpdate(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
					return
				}
				if err.Code != tt.errCode {
					t.Errorf("error code = %s, want %s", err.Code, tt.errCode)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestInventoryAvailable(t *testing.T) {
	store := inventory.NewMemoryStore(inventory.RealClock{})
	store.SetLevel("var-001", "loc-001", 100)

	ctx := context.Background()
	levels, err := store.Levels(ctx, "var-001")
	if err != nil {
		t.Fatalf("Levels() error: %v", err)
	}

	if len(levels) != 1 {
		t.Fatalf("len(levels) = %d, want 1", len(levels))
	}

	if levels[0].Available() != 100 {
		t.Errorf("Available() = %d, want 100", levels[0].Available())
	}

	res, err := store.Reserve(ctx, "var-001", 10, 5*time.Minute)
	if err != nil {
		t.Fatalf("Reserve() error: %v", err)
	}

	levels, _ = store.Levels(ctx, "var-001")
	if levels[0].Available() != 90 {
		t.Errorf("Available() after reserve = %d, want 90", levels[0].Available())
	}

	if levels[0].Reserved != 10 {
		t.Errorf("Reserved = %d, want 10", levels[0].Reserved)
	}

	store.Commit(ctx, res.ID)

	levels, _ = store.Levels(ctx, "var-001")
	if levels[0].OnHand != 90 {
		t.Errorf("OnHand after commit = %d, want 90", levels[0].OnHand)
	}
}
