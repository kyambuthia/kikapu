package channel

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresRepository(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	repo := NewPostgresRepository(db)
	ctx := context.Background()

	list, err := repo.List(ctx)
	if err != nil || len(list) < 1 {
		t.Fatalf("List() = (%d items, %v), want >= 1", len(list), err)
	}

	got, err := repo.Get(ctx, list[0].ID)
	if err != nil || got.ID != list[0].ID {
		t.Fatalf("Get(%s) = (%+v, %v)", list[0].ID, got, err)
	}

	bySlug, err := repo.GetBySlug(ctx, list[0].Slug)
	if err != nil || bySlug.ID != list[0].ID {
		t.Fatalf("GetBySlug(%s) = (%+v, %v)", list[0].Slug, bySlug, err)
	}

	if _, err := repo.Get(ctx, "missing-channel"); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("Get(missing) = %v, want ErrChannelNotFound", err)
	}
}
