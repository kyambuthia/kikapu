package auth

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PostgresStore persists API keys in PostgreSQL.
type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) FindByHash(hash []byte) (*Key, error) {
	return s.scanKey(`
		SELECT id, name, prefix, hash, scopes, expires_at, revoked_at
		FROM api_keys
		WHERE hash = $1
	`, hash)
}

func (s *PostgresStore) FindByID(id string) (*Key, error) {
	return s.scanKey(`
		SELECT id, name, prefix, hash, scopes, expires_at, revoked_at
		FROM api_keys
		WHERE id = $1
	`, id)
}

func (s *PostgresStore) scanKey(query string, arg interface{}) (*Key, error) {
	var (
		k         Key
		scopesRaw string
		expiresAt sql.NullTime
		revokedAt sql.NullTime
	)
	err := s.db.QueryRow(query, arg).Scan(
		&k.ID, &k.Name, &k.Prefix, &k.Hash, &scopesRaw, &expiresAt, &revokedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("auth: query key: %w", err)
	}
	k.Scopes = parsePGTextArray(scopesRaw)
	if expiresAt.Valid {
		t := expiresAt.Time.UTC()
		k.ExpiresAt = &t
	}
	if revokedAt.Valid {
		t := revokedAt.Time.UTC()
		k.RevokedAt = &t
	}
	return &k, nil
}

func (s *PostgresStore) Create(name string, scopes []string, expiresAt *int64) (*Key, string, error) {
	keyStr, hash, err := GenerateKey()
	if err != nil {
		return nil, "", err
	}

	k := &Key{
		ID:     generateID(),
		Name:   name,
		Prefix: ExtractPrefix(keyStr),
		Hash:   hash,
		Scopes: scopes,
	}
	if expiresAt != nil {
		t := time.Unix(*expiresAt, 0).UTC()
		k.ExpiresAt = &t
	}

	_, err = s.db.Exec(`
		INSERT INTO api_keys (id, name, prefix, hash, scopes, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, k.ID, k.Name, k.Prefix, k.Hash, formatPGTextArray(k.Scopes), k.ExpiresAt)
	if err != nil {
		return nil, "", fmt.Errorf("auth: create key: %w", err)
	}

	return k, keyStr, nil
}

// Seed registers a pre-shared API key idempotently (for example from KIKAPU_ADMIN_KEY).
func (s *PostgresStore) Seed(name, rawKey string, scopes []string) (*Key, error) {
	if rawKey == "" {
		return nil, ErrKeyInvalid
	}
	hash := sha256.Sum256([]byte(rawKey))
	k := &Key{
		ID:     generateID(),
		Name:   name,
		Prefix: ExtractPrefix(rawKey),
		Hash:   hash[:],
		Scopes: scopes,
	}
	_, err := s.db.Exec(`
		INSERT INTO api_keys (id, name, prefix, hash, scopes)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (hash) DO NOTHING
	`, k.ID, k.Name, k.Prefix, k.Hash, formatPGTextArray(k.Scopes))
	if err != nil {
		return nil, fmt.Errorf("auth: seed key: %w", err)
	}
	return k, nil
}

func (s *PostgresStore) List() ([]Key, error) {
	rows, err := s.db.Query(`
		SELECT id, name, prefix, scopes, expires_at, revoked_at
		FROM api_keys
		ORDER BY created_at
	`)
	if err != nil {
		return nil, fmt.Errorf("auth: list keys: %w", err)
	}
	defer rows.Close()

	var keys []Key
	for rows.Next() {
		var (
			k         Key
			scopesRaw string
			expiresAt sql.NullTime
			revokedAt sql.NullTime
		)
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &scopesRaw, &expiresAt, &revokedAt); err != nil {
			return nil, fmt.Errorf("auth: scan key: %w", err)
		}
		k.Scopes = parsePGTextArray(scopesRaw)
		if expiresAt.Valid {
			t := expiresAt.Time.UTC()
			k.ExpiresAt = &t
		}
		if revokedAt.Valid {
			t := revokedAt.Time.UTC()
			k.RevokedAt = &t
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: iterate keys: %w", err)
	}
	return keys, nil
}

func (s *PostgresStore) Revoke(id string) error {
	now := time.Now().UTC()
	res, err := s.db.Exec(`
		UPDATE api_keys
		SET revoked_at = $2
		WHERE id = $1
	`, id, now)
	if err != nil {
		return fmt.Errorf("auth: revoke key: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("auth: rows affected: %w", err)
	}
	if rows == 0 {
		return ErrKeyNotFound
	}
	return nil
}

func parsePGTextArray(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return nil
	}
	trimmed = strings.TrimPrefix(trimmed, "{")
	trimmed = strings.TrimSuffix(trimmed, "}")
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(strings.TrimSpace(p), `"`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func formatPGTextArray(items []string) string {
	escaped := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.ReplaceAll(item, `"`, `\"`)
		escaped = append(escaped, `"`+item+`"`)
	}
	return "{" + strings.Join(escaped, ",") + "}"
}
