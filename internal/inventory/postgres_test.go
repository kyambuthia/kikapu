package inventory

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func setupInventoryPostgresTest(t *testing.T) (*sql.DB, *PostgresStore, *FakeClock) {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "TRUNCATE stock_levels, inventory_reservations"); err != nil {
		t.Fatalf("truncate inventory tables: %v", err)
	}

	clock := &FakeClock{Current: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	return db, NewPostgresStore(db, clock), clock
}

func TestPostgresStore_Lifecycle(t *testing.T) {
	_, store, clock := setupInventoryPostgresTest(t)
	ctx := context.Background()

	if err := store.SetLevel(ctx, "var-pg-1", "loc-1", 10); err != nil {
		t.Fatalf("SetLevel() error = %v", err)
	}

	res, err := store.Reserve(ctx, "var-pg-1", 4, 5*time.Minute)
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}

	levels, err := store.Levels(ctx, "var-pg-1")
	if err != nil || len(levels) != 1 || levels[0].Available() != 6 {
		t.Fatalf("Levels after reserve = %+v, err=%v (want Available=6)", levels, err)
	}

	if err := store.Commit(ctx, res.ID); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	levels, _ = store.Levels(ctx, "var-pg-1")
	if levels[0].OnHand != 6 || levels[0].Reserved != 0 {
		t.Fatalf("Levels after commit = %+v, want OnHand=6 Reserved=0", levels[0])
	}

	// Reserve and release
	res2, err := store.Reserve(ctx, "var-pg-1", 3, 5*time.Minute)
	if err != nil {
		t.Fatalf("Reserve() #2 error = %v", err)
	}
	if err := store.Release(ctx, res2.ID); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	levels, _ = store.Levels(ctx, "var-pg-1")
	if levels[0].Available() != 6 {
		t.Fatalf("Available after release = %d, want 6", levels[0].Available())
	}

	// Reserve and expire
	if _, err := store.Reserve(ctx, "var-pg-1", 2, 1*time.Minute); err != nil {
		t.Fatalf("Reserve() #3 error = %v", err)
	}
	clock.Add(2 * time.Minute)
	expired, err := store.ExpireDue(ctx, clock.Now())
	if err != nil || expired != 1 {
		t.Fatalf("ExpireDue() = (%d, %v), want (1, nil)", expired, err)
	}

	// Insufficient stock
	if _, err := store.Reserve(ctx, "var-pg-1", 99, 5*time.Minute); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("Reserve(99) error = %v, want ErrInsufficientStock", err)
	}
}
