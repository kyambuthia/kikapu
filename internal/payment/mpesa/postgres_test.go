package mpesa

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/kyambuthia/kikapu/internal/payment"
)

func TestPostgresCallbackStore(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), "TRUNCATE mpesa_callbacks"); err != nil {
		t.Fatalf("truncate mpesa_callbacks: %v", err)
	}

	store := NewPostgresCallbackStore(db)
	if _, ok := store.GetCallback("ws_missing", 0); ok {
		t.Fatal("expected missing callback to return ok=false")
	}

	cb := &ProcessedCallback{
		CheckoutRequestID: "ws_CO_123",
		MerchantRequestID: "mr_123",
		ResultCode:        0,
		ResultDesc:        "The service request is processed successfully.",
		Amount:            1500,
		MpesaReceipt:      "QWE123RTY",
		PhoneNumber:       "254712345678",
		TransactionDate:   20261117093000,
		State:             payment.StateCaptured,
	}
	if err := store.SaveCallback(cb); err != nil {
		t.Fatalf("SaveCallback() error = %v", err)
	}
	if err := store.SaveCallback(cb); err != nil {
		t.Fatalf("duplicate SaveCallback() error = %v", err)
	}

	got, ok := store.GetCallback("ws_CO_123", 0)
	if !ok {
		t.Fatal("expected saved callback to be found")
	}
	if got.MpesaReceipt != "QWE123RTY" || got.Amount != 1500 || got.State != payment.StateCaptured {
		t.Fatalf("got = %+v, want receipt=QWE123RTY amount=1500 state=captured", got)
	}
}
