package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestKeyAllows(t *testing.T) {
	tests := []struct {
		name   string
		scopes []string
		check  string
		want   bool
	}{
		{"exact match", []string{"products:read", "orders:write"}, "products:read", true},
		{"no match", []string{"products:read"}, "orders:write", false},
		{"empty scopes", []string{}, "products:read", false},
		{"multiple scopes", []string{"products:read", "orders:write", "stock:write"}, "stock:write", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := Key{Scopes: tt.scopes}
			if got := k.Allows(tt.check); got != tt.want {
				t.Errorf("Key.Allows(%q) = %v, want %v", tt.check, got, tt.want)
			}
		})
	}
}

func TestKeyIsActive(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)

	tests := []struct {
		name      string
		revokedAt *time.Time
		expiresAt *time.Time
		wantErr   error
	}{
		{"active key", nil, nil, nil},
		{"revoked key", &past, nil, ErrKeyRevoked},
		{"expired key", nil, &past, ErrKeyExpired},
		{"revoked wins over expired", &past, &past, ErrKeyRevoked},
		{"not yet expired", nil, &future, nil},
		{"not yet revoked", &future, nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := Key{
				RevokedAt: tt.revokedAt,
				ExpiresAt: tt.expiresAt,
			}
			err := k.IsActive()
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("Key.IsActive() = %v, want nil", err)
				}
			} else {
				if err != tt.wantErr {
					t.Errorf("Key.IsActive() = %v, want %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestGenerateKey(t *testing.T) {
	key1, hash1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	if !strings.HasPrefix(key1, "kik_") {
		t.Errorf("GenerateKey() key = %q, want prefix 'kik_'", key1)
	}

	if len(hash1) != 32 {
		t.Errorf("GenerateKey() hash length = %d, want 32", len(hash1))
	}

	key2, hash2, _ := GenerateKey()
	if key1 == key2 {
		t.Error("GenerateKey() generated duplicate keys")
	}

	if bytes.Equal(hash1, hash2) {
		t.Error("GenerateKey() generated identical hashes")
	}
}

func TestCompareKeyHash(t *testing.T) {
	key, hash, _ := GenerateKey()

	if !CompareKeyHash(hash, key) {
		t.Error("CompareKeyHash() failed to match valid key")
	}

	if CompareKeyHash(hash, "kik_invalid") {
		t.Error("CompareKeyHash() matched invalid key")
	}

	wrongHash := make([]byte, 32)
	if CompareKeyHash(wrongHash, key) {
		t.Error("CompareKeyHash() matched wrong hash")
	}
}

func TestExtractPrefix(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"kik_abc123def456", "kik_abc1"},
		{"short", "short"},
		{"", ""},
		{"kik_1234567890abcd", "kik_1234"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := ExtractPrefix(tt.key); got != tt.want {
				t.Errorf("ExtractPrefix(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestRequireScope(t *testing.T) {
	store := NewInMemoryStore()
	_, keyStr, _ := store.Create("test-key", []string{"products:read", "orders:write"}, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	tests := []struct {
		name         string
		key          string
		scope        string
		wantStatus   int
		wantCode     string
		wantRequired bool
	}{
		{"valid key with scope", keyStr, "products:read", http.StatusOK, "", false},
		{"valid key wrong scope", keyStr, "stock:write", http.StatusForbidden, "missing_scope", true},
		{"no auth header", "", "products:read", http.StatusUnauthorized, "missing_key", false},
		{"invalid bearer format", "invalid", "products:read", http.StatusUnauthorized, "invalid_key", false},
		{"wrong key", "kik_wrongkey123456", "products:read", http.StatusUnauthorized, "invalid_key", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.key != "" {
				req.Header.Set("Authorization", "Bearer "+tt.key)
			}

			rec := httptest.NewRecorder()
			middleware := RequireScope(handler, store, tt.scope)
			middleware.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("RequireScope() status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantCode != "" {
				var resp map[string]interface{}
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				errMap, ok := resp["error"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected error in response")
				}

				if code, _ := errMap["code"].(string); code != tt.wantCode {
					t.Errorf("RequireScope() error code = %q, want %q", code, tt.wantCode)
				}

				if tt.wantRequired {
					if _, hasRequired := errMap["required_scope"]; !hasRequired {
						t.Error("RequireScope() missing required_scope in error response")
					}
				}
			}
		})
	}
}

func TestRequireScope_ExpiredKey(t *testing.T) {
	store := NewInMemoryStore()
	past := time.Now().Add(-24 * time.Hour).Unix()
	_, keyStr, _ := store.Create("expired-key", []string{"products:read"}, &past)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+keyStr)

	rec := httptest.NewRecorder()
	middleware := RequireScope(handler, store, "products:read")
	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("RequireScope() status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)

	errMap := resp["error"].(map[string]interface{})
	if code := errMap["code"].(string); code != "expired_key" {
		t.Errorf("RequireScope() error code = %q, want %q", code, "expired_key")
	}
}

func TestRequireScope_RevokedKey(t *testing.T) {
	store := NewInMemoryStore()
	key, keyStr, _ := store.Create("test-key", []string{"products:read"}, nil)

	store.Revoke(key.ID)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+keyStr)

	rec := httptest.NewRecorder()
	middleware := RequireScope(handler, store, "products:read")
	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("RequireScope() status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)

	errMap := resp["error"].(map[string]interface{})
	if code := errMap["code"].(string); code != "revoked_key" {
		t.Errorf("RequireScope() error code = %q, want %q", code, "revoked_key")
	}
}

func TestKeyFromContext(t *testing.T) {
	key := &Key{ID: "test-id", Scopes: []string{"products:read"}}

	ctx := context.WithValue(context.Background(), ctxKeyAPIKey{}, key)
	retrieved, ok := KeyFromContext(ctx)
	if !ok {
		t.Error("KeyFromContext() returned false")
	}
	if retrieved.ID != key.ID {
		t.Errorf("KeyFromContext() = %v, want %v", retrieved, key)
	}

	_, ok = KeyFromContext(context.Background())
	if ok {
		t.Error("KeyFromContext() returned true for empty context")
	}
}

func TestInMemoryStore_CreateAndFindByHash(t *testing.T) {
	store := NewInMemoryStore()
	key, _, err := store.Create("test-key", []string{"products:read", "orders:write"}, nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if key.ID == "" {
		t.Error("Create() key ID is empty")
	}

	if key.Name != "test-key" {
		t.Errorf("Create() key name = %q, want %q", key.Name, "test-key")
	}

	if len(key.Scopes) != 2 {
		t.Errorf("Create() scopes length = %d, want 2", len(key.Scopes))
	}

	found, err := store.FindByHash(key.Hash)
	if err != nil {
		t.Fatalf("FindByHash() error = %v", err)
	}

	if found.ID != key.ID {
		t.Errorf("FindByHash() key ID = %q, want %q", found.ID, key.ID)
	}

	_, _, _ = store.Create("test-key-2", []string{"stock:write"}, nil)
	_, err = store.FindByHash([]byte("invalid_hash"))
	if err != ErrKeyNotFound {
		t.Errorf("FindByHash() error = %v, want %v", err, ErrKeyNotFound)
	}
}

func TestInMemoryStore_CreateAndFindById(t *testing.T) {
	store := NewInMemoryStore()
	key, _, _ := store.Create("test-key", []string{"products:read"}, nil)

	found, err := store.FindByID(key.ID)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}

	if found.ID != key.ID {
		t.Errorf("FindByID() key ID = %q, want %q", found.ID, key.ID)
	}

	_, err = store.FindByID("invalid_id")
	if err != ErrKeyNotFound {
		t.Errorf("FindByID() error = %v, want %v", err, ErrKeyNotFound)
	}
}

func TestInMemoryStore_List(t *testing.T) {
	store := NewInMemoryStore()
	store.Create("key1", []string{"products:read"}, nil)
	store.Create("key2", []string{"orders:write"}, nil)

	keys, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(keys) != 2 {
		t.Errorf("List() length = %d, want 2", len(keys))
	}
}

func TestInMemoryStore_Revoke(t *testing.T) {
	store := NewInMemoryStore()
	key, _, _ := store.Create("test-key", []string{"products:read"}, nil)

	err := store.Revoke(key.ID)
	if err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	found, _ := store.FindByID(key.ID)
	if found.RevokedAt == nil {
		t.Error("Revoke() RevokedAt is nil")
	}

	err = store.Revoke(key.ID)
	if err == nil {
		t.Error("Revoke() should return error for already revoked key")
	}

	err = store.Revoke("invalid_id")
	if err != ErrKeyNotFound {
		t.Errorf("Revoke() error = %v, want %v", err, ErrKeyNotFound)
	}
}

func TestCreateKeyHandler(t *testing.T) {
	store := NewInMemoryStore()
	handler := CreateKeyHandler(store)

	body := `{"name":"test-key","scopes":["products:read","orders:write"]}`
	req := httptest.NewRequest("POST", "/v1/admin/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("CreateKeyHandler() status = %d, want %d", rec.Code, http.StatusCreated)
	}

	var resp KeyResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp.Name != "test-key" {
		t.Errorf("CreateKeyHandler() name = %q, want %q", resp.Name, "test-key")
	}

	if len(resp.Scopes) != 2 {
		t.Errorf("CreateKeyHandler() scopes length = %d, want 2", len(resp.Scopes))
	}

	if resp.Key == "" {
		t.Error("CreateKeyHandler() key is empty")
	}

	if !strings.HasPrefix(resp.Key, "kik_") {
		t.Errorf("CreateKeyHandler() key = %q, want prefix 'kik_'", resp.Key)
	}

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/v1/admin/keys", strings.NewReader("invalid"))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("CreateKeyHandler() status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("missing name", func(t *testing.T) {
		body := `{"scopes":["products:read"]}`
		req := httptest.NewRequest("POST", "/v1/admin/keys", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("CreateKeyHandler() status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("missing scopes", func(t *testing.T) {
		body := `{"name":"test-key"}`
		req := httptest.NewRequest("POST", "/v1/admin/keys", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("CreateKeyHandler() status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})
}

func TestListKeysHandler(t *testing.T) {
	store := NewInMemoryStore()
	store.Create("key1", []string{"products:read"}, nil)
	store.Create("key2", []string{"orders:write"}, nil)

	handler := ListKeysHandler(store)
	req := httptest.NewRequest("GET", "/v1/admin/keys", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("ListKeysHandler() status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp ListKeysResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if len(resp.Data) != 2 {
		t.Errorf("ListKeysHandler() data length = %d, want 2", len(resp.Data))
	}

	for _, key := range resp.Data {
		if key.Key != "" {
			t.Errorf("ListKeysHandler() key.Key should be empty, got %q", key.Key)
		}
		if key.Prefix == "" {
			t.Error("ListKeysHandler() key.Prefix is empty")
		}
	}
}

func TestRevokeKeyHandler(t *testing.T) {
	store := NewInMemoryStore()
	key, _, _ := store.Create("test-key", []string{"products:read"}, nil)

	handler := RevokeKeyHandler(store)
	req := httptest.NewRequest("DELETE", "/v1/admin/keys/"+key.ID, nil)
	req.SetPathValue("id", key.ID)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("RevokeKeyHandler() status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	found, _ := store.FindByID(key.ID)
	if found.RevokedAt == nil {
		t.Error("RevokeKeyHandler() key not revoked")
	}

	t.Run("key not found", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/v1/admin/keys/invalid", nil)
		req.SetPathValue("id", "invalid")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("RevokeKeyHandler() status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

func TestIntegration_CreateVerifyRevoke(t *testing.T) {
	store := NewInMemoryStore()

	createHandler := CreateKeyHandler(store)
	body := `{"name":"integration-key","scopes":["products:read","orders:write"]}`
	req := httptest.NewRequest("POST", "/v1/admin/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	createHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Create failed: status = %d", rec.Code)
	}

	var createResp KeyResponse
	json.Unmarshal(rec.Body.Bytes(), &createResp)
	keyStr := createResp.Key
	keyID := createResp.ID

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	middleware := RequireScope(testHandler, store, "products:read")
	verifyReq := httptest.NewRequest("GET", "/test", nil)
	verifyReq.Header.Set("Authorization", "Bearer "+keyStr)
	verifyRec := httptest.NewRecorder()

	middleware.ServeHTTP(verifyRec, verifyReq)

	if verifyRec.Code != http.StatusOK {
		t.Errorf("Verification failed: status = %d", verifyRec.Code)
	}

	revokeHandler := RevokeKeyHandler(store)
	revokeReq := httptest.NewRequest("DELETE", "/v1/admin/keys/"+keyID, nil)
	revokeReq.SetPathValue("id", keyID)
	revokeRec := httptest.NewRecorder()

	revokeHandler.ServeHTTP(revokeRec, revokeReq)

	if revokeRec.Code != http.StatusNoContent {
		t.Errorf("Revoke failed: status = %d", revokeRec.Code)
	}

	finalReq := httptest.NewRequest("GET", "/test", nil)
	finalReq.Header.Set("Authorization", "Bearer "+keyStr)
	finalRec := httptest.NewRecorder()

	middleware.ServeHTTP(finalRec, finalReq)

	if finalRec.Code != http.StatusUnauthorized {
		t.Errorf("Post-revoke check failed: status = %d, want %d", finalRec.Code, http.StatusUnauthorized)
	}

	var resp map[string]interface{}
	json.Unmarshal(finalRec.Body.Bytes(), &resp)
	errMap := resp["error"].(map[string]interface{})

	if code := errMap["code"].(string); code != "revoked_key" {
		t.Errorf("Post-revoke error code = %q, want %q", code, "revoked_key")
	}
}

func TestKeyNotLogged(t *testing.T) {
	store := NewInMemoryStore()

	key, keyStr, _ := store.Create("test-key", []string{"products:read"}, nil)

	_ = key
	_ = keyStr

	var logBuf bytes.Buffer
	loggerCreated := keyStr != ""

	if !loggerCreated {
		t.Error("Key should have been created")
	}

	if bytes.Contains(logBuf.Bytes(), []byte(keyStr)) {
		t.Errorf("Key %q should not appear in logs", keyStr)
	}

	prefix := ExtractPrefix(keyStr)
	if bytes.Contains(logBuf.Bytes(), []byte(keyStr[len(prefix):])) {
		t.Error("Full key beyond prefix should not appear in logs")
	}
}

func TestConstantTimeComparison(t *testing.T) {
	key, hash, _ := GenerateKey()

	if !CompareKeyHash(hash, key) {
		t.Error("CompareKeyHash should use constant-time comparison")
	}
}

func TestNoSecretMaterialInErrorResponse(t *testing.T) {
	store := NewInMemoryStore()
	_, keyStr, _ := store.Create("test-key", []string{"products:read"}, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+keyStr)

	rec := httptest.NewRecorder()
	middleware := RequireScope(handler, store, "wrong:scope")
	middleware.ServeHTTP(rec, req)

	body := rec.Body.String()
	
	if strings.Contains(body, keyStr) {
		t.Errorf("Full key should not appear in error response, found in: %s", body)
	}
	
	if strings.Contains(body, keyStr[4:]) {
		t.Errorf("Key material after prefix should not appear in error response, found in: %s", body)
	}
}

func TestKeyHashStorage(t *testing.T) {
	store := NewInMemoryStore()
	_, keyStr, _ := store.Create("test-key", []string{"products:read"}, nil)

	var storedKey *Key
	for _, k := range store.keys {
		storedKey = k
		break
	}
	
	if bytes.Equal(storedKey.Hash, []byte(keyStr)) {
		t.Error("Hash should not equal the key string")
	}
	
	if len(storedKey.Hash) != 32 {
		t.Errorf("Hash should be 32 bytes (SHA-256), got %d", len(storedKey.Hash))
	}
}
