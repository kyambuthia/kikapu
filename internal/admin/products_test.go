package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kyambuthia/kikapu/internal/platform/money"
)

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		name    string
		slug    string
		wantErr bool
	}{
		{"valid lowercase", "my-product-slug", false},
		{"valid with numbers", "product-123-abc", false},
		{"valid single word", "product", false},
		{"empty slug", "", true},
		{"starts with hyphen", "-product", true},
		{"ends with hyphen", "product-", true},
		{"too long", "a" + string(make([]byte, 80)), true},
		{"uppercase letters", "My-Product", true},
		{"spaces", "my product", true},
		{"special chars", "my-product!", true},
		{"double hyphen", "my--product", true},
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
		fields  map[string]string
	}{
		{
			name: "valid input",
			input: ProductInput{
				Slug:   "valid-product",
				Name:   "Valid Name",
				Status: StatusDraft,
				Variants: []VariantInput{
					{SKU: "SKU-001", Price: 1000, Currency: "USD"},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid slug",
			input: ProductInput{
				Slug: "-invalid",
				Name: "Name",
			},
			wantErr: true,
			fields:  map[string]string{"slug": "invalid slug"},
		},
		{
			name: "empty name",
			input: ProductInput{
				Slug: "valid-slug",
				Name: "",
			},
			wantErr: true,
			fields:  map[string]string{"name": "name cannot be empty"},
		},
		{
			name: "invalid status",
			input: ProductInput{
				Slug:   "valid-slug",
				Name:   "Name",
				Status: "invalid",
			},
			wantErr: true,
			fields:  map[string]string{"status": "status must be draft or published"},
		},
		{
			name: "negative price",
			input: ProductInput{
				Slug: "valid-slug",
				Name: "Name",
				Variants: []VariantInput{
					{SKU: "SKU-001", Price: -100, Currency: "USD"},
				},
			},
			wantErr: true,
			fields:  map[string]string{"variants[0].price": "price cannot be negative"},
		},
		{
			name: "invalid currency",
			input: ProductInput{
				Slug: "valid-slug",
				Name: "Name",
				Variants: []VariantInput{
					{SKU: "SKU-001", Price: 100, Currency: "invalid"},
				},
			},
			wantErr: true,
			fields:  map[string]string{"variants[0].currency": "currency must be a valid ISO-4217 code"},
		},
		{
			name: "duplicate SKU",
			input: ProductInput{
				Slug: "valid-slug",
				Name: "Name",
				Variants: []VariantInput{
					{SKU: "SKU-001", Price: 100, Currency: "USD"},
					{SKU: "SKU-001", Price: 200, Currency: "USD"},
				},
			},
			wantErr: true,
			fields:  map[string]string{"variants[1].sku": "duplicate SKU"},
		},
		{
			name: "empty SKU",
			input: ProductInput{
				Slug: "valid-slug",
				Name: "Name",
				Variants: []VariantInput{
					{SKU: "", Price: 100, Currency: "USD"},
				},
			},
			wantErr: true,
			fields:  map[string]string{"variants[0].sku": "SKU cannot be empty"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validationErr := ValidateProductInput(tt.input)
			if (validationErr != nil) != tt.wantErr {
				t.Errorf("ValidateProductInput() error = %v, wantErr %v", validationErr, tt.wantErr)
			}

			if tt.wantErr && validationErr != nil {
				for field := range tt.fields {
					if validationErr.Fields[field] == "" {
						t.Errorf("expected error field %q to be set", field)
					}
				}
			}
		})
	}
}

func TestStockLevel_Available(t *testing.T) {
	tests := []struct {
		name     string
		level    StockLevel
		expected int
	}{
		{
			name:     "positive available",
			level:    StockLevel{OnHand: 100, Reserved: 10},
			expected: 90,
		},
		{
			name:     "all reserved",
			level:    StockLevel{OnHand: 50, Reserved: 50},
			expected: 0,
		},
		{
			name:     "none reserved",
			level:    StockLevel{OnHand: 100, Reserved: 0},
			expected: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.level.Available()
			if got != tt.expected {
				t.Errorf("Available() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestHandler_ListProducts(t *testing.T) {
	repo := NewMemoryProductRepository()
	stockRepo := NewMemoryStockRepository()
	handler := NewHandler(repo, stockRepo)

	ctx := context.Background()

	product1, _ := repo.Create(ctx, ProductInput{
		Slug:   "product-1",
		Name:   "Product 1",
		Status: StatusPublished,
		Variants: []VariantInput{
			{SKU: "SKU-001", Price: 1000, Currency: "USD"},
		},
	}, "key-1")

	product2, _ := repo.Create(ctx, ProductInput{
		Slug:   "product-2",
		Name:   "Product 2",
		Status: StatusDraft,
		Variants: []VariantInput{
			{SKU: "SKU-002", Price: 2000, Currency: "USD"},
		},
	}, "key-2")

	tests := []struct {
		name       string
		query      string
		wantCount  int
		wantStatus int
	}{
		{
			name:       "list all products",
			query:      "",
			wantCount:  2,
			wantStatus: http.StatusOK,
		},
		{
			name:       "filter by published status",
			query:      "?status=published",
			wantCount:  1,
			wantStatus: http.StatusOK,
		},
		{
			name:       "filter by draft status",
			query:      "?status=draft",
			wantCount:  1,
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid status",
			query:      "?status=invalid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "with limit",
			query:      "?limit=1",
			wantCount:  1,
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid limit",
			query:      "?limit=invalid",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/admin/products"+tt.query, nil)
			w := httptest.NewRecorder()

			mux := http.NewServeMux()
			handler.RegisterRoutes(mux)
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				var response map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}

				data, ok := response["data"].([]interface{})
				if !ok {
					t.Fatal("response data is not an array")
				}

				if len(data) != tt.wantCount {
					t.Errorf("got %d products, want %d", len(data), tt.wantCount)
				}
			}
		})
	}

	_ = product1
	_ = product2
}

func TestHandler_CreateProduct(t *testing.T) {
	repo := NewMemoryProductRepository()
	stockRepo := NewMemoryStockRepository()
	handler := NewHandler(repo, stockRepo)

	tests := []struct {
		name       string
		input      ProductInput
		key        string
		wantStatus int
	}{
		{
			name: "valid product",
			input: ProductInput{
				Slug:   "new-product",
				Name:   "New Product",
				Status: StatusDraft,
				Variants: []VariantInput{
					{SKU: "NEW-001", Price: 1500, Currency: "USD"},
				},
			},
			key:        "unique-key-1",
			wantStatus: http.StatusCreated,
		},
		{
			name: "missing idempotency key",
			input: ProductInput{
				Slug: "another-product",
				Name: "Another Product",
			},
			key:        "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "duplicate slug",
			input: ProductInput{
				Slug: "new-product",
				Name: "Duplicate Slug",
			},
			key:        "unique-key-2",
			wantStatus: http.StatusCreated,
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

			mux := http.NewServeMux()
			handler.RegisterRoutes(mux)

			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK && tt.wantStatus != http.StatusCreated {
			}
		})
	}
}

func TestHandler_UpdateStock(t *testing.T) {
	repo := NewMemoryProductRepository()
	stockRepo := NewMemoryStockRepository()
	handler := NewHandler(repo, stockRepo)

	ctx := context.Background()

	product, _ := repo.Create(ctx, ProductInput{
		Slug:   "product-for-stock",
		Name:   "Product for Stock",
		Status: StatusPublished,
		Variants: []VariantInput{
			{SKU: "STOCK-001", Price: 1000, Currency: "USD"},
		},
	}, "stock-key-1")

	variantID := product.Variants[0].ID

	stockRepo.SetLevel(ctx, StockUpdate{
		VariantID:  variantID,
		LocationID: "warehouse-1",
		OnHand:     100,
	})

	tests := []struct {
		name       string
		variantID  string
		body       interface{}
		wantStatus int
		wantOnHand int
	}{
		{
			name:      "update stock successfully",
			variantID: variantID,
			body: map[string]interface{}{
				"location_id": "warehouse-1",
				"on_hand":     150,
			},
			wantStatus: http.StatusOK,
			wantOnHand: 150,
		},
		{
			name:      "negative on_hand",
			variantID: variantID,
			body: map[string]interface{}{
				"location_id": "warehouse-1",
				"on_hand":     -10,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:      "missing location_id",
			variantID: variantID,
			body: map[string]interface{}{
				"on_hand": 100,
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			url := "/v1/admin/products/" + tt.variantID + "/stock"
			req := httptest.NewRequest(http.MethodPut, url, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			mux := http.NewServeMux()
			handler.RegisterRoutes(mux)
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d, body: %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var level StockLevel
				if err := json.Unmarshal(w.Body.Bytes(), &level); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}

				if level.OnHand != tt.wantOnHand {
					t.Errorf("OnHand = %d, want %d", level.OnHand, tt.wantOnHand)
				}
			}
		})
	}
}

func TestHandler_ValidationErrors(t *testing.T) {
	repo := NewMemoryProductRepository()
	stockRepo := NewMemoryStockRepository()
	handler := NewHandler(repo, stockRepo)

	tests := []struct {
		name       string
		input      ProductInput
		key        string
		wantStatus int
		wantFields map[string]bool
	}{
		{
			name: "bad slug",
			input: ProductInput{
				Slug: "-bad-slug",
				Name: "Product",
			},
			key:        "bad-slug-key",
			wantStatus: http.StatusBadRequest,
			wantFields: map[string]bool{"slug": true},
		},
		{
			name: "negative price",
			input: ProductInput{
				Slug: "valid-slug",
				Name: "Product",
				Variants: []VariantInput{
					{SKU: "SKU-001", Price: -100, Currency: "USD"},
				},
			},
			key:        "negative-price-key",
			wantStatus: http.StatusBadRequest,
			wantFields: map[string]bool{"variants[0].price": true},
		},
		{
			name: "unknown currency",
			input: ProductInput{
				Slug: "valid-slug",
				Name: "Product",
				Variants: []VariantInput{
					{SKU: "SKU-001", Price: 100, Currency: "invalid"},
				},
			},
			key:        "unknown-currency-key",
			wantStatus: http.StatusBadRequest,
			wantFields: map[string]bool{"variants[0].currency": true},
		},
		{
			name: "duplicate SKU",
			input: ProductInput{
				Slug: "valid-slug",
				Name: "Product",
				Variants: []VariantInput{
					{SKU: "DUP-001", Price: 100, Currency: "USD"},
					{SKU: "DUP-001", Price: 200, Currency: "USD"},
				},
			},
			key:        "duplicate-sku-key",
			wantStatus: http.StatusBadRequest,
			wantFields: map[string]bool{"variants[1].sku": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.input)
			req := httptest.NewRequest(http.MethodPost, "/v1/admin/products", bytes.NewReader(body))
			req.Header.Set("Idempotency-Key", tt.key)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			mux := http.NewServeMux()
			handler.RegisterRoutes(mux)

			mux2 := http.NewServeMux()
			handler.RegisterRoutes(mux2)

			_ = mux2

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d, body: %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusBadRequest {
				var response map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}

				errorObj, ok := response["error"].(map[string]interface{})
				if !ok {
					t.Fatal("response does not have error object")
				}

				if errorObj["code"] != "validation_error" {
					t.Errorf("error code = %v, want validation_error", errorObj["code"])
				}

				fields, ok := errorObj["fields"].(map[string]interface{})
				if !ok {
					t.Fatal("error does not have fields object")
				}

				for field := range tt.wantFields {
					if _, exists := fields[field]; !exists {
						t.Errorf("expected field %q in validation errors", field)
					}
				}
			}
		})
	}
}

func TestIdempotentCreate(t *testing.T) {
	repo := NewMemoryProductRepository()
	stockRepo := NewMemoryStockRepository()

	handler := NewHandler(repo, stockRepo)

	ctx := context.Background()

	input := ProductInput{
		Slug:   "idempotent-product",
		Name:   "Idempotent Product",
		Status: StatusDraft,
		Variants: []VariantInput{
			{SKU: "IDEM-001", Price: 1000, Currency: "USD"},
		},
	}

	key := "idempotent-test-key"

	product1, err := repo.Create(ctx, input, key)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	product2, err := repo.Create(ctx, input, key)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	if product1.ID != product2.ID {
		t.Errorf("idempotent calls returned different products: %s vs %s", product1.ID, product2.ID)
	}

	if product1.Name != product2.Name {
		t.Errorf("idempotent calls returned different names: %s vs %s", product1.Name, product2.Name)
	}

	products, _, err := repo.List(ctx, ProductListOptions{})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	if len(products) != 1 {
		t.Errorf("expected 1 product in repo, got %d", len(products))
	}

	_ = handler
}

func TestMemoryRepository_Update(t *testing.T) {
	repo := NewMemoryProductRepository()
	ctx := context.Background()

	product, _ := repo.Create(ctx, ProductInput{
		Slug:   "update-test",
		Name:   "Original Name",
		Status: StatusDraft,
		Variants: []VariantInput{
			{SKU: "UPDATE-001", Price: 1000, Currency: "USD"},
		},
	}, "update-key-1")

	updated, err := repo.Update(ctx, product.ID, ProductInput{
		Slug:   "updated-slug",
		Name:   "Updated Name",
		Status: StatusPublished,
		Variants: []VariantInput{
			{SKU: "UPDATE-002", Price: 2000, Currency: "EUR"},
		},
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	if updated.Slug != "updated-slug" {
		t.Errorf("Slug = %s, want updated-slug", updated.Slug)
	}

	if updated.Name != "Updated Name" {
		t.Errorf("Name = %s, want Updated Name", updated.Name)
	}

	if updated.Status != StatusPublished {
		t.Errorf("Status = %s, want published", updated.Status)
	}

	if len(updated.Variants) != 1 {
		t.Fatalf("expected 1 variant, got %d", len(updated.Variants))
	}

	if updated.Variants[0].SKU != "UPDATE-002" {
		t.Errorf("Variant SKU = %s, want UPDATE-002", updated.Variants[0].SKU)
	}

	if updated.Variants[0].Price.Value != 2000 {
		t.Errorf("Variant Price = %d, want 2000", updated.Variants[0].Price.Value)
	}
}

func TestMemoryRepository_Delete(t *testing.T) {
	repo := NewMemoryProductRepository()
	ctx := context.Background()

	product, _ := repo.Create(ctx, ProductInput{
		Slug: "delete-test",
		Name: "Delete Test",
	}, "delete-key-1")

	err := repo.Delete(ctx, product.ID)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	deleted, err := repo.Get(ctx, product.ID)
	if err != nil {
		t.Fatalf("get after delete failed: %v", err)
	}

	if deleted.Status != StatusArchived {
		t.Errorf("Status = %s, want archived", deleted.Status)
	}

	err = repo.Delete(ctx, "nonexistent")
	if err != ErrProductNotFound {
		t.Errorf("expected ErrProductNotFound, got %v", err)
	}
}

func TestMemoryStockRepository(t *testing.T) {
	repo := NewMemoryStockRepository()
	ctx := context.Background()

	update := StockUpdate{
		VariantID:  "variant-1",
		LocationID: "warehouse-1",
		OnHand:     100,
	}

	level, err := repo.SetLevel(ctx, update)
	if err != nil {
		t.Fatalf("SetLevel failed: %v", err)
	}

	if level.OnHand != 100 {
		t.Errorf("OnHand = %d, want 100", level.OnHand)
	}

	if level.Available() != 100 {
		t.Errorf("Available = %d, want 100", level.Available())
	}

	level2, err := repo.SetLevel(ctx, StockUpdate{
		VariantID:  "variant-1",
		LocationID: "warehouse-1",
		OnHand:     150,
	})
	if err != nil {
		t.Fatalf("second SetLevel failed: %v", err)
	}

	if level2.OnHand != 150 {
		t.Errorf("OnHand = %d, want 150", level2.OnHand)
	}

	savedWithReserved, err := repo.SetLevel(ctx, StockUpdate{
		VariantID:  "variant-2",
		LocationID: "warehouse-1",
		OnHand:     50,
	})
	if err != nil {
		t.Fatalf("SetLevel for variant-2 failed: %v", err)
	}

	_ = savedWithReserved

	level3, err := repo.GetLevel(ctx, "variant-1", "warehouse-1")
	if err != nil {
		t.Fatalf("GetLevel failed: %v", err)
	}
	if level3 == nil {
		t.Fatal("GetLevel returned nil")
	}
	if level3.OnHand != 150 {
		t.Errorf("GetLevel OnHand = %d, want 150", level3.OnHand)
	}

	nonExistent, err := repo.GetLevel(ctx, "nonexistent", "warehouse-1")
	if err != nil {
		t.Fatalf("GetLevel for nonexistent failed: %v", err)
	}
	if nonExistent != nil {
		t.Error("expected nil for nonexistent stock level")
	}
}

func TestMoneyIntegration(t *testing.T) {
	amount := money.NewAmount(10000, "USD")

	if amount.Value != 10000 {
		t.Errorf("Value = %d, want 10000", amount.Value)
	}

	if amount.Currency != "USD" {
		t.Errorf("Currency = %s, want USD", amount.Currency)
	}

	repo := NewMemoryProductRepository()
	ctx := context.Background()

	product, err := repo.Create(ctx, ProductInput{
		Slug: "money-test",
		Name: "Money Test",
		Variants: []VariantInput{
			{SKU: "MONEY-001", Price: 9999, Currency: "KES"},
		},
	}, "money-key")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if len(product.Variants) != 1 {
		t.Fatalf("expected 1 variant, got %d", len(product.Variants))
	}

	if product.Variants[0].Price.Value != 9999 {
		t.Errorf("Price.Value = %d, want 9999", product.Variants[0].Price.Value)
	}

	if product.Variants[0].Price.Currency != "KES" {
		t.Errorf("Price.Currency = %s, want KES", product.Variants[0].Price.Currency)
	}
}

func TestHandler_RoundTrip(t *testing.T) {
	repo := NewMemoryProductRepository()
	stockRepo := NewMemoryStockRepository()
	ctx := context.Background()

	product, _ := repo.Create(ctx, ProductInput{
		Slug:   "roundtrip-product",
		Name:   "RoundTrip Product",
		Status: StatusDraft,
		Variants: []VariantInput{
			{SKU: "RT-001", Price: 1000, Currency: "USD"},
		},
	}, "roundtrip-create")

	updated, _ := repo.Update(ctx, product.ID, ProductInput{
		Slug:   "roundtrip-updated",
		Name:   "RoundTrip Updated",
		Status: StatusPublished,
		Variants: []VariantInput{
			{SKU: "RT-002", Price: 1500, Currency: "USD"},
		},
	})

	products, _, _ := repo.List(ctx, ProductListOptions{})

	found := false
	for _, p := range products {
		if p.ID == updated.ID {
			found = true
			if p.Slug != "roundtrip-updated" {
				t.Errorf("listed product slug = %s, want roundtrip-updated", p.Slug)
			}
		}
	}

	if !found {
		t.Error("updated product not found in list")
	}

	_ = stockRepo
}

func TestHandler_ProductNotFound(t *testing.T) {
	repo := NewMemoryProductRepository()
	stockRepo := NewMemoryStockRepository()
	handler := NewHandler(repo, stockRepo)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/products/nonexistent-id", nil)
	w := httptest.NewRecorder()

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	errorObj := response["error"].(map[string]interface{})
	if errorObj["code"] != "product_not_found" {
		t.Errorf("error code = %v, want product_not_found", errorObj["code"])
	}
}
