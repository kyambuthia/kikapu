package checkout

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresOrderStore(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), "TRUNCATE checkout_orders"); err != nil {
		t.Fatalf("truncate checkout_orders: %v", err)
	}

	store := NewPostgresOrderStore(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	ord := &Order{
		ID:           "chk_ord_1",
		CartID:       "cart_1",
		Status:       OrderStatusPaid,
		Total:        2500,
		Currency:     "USD",
		PaymentID:    "pay_1",
		Reservations: []string{"res_1"},
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := store.SaveOrder("idem-key-1", ord); err != nil {
		t.Fatalf("SaveOrder() error = %v", err)
	}

	got, ok := store.GetOrderForKey("idem-key-1")
	if !ok || got.ID != "chk_ord_1" || len(got.Reservations) != 1 {
		t.Fatalf("GetOrderForKey() = (%+v, %v)", got, ok)
	}

	byID, err := store.GetOrder("chk_ord_1")
	if err != nil || byID.PaymentID != "pay_1" {
		t.Fatalf("GetOrder() = (%+v, %v)", byID, err)
	}

	if _, err := store.GetOrder("missing"); !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("GetOrder(missing) = %v, want ErrOrderNotFound", err)
	}
}
