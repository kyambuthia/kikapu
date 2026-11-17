package paystack

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/kyambuthia/kikapu/internal/payment"
)

func TestPostgresEventStore(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), "TRUNCATE paystack_webhook_events"); err != nil {
		t.Fatalf("truncate paystack_webhook_events: %v", err)
	}

	store := NewPostgresEventStore(db)
	if _, ok := store.GetEvent("evt_missing"); ok {
		t.Fatal("expected missing event to return ok=false")
	}

	ev := &ProcessedEvent{
		EventID:   "evt_101",
		Reference: "ref_101",
		State:     payment.StateCaptured,
		Timestamp: 1730000000,
	}
	if err := store.SaveEvent(ev); err != nil {
		t.Fatalf("SaveEvent() error = %v", err)
	}
	// Duplicate save should be idempotent (ON CONFLICT DO NOTHING).
	if err := store.SaveEvent(ev); err != nil {
		t.Fatalf("duplicate SaveEvent() error = %v", err)
	}

	got, ok := store.GetEvent("evt_101")
	if !ok {
		t.Fatal("expected saved event to be found")
	}
	if got.Reference != "ref_101" || got.State != payment.StateCaptured {
		t.Fatalf("got = %+v, want reference=ref_101 state=captured", got)
	}
}
