package catalog

import (
	"encoding/base64"
	"testing"
)

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		name    string
		slug    string
		wantErr error
	}{
		{"valid simple", "product-123", nil},
		{"valid complex", "wireless-mouse-27-inch", nil},
		{"valid numbers only", "12345", nil},
		{"valid single char", "a", nil},
		{"valid max length", "abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuv", nil},
		{"empty slug", "", ErrInvalidSlug},
		{"leading hyphen", "-invalid", ErrInvalidSlug},
		{"trailing hyphen", "invalid-", ErrInvalidSlug},
		{"double hyphen", "invalid--slug", ErrInvalidSlug},
		{"uppercase letter", "Invalid", ErrInvalidSlug},
		{"special chars", "invalid@slug", ErrInvalidSlug},
		{"spaces", "invalid slug", ErrInvalidSlug},
		{"underscores", "invalid_slug", ErrInvalidSlug},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSlug(tt.slug)
			if err != tt.wantErr {
				t.Errorf("ValidateSlug(%q) = %v, want %v", tt.slug, err, tt.wantErr)
			}
		})
	}
}

func TestInMemoryRepository_Get(t *testing.T) {
	products := []Product{
		{ID: "1", Slug: "test-product", Name: "Test Product", Status: "published"},
		{ID: "2", Slug: "another-product", Name: "Another Product", Status: "published"},
	}
	repo := NewInMemoryRepository(products)

	t.Run("found", func(t *testing.T) {
		product, err := repo.Get(nil, "test-product")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if product.ID != "1" {
			t.Errorf("expected ID 1, got %s", product.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := repo.Get(nil, "nonexistent")
		if err != ErrProductNotFound {
			t.Errorf("expected ErrProductNotFound, got %v", err)
		}
	})
}

func TestInMemoryRepository_List_Pagination(t *testing.T) {
	var products []Product
	for i := 1; i <= 25; i++ {
		products = append(products, Product{
			ID:     string(rune('a' + i - 1)),
			Slug:   "product-" + string(rune('a'+i-1)),
			Name:   "Product " + string(rune('a'+i-1)),
			Status: "published",
		})
	}
	repo := NewInMemoryRepository(products)

	var allProducts []Product
	var nextCursor string
	iterations := 0

	for {
		opts := ListOptions{Limit: 5, Cursor: nextCursor}
		batch, cursor, err := repo.List(nil, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		allProducts = append(allProducts, batch...)
		nextCursor = cursor

		iterations++
		if iterations > 10 {
			t.Fatal("too many iterations, possible infinite loop")
		}

		if nextCursor == "" {
			break
		}
	}

	if len(allProducts) != 25 {
		t.Errorf("expected 25 products, got %d", len(allProducts))
	}

	seenIDs := make(map[string]bool)
	for _, p := range allProducts {
		if seenIDs[p.ID] {
			t.Errorf("duplicate product ID: %s", p.ID)
		}
		seenIDs[p.ID] = true
	}

	if len(seenIDs) != 25 {
		t.Errorf("expected 25 unique products, got %d", len(seenIDs))
	}
}

func TestInMemoryRepository_List_InvalidLimit(t *testing.T) {
	products := []Product{
		{ID: "1", Slug: "test", Name: "Test", Status: "published"},
	}
	repo := NewInMemoryRepository(products)

	_, _, err := repo.List(nil, ListOptions{Limit: 150})
	if err != ErrInvalidLimit {
		t.Errorf("expected ErrInvalidLimit, got %v", err)
	}
}

func TestInMemoryRepository_List_DefaultLimit(t *testing.T) {
	var products []Product
	for i := 1; i <= 30; i++ {
		products = append(products, Product{
			ID:     string(rune('a' + i - 1)),
			Slug:   "product-" + string(rune('a'+i-1)),
			Name:   "Product",
			Status: "published",
		})
	}
	repo := NewInMemoryRepository(products)

	batch, _, err := repo.List(nil, ListOptions{Limit: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(batch) != 20 {
		t.Errorf("expected 20 products (default limit), got %d", len(batch))
	}
}

func TestInMemoryRepository_List_InvalidCursor(t *testing.T) {
	products := []Product{
		{ID: "1", Slug: "test", Name: "Test", Status: "published"},
	}
	repo := NewInMemoryRepository(products)

	_, _, err := repo.List(nil, ListOptions{Cursor: "invalid-base64"})
	if err != ErrInvalidCursor {
		t.Errorf("expected ErrInvalidCursor, got %v", err)
	}

	invalidJSON := base64.StdEncoding.EncodeToString([]byte("not json"))
	_, _, err = repo.List(nil, ListOptions{Cursor: invalidJSON})
	if err != ErrInvalidCursor {
		t.Errorf("expected ErrInvalidCursor for invalid JSON, got %v", err)
	}
}

func TestInMemoryRepository_List_StableOrder(t *testing.T) {
	products := []Product{
		{ID: "c", Slug: "c", Name: "C", Status: "published"},
		{ID: "a", Slug: "a", Name: "A", Status: "published"},
		{ID: "b", Slug: "b", Name: "B", Status: "published"},
	}
	repo := NewInMemoryRepository(products)

	batch, _, err := repo.List(nil, ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if batch[0].ID != "a" || batch[1].ID != "b" || batch[2].ID != "c" {
		t.Errorf("products not in stable order by ID: %v", batch)
	}
}

func TestLoadProductsFromJSON(t *testing.T) {
	jsonData := `[
		{
			"id": "prod1",
			"slug": "test-product",
			"name": "Test Product",
			"status": "published",
			"variants": [
				{"id": "var1", "sku": "SKU1", "price": {"value": 1000, "currency": "USD"}, "currency": "USD"}
			]
		}
	]`

	products, err := LoadProductsFromJSON([]byte(jsonData))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}

	if products[0].ID != "prod1" {
		t.Errorf("expected ID prod1, got %s", products[0].ID)
	}

	if len(products[0].Variants) != 1 {
		t.Errorf("expected 1 variant, got %d", len(products[0].Variants))
	}

	if products[0].Variants[0].Price.Value != 1000 {
		t.Errorf("expected price 1000, got %d", products[0].Variants[0].Price.Value)
	}
}

func TestValidateProducts(t *testing.T) {
	tests := []struct {
		name     string
		products []Product
		wantErr  bool
	}{
		{
			name: "valid products",
			products: []Product{
				{ID: "1", Slug: "valid-slug", Name: "Product"},
			},
			wantErr: false,
		},
		{
			name: "invalid slug",
			products: []Product{
				{ID: "1", Slug: "Invalid-Slug", Name: "Product"},
			},
			wantErr: true,
		},
		{
			name: "empty ID",
			products: []Product{
				{ID: "", Slug: "valid-slug", Name: "Product"},
			},
			wantErr: true,
		},
		{
			name: "empty name",
			products: []Product{
				{ID: "1", Slug: "valid-slug", Name: ""},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProducts(tt.products)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProducts() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIntegration_FullPaginationWalk(t *testing.T) {
	productData := `[
		{"id": "prod01", "slug": "p01", "name": "P01", "status": "published"},
		{"id": "prod02", "slug": "p02", "name": "P02", "status": "published"},
		{"id": "prod03", "slug": "p03", "name": "P03", "status": "published"},
		{"id": "prod04", "slug": "p04", "name": "P04", "status": "published"},
		{"id": "prod05", "slug": "p05", "name": "P05", "status": "published"},
		{"id": "prod06", "slug": "p06", "name": "P06", "status": "published"},
		{"id": "prod07", "slug": "p07", "name": "P07", "status": "published"},
		{"id": "prod08", "slug": "p08", "name": "P08", "status": "published"},
		{"id": "prod09", "slug": "p09", "name": "P09", "status": "published"},
		{"id": "prod10", "slug": "p10", "name": "P10", "status": "published"}
	]`

	products, err := LoadProductsFromJSON([]byte(productData))
	if err != nil {
		t.Fatalf("LoadProductsFromJSON error: %v", err)
	}

	repo := NewInMemoryRepository(products)

	var allIDs []string
	var nextCursor string

	for {
		batch, cursor, err := repo.List(nil, ListOptions{Limit: 3, Cursor: nextCursor})
		if err != nil {
			t.Fatalf("List error: %v", err)
		}

		for _, p := range batch {
			allIDs = append(allIDs, p.ID)
		}

		nextCursor = cursor
		if nextCursor == "" {
			break
		}
	}

	if len(allIDs) != 10 {
		t.Errorf("expected 10 products, got %d", len(allIDs))
	}
}
