package migrate

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	tmpDir := t.TempDir()

	migrationFile := filepath.Join(tmpDir, "9998_test.sql")
	content := `
-- Test migration
-- Why: Verify migration works
-- Reversible: no

CREATE TABLE test_migration (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL
);

INSERT INTO test_migration (name) VALUES ('test');

CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO schema_migrations (version) VALUES ('9998') ON CONFLICT (version) DO NOTHING;
`
	if err := os.WriteFile(migrationFile, []byte(content), 0644); err != nil {
		t.Fatalf("write migration file: %v", err)
	}

	err := Run(dbURL, tmpDir)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	var name string
	err = db.QueryRow("SELECT name FROM test_migration WHERE id = 1").Scan(&name)
	if err != nil {
		t.Fatalf("query test_migration: %v", err)
	}
	if name != "test" {
		t.Errorf("test_migration.name = %v, want test", name)
	}

	_, err = db.Exec("DROP TABLE IF EXISTS test_migration CASCADE")
	if err != nil {
		t.Logf("cleanup test_migration: %v", err)
	}
	_, err = db.Exec("DELETE FROM schema_migrations WHERE version = '9998'")
	if err != nil {
		t.Logf("cleanup schema_migrations: %v", err)
	}
}

func TestRun_Idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	tmpDir := t.TempDir()

	migrationFile := filepath.Join(tmpDir, "9999_test.sql")
	content := `
-- Test migration
-- Why: Verify idempotent migration
-- Reversible: no

CREATE TABLE test_idempotent (
    id SERIAL PRIMARY KEY,
    value INT NOT NULL
);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO schema_migrations (version) VALUES ('9999') ON CONFLICT (version) DO NOTHING;
`
	if err := os.WriteFile(migrationFile, []byte(content), 0644); err != nil {
		t.Fatalf("write migration file: %v", err)
	}

	err := Run(dbURL, tmpDir)
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec("INSERT INTO test_idempotent (value) VALUES (1)")
	if err != nil {
		t.Fatalf("insert test_idempotent: %v", err)
	}

	err = Run(dbURL, tmpDir)
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM test_idempotent").Scan(&count)
	if err != nil {
		t.Fatalf("count test_idempotent: %v", err)
	}
	if count != 1 {
		t.Errorf("test_idempotent count = %d, want 1", count)
	}

	_, err = db.Exec("DROP TABLE IF EXISTS test_idempotent CASCADE")
	if err != nil {
		t.Logf("cleanup test_idempotent: %v", err)
	}
	_, err = db.Exec("DELETE FROM schema_migrations WHERE version = '9999'")
	if err != nil {
		t.Logf("cleanup schema_migrations: %v", err)
	}
}

func TestExtractVersion(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"0001_init.sql", "0001"},
		{"0002_add_product_slug.sql", "0002"},
		{"0010_add_orders_table.sql", "0010"},
		{"invalid.sql", ""},
		{"no_version_test.sql", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := extractVersion(tt.filename)
			if got != tt.want {
				t.Errorf("extractVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindMigrationFiles(t *testing.T) {
	tmpDir := t.TempDir()

	files := []string{
		"0001_init.sql",
		"0002_add_users.sql",
		"0003_add_orders.sql",
		"README.md",
		"config.yaml",
	}

	for _, f := range files {
		path := filepath.Join(tmpDir, f)
		if err := os.WriteFile(path, []byte("-- test"), 0644); err != nil {
			t.Fatalf("write file %s: %v", f, err)
		}
	}

	found, err := findMigrationFiles(tmpDir)
	if err != nil {
		t.Fatalf("findMigrationFiles() error = %v", err)
	}

	if len(found) != 3 {
		t.Errorf("findMigrationFiles() count = %d, want 3", len(found))
	}

	for i, f := range found {
		expected := filepath.Join(tmpDir, files[i])
		if f != expected {
			t.Errorf("findMigrationFiles()[%d] = %v, want %v", i, f, expected)
		}
	}
}
