package order

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/kyambuthia/kikapu/internal/platform/money"
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

	cleanupOrders(t, db)

	orderID := insertTestOrder(t, ctx, db, "00000000-0000-0000-0000-000000000001", "KIK-001", "reg-001", 10000, 0, 1600, 11600, "USD")
	insertTestOrderLine(t, ctx, db, orderID, "var-001", "SKU-001", 2, 5000, 0, 800, "USD")

	tests := []struct {
		name    string
		id      string
		wantErr error
	}{
		{"found", orderID, nil},
		{"not found", "nonexistent", ErrOrderNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order, err := store.Get(ctx, tt.id)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Get() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if order.ID != tt.id {
				t.Errorf("Get() id = %v, want %v", order.ID, tt.id)
			}
			if len(order.Lines) != 1 {
				t.Errorf("Get() lines count = %d, want 1", len(order.Lines))
			}
		})
	}
}

func TestPostgresStore_Create(t *testing.T) {
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

	cleanupOrders(t, db)

	order := &Order{
		ID:                "00000000-0000-0000-0000-000000000001",
		Number:            "KIK-001",
		RegionID:          "reg-001",
		Status:            StatusPending,
		PaymentStatus:     PaymentStatusPending,
		FulfillmentStatus: FulfillmentStatusUnfulfilled,
		Lines: []Line{
			{
				VariantID: "var-001",
				SKU:       "SKU-001",
				Quantity:  2,
				UnitPrice: money.NewAmount(5000, "USD"),
				Discount:  money.NewAmount(0, "USD"),
				Tax:       money.NewAmount(800, "USD"),
			},
		},
		Totals: Totals{
			Subtotal:   money.NewAmount(10000, "USD"),
			Discount:   money.NewAmount(0, "USD"),
			Tax:        money.NewAmount(1600, "USD"),
			GrandTotal: money.NewAmount(11600, "USD"),
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err = store.Create(ctx, nil, order)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	retrieved, err := store.Get(ctx, order.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if retrieved.ID != order.ID {
		t.Errorf("Retrieved order ID = %v, want %v", retrieved.ID, order.ID)
	}
	if len(retrieved.Lines) != 1 {
		t.Errorf("Retrieved lines count = %d, want 1", len(retrieved.Lines))
	}
}

func TestPostgresStore_Save(t *testing.T) {
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

	cleanupOrders(t, db)

	orderID := insertTestOrder(t, ctx, db, "00000000-0000-0000-0000-000000000001", "KIK-001", "reg-001", 10000, 0, 1600, 11600, "USD")
	insertTestOrderLine(t, ctx, db, orderID, "var-001", "SKU-001", 2, 5000, 0, 800, "USD")

	order, err := store.Get(ctx, orderID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	order.Status = StatusConfirmed
	order.PaymentStatus = PaymentStatusCaptured
	order.UpdatedAt = time.Now()

	err = store.Save(ctx, order)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	retrieved, err := store.Get(ctx, orderID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if retrieved.Status != StatusConfirmed {
		t.Errorf("Retrieved status = %v, want %v", retrieved.Status, StatusConfirmed)
	}
	if retrieved.PaymentStatus != PaymentStatusCaptured {
		t.Errorf("Retrieved payment status = %v, want %v", retrieved.PaymentStatus, PaymentStatusCaptured)
	}
}

func TestPostgresStore_RefundConstraint(t *testing.T) {
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

	cleanupOrders(t, db)

	orderID := insertTestOrder(t, ctx, db, "00000000-0000-0000-0000-000000000001", "KIK-001", "reg-001", 10000, 0, 1600, 11600, "USD")
	insertTestOrderLine(t, ctx, db, orderID, "var-001", "SKU-001", 2, 5000, 0, 800, "USD")

	order, err := store.Get(ctx, orderID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	order.PaymentStatus = PaymentStatusCaptured
	order.UpdatedAt = time.Now()

	err = store.Save(ctx, order)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	order, err = store.Get(ctx, orderID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	refundAmount := money.NewAmount(20000, "USD")
	_, err = order.Refund(refundAmount, "Test refund")
	if err != ErrRefundExceedsCaptured {
		t.Errorf("Refund() error = %v, want %v", err, ErrRefundExceedsCaptured)
	}
}

func TestPostgresStore_DuplicateOrderNumber(t *testing.T) {
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

	cleanupOrders(t, db)

	order1 := &Order{
		ID:                "00000000-0000-0000-0000-000000000001",
		Number:            "KIK-001",
		RegionID:          "reg-001",
		Status:            StatusPending,
		PaymentStatus:     PaymentStatusPending,
		FulfillmentStatus: FulfillmentStatusUnfulfilled,
		Totals: Totals{
			Subtotal:   money.NewAmount(10000, "USD"),
			Discount:   money.NewAmount(0, "USD"),
			Tax:        money.NewAmount(1600, "USD"),
			GrandTotal: money.NewAmount(11600, "USD"),
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err = store.Create(ctx, nil, order1)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	order2 := &Order{
		ID:                "00000000-0000-0000-0000-000000000002",
		Number:            "KIK-001",
		RegionID:          "reg-001",
		Status:            StatusPending,
		PaymentStatus:     PaymentStatusPending,
		FulfillmentStatus: FulfillmentStatusUnfulfilled,
		Totals: Totals{
			Subtotal:   money.NewAmount(10000, "USD"),
			Discount:   money.NewAmount(0, "USD"),
			Tax:        money.NewAmount(1600, "USD"),
			GrandTotal: money.NewAmount(11600, "USD"),
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err = store.Create(ctx, nil, order2)
	if err == nil {
		t.Error("Create() should fail with duplicate order number in same region")
	}
}

func insertTestOrder(t *testing.T, ctx context.Context, db *sql.DB, id, number, regionID string, subtotal, discount, tax, grandTotal int64, currency string) string {
	err := db.QueryRowContext(ctx, `
		INSERT INTO orders (id, number, region_id, status, payment_status, fulfillment_status, 
		                    subtotal, discount, tax, grand_total, currency, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id
	`, id, number, regionID, StatusPending, PaymentStatusPending, FulfillmentStatusUnfulfilled,
		subtotal, discount, tax, grandTotal, currency, time.Now(), time.Now()).Scan(&id)
	if err != nil {
		t.Fatalf("insert test order: %v", err)
	}
	return id
}

func insertTestOrderLine(t *testing.T, ctx context.Context, db *sql.DB, orderID, variantID, sku string, quantity int, unitPrice, discount, tax int64, currency string) {
	_, err := db.ExecContext(ctx, `
		INSERT INTO order_lines (order_id, variant_id, sku, quantity, unit_price, discount, tax, currency, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, orderID, variantID, sku, quantity, unitPrice, discount, tax, currency, time.Now())
	if err != nil {
		t.Fatalf("insert test order line: %v", err)
	}
}

func cleanupOrders(t *testing.T, db *sql.DB) {
	ctx := context.Background()
	_, err := db.ExecContext(ctx, "TRUNCATE orders CASCADE")
	if err != nil {
		t.Fatalf("cleanup orders: %v", err)
	}
}
