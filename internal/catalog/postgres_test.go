package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresStore_Get(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	store := NewPostgresStore(db)

	cleanup(t, db)
	productID := insertTestProduct(t, ctx, db, "test-product", "Test Product", "published")
	insertTestVariant(t, ctx, db, productID, "var-001", "SKU-001", 2999, "USD")

	tests := []struct {
		name    string
		slug    string
		wantErr error
	}{
		{"found", "test-product", nil},
		{"not found", "nonexistent", ErrProductNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			product, err := store.Get(ctx, tt.slug)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Get() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if product.Slug != tt.slug {
				t.Errorf("Get() slug = %v, want %v", product.Slug, tt.slug)
			}
		})
	}
}

func TestPostgresStore_List(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	store := NewPostgresStore(db)

	cleanup(t, db)

	for i := 0; i < 25; i++ {
		productID := insertTestProduct(t, ctx, db, fmt.Sprintf("product-%d", i), fmt.Sprintf("Product %d", i), "published")
		insertTestVariant(t, ctx, db, productID, fmt.Sprintf("var-%d", i), fmt.Sprintf("SKU-%d", i), 1000+int64(i), "USD")
	}

	tests := []struct {
		name      string
		opts      ListOptions
		wantCount int
		wantMore  bool
	}{
		{"default limit", ListOptions{}, 20, true},
		{"limit 10", ListOptions{Limit: 10}, 10, true},
		{"limit 30", ListOptions{Limit: 30}, 25, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			products, cursor, err := store.List(ctx, tt.opts)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(products) != tt.wantCount {
				t.Errorf("List() count = %d, want %d", len(products), tt.wantCount)
			}
			if (cursor != "") != tt.wantMore {
				t.Errorf("List() has_cursor = %v, want %v", cursor != "", tt.wantMore)
			}
		})
	}
}

func TestPostgresStore_ListPagination(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	store := NewPostgresStore(db)

	cleanup(t, db)

	for i := 0; i < 25; i++ {
		productID := insertTestProduct(t, ctx, db, fmt.Sprintf("product-%d", i), fmt.Sprintf("Product %d", i), "published")
		insertTestVariant(t, ctx, db, productID, fmt.Sprintf("var-%d", i), fmt.Sprintf("SKU-%d", i), 1000+int64(i), "USD")
	}

	firstPage, cursor, err := store.List(ctx, ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List first page error = %v", err)
	}
	if len(firstPage) != 10 {
		t.Errorf("First page count = %d, want 10", len(firstPage))
	}
	if cursor == "" {
		t.Fatal("First page should have cursor")
	}

	secondPage, _, err := store.List(ctx, ListOptions{Limit: 10, Cursor: cursor})
	if err != nil {
		t.Fatalf("List second page error = %v", err)
	}
	if len(secondPage) != 10 {
		t.Errorf("Second page count = %d, want 10", len(secondPage))
	}

	seen := make(map[string]bool)
	for _, p := range firstPage {
		seen[p.ID] = true
	}
	for _, p := range secondPage {
		if seen[p.ID] {
			t.Errorf("Product %s appears in both pages", p.ID)
		}
	}

	for _, p := range firstPage {
		if len(p.Variants) == 0 {
			t.Errorf("Product %s has no variants", p.ID)
		}
	}
}

func insertTestProduct(t *testing.T, ctx context.Context, db *sql.DB, slug, name, status string) string {
	var id string
	err := db.QueryRowContext(ctx, `
		INSERT INTO products (slug, name, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, slug, name, status, time.Now(), time.Now()).Scan(&id)
	if err != nil {
		t.Fatalf("insert test product: %v", err)
	}
	return id
}

func insertTestVariant(t *testing.T, ctx context.Context, db *sql.DB, productID, id, sku string, price int64, currency string) {
	_, err := db.ExecContext(ctx, `
		INSERT INTO variants (id, product_id, sku, price, currency, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, id, productID, sku, price, currency, time.Now(), time.Now())
	if err != nil {
		t.Fatalf("insert test variant: %v", err)
	}
}

func cleanup(t *testing.T, db *sql.DB) {
	ctx := context.Background()
	_, err := db.ExecContext(ctx, "TRUNCATE products CASCADE")
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}
