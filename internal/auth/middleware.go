package auth

import (
	"context"
	"crypto/sha256"
	"net/http"
	"strings"
)

type ctxKeyAPIKey struct{}

func KeyFromContext(ctx context.Context) (*Key, bool) {
	k, ok := ctx.Value(ctxKeyAPIKey{}).(*Key)
	return k, ok
}

type KeyStore interface {
	FindByHash(hash []byte) (*Key, error)
	FindByID(id string) (*Key, error)
	Create(name string, scopes []string, expiresAt *int64) (*Key, string, error)
	List() ([]Key, error)
	Revoke(id string) error
}

func RequireScope(next http.Handler, store KeyStore, scope string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			WriteAuthError(w, r, ErrMissingKey, "")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			WriteAuthError(w, r, ErrKeyInvalid, "")
			return
		}

		keyStr := parts[1]
		if len(keyStr) < 4 {
			WriteAuthError(w, r, ErrKeyInvalid, "")
			return
		}

		hash := sha256.Sum256([]byte(keyStr))
		key, err := store.FindByHash(hash[:])
		if err != nil {
			WriteAuthError(w, r, ErrKeyNotFound, "")
			return
		}

		if err := key.IsActive(); err != nil {
			WriteAuthError(w, r, err, key.Prefix)
			return
		}

		if !CompareKeyHash(key.Hash, keyStr) {
			WriteAuthError(w, r, ErrKeyInvalid, key.Prefix)
			return
		}

		if !key.Allows(scope) {
			WriteScopeError(w, r, scope, key.Prefix)
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyAPIKey{}, key)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
