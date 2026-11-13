package search

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func skipIfNoDatabase(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
}

func setupTestDB(t *testing.T) (*sql.DB, func()) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	cleanup := func() {
		db.Close()
	}

	return db, cleanup
}

func TestQuery_Ranking(t *testing.T) {
	skipIfNoDatabase(t)
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewPostgresStore(db)
	ctx := context.Background()

	tests := []struct {
		name           string
		query          string
		minResults     int
		checkRankOrder bool
	}{
		{
			name:           "FTS ranking by name weight",
			query:          "cotton",
			minResults:     1,
			checkRankOrder: true,
		},
		{
			name:           "FTS with multiple terms",
			query:          "organic cotton",
			minResults:     1,
			checkRankOrder: true,
		},
		{
			name:           "SKU search",
			query:          "SKU",
			minResults:     1,
			checkRankOrder: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := store.Query(ctx, tt.query, Filters{}, Page{Limit: 20})
			if err != nil {
				t.Fatalf("Query failed: %v", err)
			}

			if len(results.Data) < tt.minResults {
				t.Errorf("Expected at least %d results, got %d", tt.minResults, len(results.Data))
			}

			if tt.checkRankOrder && len(results.Data) > 1 {
				for i := 1; i < len(results.Data); i++ {
					if results.Data[i].Rank > results.Data[i-1].Rank {
						t.Errorf("Results not ordered by rank descending: position %d has rank %.4f, position %d has rank %.4f",
							i-1, results.Data[i-1].Rank, i, results.Data[i].Rank)
					}
				}
			}
		})
	}
}

func TestQuery_TypoTolerance(t *testing.T) {
	skipIfNoDatabase(t)
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewPostgresStore(db)
	ctx := context.Background()

	tests := []struct {
		name         string
		typoQuery    string
		correctQuery string
		shouldMatch  bool
	}{
		{
			name:         "Typo tolerance for 'sokomoko'",
			typoQuery:    "sockomoko",
			correctQuery: "sokomoko",
			shouldMatch:  true,
		},
		{
			name:         "Typo tolerance for 'coton'",
			typoQuery:    "coton",
			correctQuery: "cotton",
			shouldMatch:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typoResults, err := store.Query(ctx, tt.typoQuery, Filters{}, Page{Limit: 10})
			if err != nil {
				t.Fatalf("Typo query failed: %v", err)
			}

			correctResults, err := store.Query(ctx, tt.correctQuery, Filters{}, Page{Limit: 10})
			if err != nil {
				t.Fatalf("Correct query failed: %v", err)
			}

			if tt.shouldMatch && len(typoResults.Data) == 0 && len(correctResults.Data) > 0 {
				t.Errorf("Expected typo query '%s' to match results like correct query '%s'", tt.typoQuery, tt.correctQuery)
			}
		})
	}
}

func TestQuery_EmptyResult(t *testing.T) {
	skipIfNoDatabase(t)
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewPostgresStore(db)
	ctx := context.Background()

	results, err := store.Query(ctx, "xyznonexistentproduct123", Filters{}, Page{Limit: 20})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(results.Data) != 0 {
		t.Errorf("Expected empty results for nonexistent product, got %d results", len(results.Data))
	}
}

func TestQuery_CursorStability(t *testing.T) {
	skipIfNoDatabase(t)
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewPostgresStore(db)
	ctx := context.Background()

	results1, err := store.Query(ctx, "cotton", Filters{}, Page{Limit: 5})
	if err != nil {
		t.Fatalf("First query failed: %v", err)
	}

	if results1.NextCursor == "" {
		t.Skip("Not enough results to test cursor pagination")
	}

	results2, err := store.Query(ctx, "cotton", Filters{}, Page{Limit: 5, Cursor: results1.NextCursor})
	if err != nil {
		t.Fatalf("Second query with cursor failed: %v", err)
	}

	for _, r1 := range results1.Data {
		for _, r2 := range results2.Data {
			if r1.ID == r2.ID {
				t.Errorf("Product %s appears in both pages", r1.ID)
			}
		}
	}

	if len(results1.Data) > 0 && len(results2.Data) > 0 {
		if results1.Data[len(results1.Data)-1].Rank < results2.Data[0].Rank {
			t.Errorf("Cursor pagination does not maintain rank order across pages")
		}
	}
}

func TestQuery_SQLInjection(t *testing.T) {
	skipIfNoDatabase(t)
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewPostgresStore(db)
	ctx := context.Background()

	maliciousQueries := []string{
		"'; DROP TABLE products; --",
		"' OR '1'='1",
		"' UNION SELECT * FROM products --",
		"1; DELETE FROM products WHERE 1=1; --",
	}

	for _, q := range maliciousQueries {
		t.Run(fmt.Sprintf("Malicious query: %.20s", q), func(t *testing.T) {
			results, err := store.Query(ctx, q, Filters{}, Page{Limit: 20})
			if err != nil {
				t.Logf("Query correctly returned error: %v", err)
				return
			}

			t.Logf("Query treated as text search, returned %d results (safe)", len(results.Data))

			var count int
			err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM products").Scan(&count)
			if err != nil {
				t.Fatalf("Failed to verify table integrity: %v", err)
			}
			if count == 0 {
				t.Errorf("SQL injection may have modified data!")
			}
		})
	}
}

func TestQuery_ValidationErrors(t *testing.T) {
	skipIfNoDatabase(t)
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewPostgresStore(db)
	ctx := context.Background()

	t.Run("Empty query", func(t *testing.T) {
		_, err := store.Query(ctx, "", Filters{}, Page{Limit: 20})
		if err != ErrInvalidQuery {
			t.Errorf("Expected ErrInvalidQuery, got %v", err)
		}
	})

	t.Run("Invalid limit", func(t *testing.T) {
		_, err := store.Query(ctx, "test", Filters{}, Page{Limit: 101})
		if err != ErrInvalidLimit {
			t.Errorf("Expected ErrInvalidLimit, got %v", err)
		}
	})

	t.Run("Invalid cursor", func(t *testing.T) {
		_, err := store.Query(ctx, "test", Filters{}, Page{Limit: 20, Cursor: "invalid-cursor"})
		if err != ErrInvalidCursor {
			t.Errorf("Expected ErrInvalidCursor, got %v", err)
		}
	})
}
