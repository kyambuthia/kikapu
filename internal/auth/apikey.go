package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"time"
)

var (
	ErrKeyExpired  = errors.New("api key has expired")
	ErrKeyRevoked  = errors.New("api key has been revoked")
	ErrKeyInvalid  = errors.New("api key is invalid")
	ErrKeyNotFound = errors.New("api key not found")
	ErrMissingKey  = errors.New("api key is required")
)

type Key struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	Hash      []byte     `json:"-"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

func (k Key) Allows(scope string) bool {
	for _, s := range k.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

func (k Key) IsActive() error {
	now := time.Now().UTC()
	if k.RevokedAt != nil && !k.RevokedAt.IsZero() {
		if now.After(*k.RevokedAt) || now.Equal(*k.RevokedAt) {
			return ErrKeyRevoked
		}
	}
	if k.ExpiresAt != nil && !k.ExpiresAt.IsZero() {
		if now.After(*k.ExpiresAt) {
			return ErrKeyExpired
		}
	}
	return nil
}

func CompareKeyHash(storedHash []byte, providedKey string) bool {
	hash := sha256.Sum256([]byte(providedKey))
	return subtle.ConstantTimeCompare(storedHash, hash[:]) == 1
}

func GenerateKey() (string, []byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	key := "kik_" + base64.RawURLEncoding.EncodeToString(b)
	hash := sha256.Sum256([]byte(key))
	return key, hash[:], nil
}

func ExtractPrefix(key string) string {
	if len(key) < 12 {
		return key
	}
	return key[:8]
}
