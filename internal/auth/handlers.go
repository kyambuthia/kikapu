package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/kyambuthia/kikapu/internal/platform/httpx"
)

type CreateKeyRequest struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt *int64   `json:"expires_at,omitempty"`
}

type KeyResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix,omitempty"`
	Key       string     `json:"key,omitempty"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type ListKeysResponse struct {
	Data []KeyResponse `json:"data"`
}

func CreateKeyHandler(store KeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body", httpx.RequestID(r))
			return
		}

		if req.Name == "" {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "name is required", httpx.RequestID(r))
			return
		}

		if len(req.Scopes) == 0 {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "at least one scope is required", httpx.RequestID(r))
			return
		}

		key, keyStr, err := store.Create(req.Name, req.Scopes, req.ExpiresAt)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create key", httpx.RequestID(r))
			return
		}

		resp := KeyResponse{
			ID:        key.ID,
			Name:      key.Name,
			Prefix:    key.Prefix,
			Key:       keyStr,
			Scopes:    key.Scopes,
			ExpiresAt: key.ExpiresAt,
		}

		httpx.WriteJSON(w, http.StatusCreated, resp)
	}
}

func ListKeysHandler(store KeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keys, err := store.List()
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list keys", httpx.RequestID(r))
			return
		}

		data := make([]KeyResponse, len(keys))
		for i, key := range keys {
			data[i] = KeyResponse{
				ID:        key.ID,
				Name:      key.Name,
				Prefix:    key.Prefix,
				Scopes:    key.Scopes,
				ExpiresAt: key.ExpiresAt,
				RevokedAt: key.RevokedAt,
			}
		}

		httpx.WriteJSON(w, http.StatusOK, ListKeysResponse{Data: data})
	}
}

func RevokeKeyHandler(store KeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "key ID is required", httpx.RequestID(r))
			return
		}

		err := store.Revoke(id)
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				httpx.WriteError(w, http.StatusNotFound, "key_not_found", "key not found", httpx.RequestID(r))
				return
			}
			httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to revoke key", httpx.RequestID(r))
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
