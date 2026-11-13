package worker

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresSubscriberStore(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "TRUNCATE webhook_subscriptions"); err != nil {
		t.Fatalf("truncate webhook_subscriptions: %v", err)
	}

	store := NewPostgresSubscriberStore(db)
	_ = store.Add(ctx, Subscription{ID: "s1", URL: "https://a.test/hook", Secret: "sec1", Types: []string{"order.paid"}, Active: true})
	_ = store.Add(ctx, Subscription{ID: "s2", URL: "https://b.test/hook", Secret: "sec2", Types: []string{"*"}, Active: true})
	_ = store.Add(ctx, Subscription{ID: "s3", URL: "https://c.test/hook", Secret: "sec3", Types: []string{"order.paid"}, Active: false})

	all, err := store.List(ctx)
	if err != nil || len(all) != 3 {
		t.Fatalf("List() = (%d, %v), want 3", len(all), err)
	}

	matched, err := store.ListByEventType(ctx, "order.paid")
	if err != nil || len(matched) != 2 {
		t.Fatalf("ListByEventType(order.paid) = (%d, %v), want 2", len(matched), err)
	}
}
