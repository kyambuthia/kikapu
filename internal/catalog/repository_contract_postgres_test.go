package catalog

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresRepository_Contract(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	contract := RepositoryContract{
		NewRepo: func(t *testing.T) Repository {
			db, err := sql.Open("pgx", dbURL)
			if err != nil {
				t.Fatalf("open database: %v", err)
			}

			ctx := context.Background()
			cleanupCatalog(t, db)

			productID1 := insertCatalogTestProduct(t, ctx, db, "wireless-mouse", "Wireless Mouse", "published")
			insertCatalogTestVariant(t, ctx, db, productID1, "var-001", "WM-BLK-001", 2999, "USD")

			productID2 := insertCatalogTestProduct(t, ctx, db, "wireless-keyboard", "Wireless Keyboard", "published")
			insertCatalogTestVariant(t, ctx, db, productID2, "var-002", "WK-001", 5999, "USD")

			t.Cleanup(func() { db.Close() })

			return NewPostgresStore(db)
		},
	}

	contract.Run(t)
}

func insertCatalogTestProduct(t *testing.T, ctx context.Context, db *sql.DB, slug, name, status string) string {
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

func insertCatalogTestVariant(t *testing.T, ctx context.Context, db *sql.DB, productID, _, sku string, price int64, currency string) {
	_, err := db.ExecContext(ctx, `
		INSERT INTO variants (product_id, sku, price, currency, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, productID, sku, price, currency, time.Now(), time.Now())
	if err != nil {
		t.Fatalf("insert test variant: %v", err)
	}
}

func cleanupCatalog(t *testing.T, db *sql.DB) {
	ctx := context.Background()
	_, err := db.ExecContext(ctx, "TRUNCATE products CASCADE")
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}
