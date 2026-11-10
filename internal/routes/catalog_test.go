package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

func createTestRepo() catalog.Repository {
	products := []catalog.Product{}
	for i := 1; i <= 30; i++ {
		slug := "product-" + string(rune('a'+i-1))
		products = append(products, catalog.Product{
			ID:     string(rune('a' + i - 1)),
			Slug:   slug,
			Name:   "Product " + string(rune('a'+i-1)),
			Status: "published",
			Variants: []catalog.Variant{
				{
					ID:       "var" + string(rune('a'+i-1)),
					SKU:      "SKU" + string(rune('a'+i-1)),
					Price:    money.Amount{Value: int64(1000 * i), Currency: "USD"},
					Currency: "USD",
				},
			},
		})
	}
	return catalog.NewInMemoryRepository(products)
}

func TestCatalogHandler_ListProducts(t *testing.T) {
	repo := createTestRepo()
	handler := NewCatalogHandler(repo)

	t.Run("default pagination", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/products", nil)
		w := httptest.NewRecorder()

		handler.listProducts(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var resp ProductListResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if len(resp.Data) != 20 {
			t.Errorf("expected 20 products (default limit), got %d", len(resp.Data))
		}

		if resp.NextCursor == "" {
			t.Error("expected next cursor")
		}
	})

	t.Run("custom limit", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/products?limit=10", nil)
		w := httptest.NewRecorder()

		handler.listProducts(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var resp ProductListResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if len(resp.Data) != 10 {
			t.Errorf("expected 10 products, got %d", len(resp.Data))
		}
	})

	t.Run("invalid limit", func(t *testing.T) {
		tests := []struct {
			name  string
			limit string
		}{
			{"not a number", "abc"},
			{"negative", "-1"},
			{"too large", "150"},
			{"zero", "0"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest("GET", "/v1/products?limit="+tt.limit, nil)
				w := httptest.NewRecorder()

				handler.listProducts(w, req)

				if w.Code != http.StatusBadRequest {
					t.Errorf("expected status 400, got %d", w.Code)
				}
			})
		}
	})

	t.Run("pagination with cursor", func(t *testing.T) {
		req1 := httptest.NewRequest("GET", "/v1/products?limit=10", nil)
		w1 := httptest.NewRecorder()
		handler.listProducts(w1, req1)

		if w1.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w1.Code)
		}

		var resp1 ProductListResponse
		if err := json.Unmarshal(w1.Body.Bytes(), &resp1); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if resp1.NextCursor == "" {
			t.Fatal("expected next cursor")
		}

		req2 := httptest.NewRequest("GET", "/v1/products?limit=10&cursor="+resp1.NextCursor, nil)
		w2 := httptest.NewRecorder()
		handler.listProducts(w2, req2)

		if w2.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w2.Code)
		}

		var resp2 ProductListResponse
		if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if len(resp2.Data) != 10 {
			t.Errorf("expected 10 products, got %d", len(resp2.Data))
		}

		ids1 := make(map[string]bool)
		for _, p := range resp1.Data {
			ids1[p.ID] = true
		}

		for _, p := range resp2.Data {
			if ids1[p.ID] {
				t.Errorf("duplicate product ID between pages: %s", p.ID)
			}
		}
	})

	t.Run("invalid cursor", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/products?cursor=invalid", nil)
		w := httptest.NewRecorder()

		handler.listProducts(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestCatalogHandler_SparseFields(t *testing.T) {
	repo := createTestRepo()
	handler := NewCatalogHandler(repo)

	t.Run("sparse fields id and name only", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/products?limit=1&fields=id,name", nil)
		w := httptest.NewRecorder()

		handler.listProducts(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var resp ProductListResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if len(resp.Data) < 1 {
			t.Fatal("expected at least 1 product")
		}

		p := resp.Data[0]
		if p.ID == "" {
			t.Error("expected ID to be set")
		}
		if p.Name == "" {
			t.Error("expected name to be set")
		}
		if p.Slug != "" {
			t.Error("expected slug to be empty")
		}
		if p.Status != "" {
			t.Error("expected status to be empty")
		}
		if len(p.Variants) > 0 {
			t.Error("expected variants to be empty array or nil")
		}
	})

	t.Run("sparse fields variants.sku", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/products?limit=1&fields=id,variants.sku", nil)
		w := httptest.NewRecorder()

		handler.listProducts(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var resp ProductListResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if len(resp.Data) < 1 || len(resp.Data[0].Variants) < 1 {
			t.Fatal("expected at least 1 product with 1 variant")
		}

		variant := resp.Data[0].Variants[0]
		if variant.SKU == "" {
			t.Error("expected SKU to be set")
		}
		if variant.ID != "" {
			t.Error("expected variant ID to be empty")
		}
	})

	t.Run("unknown field rejected", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/products?fields=invalid_field", nil)
		w := httptest.NewRecorder()

		handler.listProducts(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestCatalogHandler_GetProduct(t *testing.T) {
	repo := createTestRepo()
	handler := NewCatalogHandler(repo)

	t.Run("found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/products/product-a", nil)
		w := httptest.NewRecorder()

		handler.getProduct(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var resp ProductResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if resp.Slug != "product-a" {
			t.Errorf("expected slug product-a, got %s", resp.Slug)
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/products/nonexistent", nil)
		w := httptest.NewRecorder()

		handler.getProduct(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", w.Code)
		}
	})

	t.Run("sparse fields", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/products/product-a?fields=id,name", nil)
		w := httptest.NewRecorder()

		handler.getProduct(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var resp ProductResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if resp.ID == "" {
			t.Error("expected ID to be set")
		}
		if resp.Name == "" {
			t.Error("expected name to be set")
		}
		if resp.Slug != "" {
			t.Error("expected slug to be empty")
		}
	})
}

func TestCatalogHandler_RegisterRoutes(t *testing.T) {
	repo := createTestRepo()
	handler := NewCatalogHandler(repo)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/products?limit=5")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}
