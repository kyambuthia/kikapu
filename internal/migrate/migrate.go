package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func Run(databaseURL string, migrationsDir string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("migrate: open database: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("migrate: ping database: %w", err)
	}

	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("migrate: create schema_migrations table: %w", err)
	}

	advisoryLockID := int64(1234567890)
	_, err = db.ExecContext(ctx, "SELECT pg_advisory_lock($1)", advisoryLockID)
	if err != nil {
		return fmt.Errorf("migrate: acquire advisory lock: %w", err)
	}
	defer db.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", advisoryLockID)

	files, err := findMigrationFiles(migrationsDir)
	if err != nil {
		return fmt.Errorf("migrate: find migrations: %w", err)
	}

	for _, file := range files {
		version := extractVersion(file)
		if version == "" {
			continue
		}

		var exists bool
		err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("migrate: check version %s: %w", version, err)
		}

		if exists {
			fmt.Printf("Migration %s already applied\n", version)
			continue
		}

		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("migrate: read %s: %w", file, err)
		}

		fmt.Printf("Applying migration %s...\n", version)

		_, err = db.ExecContext(ctx, string(content))
		if err != nil {
			return fmt.Errorf("migrate: apply %s: %w", version, err)
		}

		fmt.Printf("Migration %s applied successfully\n", version)
	}

	return nil
}

func findMigrationFiles(dir string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".sql") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

func extractVersion(filename string) string {
	base := filepath.Base(filename)
	parts := strings.Split(base, "_")
	if len(parts) < 1 {
		return ""
	}

	version := parts[0]
	if _, err := strconv.Atoi(version); err != nil {
		return ""
	}

	return version
}
