package idempotency

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestHashRequest(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       []byte
		wantSameAs struct {
			method string
			path   string
			body   []byte
		}
		wantDifferent bool
	}{
		{
			name:   "same inputs produce same hash",
			method: "POST",
			path:   "/v1/checkout",
			body:   []byte(`{"cart_id":"123"}`),
			wantSameAs: struct {
				method string
				path   string
				body   []byte
			}{
				method: "POST",
				path:   "/v1/checkout",
				body:   []byte(`{"cart_id":"123"}`),
			},
		},
		{
			name:          "different method produces different hash",
			method:        "POST",
			path:          "/v1/checkout",
			body:          []byte(`{"cart_id":"123"}`),
			wantDifferent: true,
			wantSameAs: struct {
				method string
				path   string
				body   []byte
			}{
				method: "GET",
				path:   "/v1/checkout",
				body:   []byte(`{"cart_id":"123"}`),
			},
		},
		{
			name:          "different path produces different hash",
			method:        "POST",
			path:          "/v1/checkout",
			body:          []byte(`{"cart_id":"123"}`),
			wantDifferent: true,
			wantSameAs: struct {
				method string
				path   string
				body   []byte
			}{
				method: "POST",
				path:   "/v1/orders",
				body:   []byte(`{"cart_id":"123"}`),
			},
		},
		{
			name:          "different body produces different hash",
			method:        "POST",
			path:          "/v1/checkout",
			body:          []byte(`{"cart_id":"123"}`),
			wantDifferent: true,
			wantSameAs: struct {
				method string
				path   string
				body   []byte
			}{
				method: "POST",
				path:   "/v1/checkout",
				body:   []byte(`{"cart_id":"456"}`),
			},
		},
		{
			name:   "empty body",
			method: "POST",
			path:   "/v1/admin/products",
			body:   []byte{},
			wantSameAs: struct {
				method string
				path   string
				body   []byte
			}{
				method: "POST",
				path:   "/v1/admin/products",
				body:   []byte{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := HashRequest(tt.method, tt.path, tt.body)
			hash2 := HashRequest(tt.wantSameAs.method, tt.wantSameAs.path, tt.wantSameAs.body)

			if tt.wantDifferent {
				if hash1 == hash2 {
					t.Errorf("HashRequest() = %v, want different hash", hash1)
				}
			} else {
				if hash1 != hash2 {
					t.Errorf("HashRequest() = %v, want %v", hash1, hash2)
				}
			}

			if len(hash1) != 64 {
				t.Errorf("HashRequest() returned hash of length %d, want 64", len(hash1))
			}
		})
	}
}

func TestInMemoryStore_Begin(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	t.Run("first request creates pending record", func(t *testing.T) {
		rec, replay, err := store.Begin(ctx, "key1", "hash1")
		if err != nil {
			t.Fatalf("Begin() error = %v", err)
		}
		if replay {
			t.Error("Begin() replay = true, want false")
		}
		if rec.Key != "" {
			t.Errorf("Begin() rec.Key = %q, want empty", rec.Key)
		}
	})

	t.Run("same key and hash returns replay after finish", func(t *testing.T) {
		store.Begin(ctx, "key2", "hash2")
		store.Finish(ctx, "key2", http.StatusOK, []byte(`{"test":"response"}`))

		rec, replay, err := store.Begin(ctx, "key2", "hash2")
		if err != nil {
			t.Fatalf("Begin() error = %v", err)
		}
		if !replay {
			t.Error("Begin() replay = false, want true")
		}
		if rec.Status != http.StatusOK {
			t.Errorf("Begin() rec.Status = %d, want %d", rec.Status, http.StatusOK)
		}
	})

	t.Run("same key different hash returns conflict", func(t *testing.T) {
		store.Begin(ctx, "key3", "hash3")
		store.Finish(ctx, "key3", http.StatusOK, []byte{})

		_, _, err := store.Begin(ctx, "key3", "hash3-different")
		if err != ErrConflict {
			t.Errorf("Begin() error = %v, want %v", err, ErrConflict)
		}
	})

	t.Run("expired key is cleaned up and request proceeds", func(t *testing.T) {
		store.mu.Lock()
		store.records["key4"] = &entry{
			RequestHash: "hash4",
			ExpiresAt:   time.Now().Add(-1 * time.Hour),
		}
		store.mu.Unlock()

		rec, replay, err := store.Begin(ctx, "key4", "hash4")
		if err != nil {
			t.Errorf("Begin() error = %v, want nil", err)
		}
		if replay {
			t.Error("Begin() should not replay for expired key")
		}
		if rec.Key != "" {
			t.Error("Begin() should return empty record for new request")
		}

		store.mu.Lock()
		ent, exists := store.records["key4"]
		store.mu.Unlock()
		if !exists {
			t.Error("Begin() should have created new entry after deleting expired one")
		}
		if ent != nil && !ent.InFlight {
			t.Error("New entry should be in-flight")
		}
	})

	t.Run("pending key returns in-flight error", func(t *testing.T) {
		store.mu.Lock()
		store.records["key5"] = &entry{
			RequestHash: "hash5",
			InFlight:    true,
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		}
		store.mu.Unlock()

		_, _, err := store.Begin(ctx, "key5", "hash5")
		if err != ErrInFlight {
			t.Errorf("Begin() error = %v, want %v", err, ErrInFlight)
		}
	})
}

func TestInMemoryStore_Finish(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	t.Run("finish creates done record", func(t *testing.T) {
		store.Begin(ctx, "key1", "hash1")

		body := []byte(`{"status":"ok"}`)
		err := store.Finish(ctx, "key1", http.StatusOK, body)
		if err != nil {
			t.Fatalf("Finish() error = %v", err)
		}

		store.mu.Lock()
		ent := store.records["key1"]
		store.mu.Unlock()

		if ent.Status != http.StatusOK {
			t.Errorf("Finish() ent.Status = %d, want %d", ent.Status, http.StatusOK)
		}
		if string(ent.Response) != string(body) {
			t.Errorf("Finish() ent.Response = %s, want %s", ent.Response, body)
		}
		if ent.InFlight {
			t.Error("Finish() did not mark key as done")
		}
	})
}

func TestMiddleware_MissingKey(t *testing.T) {
	store := NewInMemoryStore()
	handler := Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))

	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("Idempotency-Key", "")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Middleware() status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"].(map[string]interface{})["code"] != "missing_idempotency_key" {
		t.Errorf("Middleware() error code = %v, want missing_idempotency_key", resp["error"])
	}
}

func TestMiddleware_Replay(t *testing.T) {
	store := NewInMemoryStore()
	handler := Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "id": "test-123"})
	}))

	body := []byte(`{"test":"data"}`)
	req1 := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req1.Header.Set("Idempotency-Key", "test-key")
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()

	handler.ServeHTTP(w1, req1)

	req2 := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req2.Header.Set("Idempotency-Key", "test-key")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()

	handler.ServeHTTP(w2, req2)

	if w1.Code != w2.Code {
		t.Errorf("Replay status codes differ: %d vs %d", w1.Code, w2.Code)
	}

	if !bytes.Equal(w1.Body.Bytes(), w2.Body.Bytes()) {
		t.Errorf("Replay bodies differ:\n%s\nvs\n%s", w1.Body.String(), w2.Body.String())
	}

	if w2.Header().Get("X-Idempotency-Replayed") != "true" {
		t.Error("Replay request missing X-Idempotency-Replayed header")
	}
}

func TestMiddleware_Conflict(t *testing.T) {
	store := NewInMemoryStore()

	handler1 := Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":1}`))
	}))

	req1 := httptest.NewRequest("POST", "/test", bytes.NewReader([]byte(`{"a":1}`)))
	req1.Header.Set("Idempotency-Key", "conflict-key")
	w1 := httptest.NewRecorder()
	handler1.ServeHTTP(w1, req1)

	handler2 := Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":2}`))
	}))

	req2 := httptest.NewRequest("POST", "/test", bytes.NewReader([]byte(`{"a":2}`)))
	req2.Header.Set("Idempotency-Key", "conflict-key")
	w2 := httptest.NewRecorder()
	handler2.ServeHTTP(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Errorf("Conflict status = %d, want %d", w2.Code, http.StatusConflict)
	}

	var resp map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&resp)
	if resp["error"].(map[string]interface{})["code"] != "idempotency_conflict" {
		t.Errorf("Conflict error code = %v, want idempotency_conflict", resp["error"])
	}
}

func TestMiddleware_Concurrent(t *testing.T) {
	store := NewInMemoryStore()

	var handlerCalled int
	var mu sync.Mutex

	handler := Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		handlerCalled++
		mu.Unlock()

		time.Sleep(10 * time.Millisecond)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]int{"call": handlerCalled})
	}))

	var wg sync.WaitGroup
	results := make([]httptest.ResponseRecorder, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			body := []byte(`{"concurrent":"test"}`)
			req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
			req.Header.Set("Idempotency-Key", "concurrent-key")
			req.Header.Set("Content-Type", "application/json")

			results[idx] = *httptest.NewRecorder()
			handler.ServeHTTP(&results[idx], req)
		}(i)
	}

	wg.Wait()

	mu.Lock()
	called := handlerCalled
	mu.Unlock()

	if called != 1 {
		t.Errorf("Handler called %d times, want 1", called)
	}

	successCount := 0
	for i := range results {
		if results[i].Code == http.StatusOK {
			successCount++
		} else if results[i].Code == http.StatusConflict {
		}
	}

	if called != 1 {
		t.Errorf("Expected handler to be called exactly once, got %d", called)
	}

	totalSuccessOrConflict := 0
	for i := range results {
		if results[i].Code == http.StatusOK || results[i].Code == http.StatusConflict {
			totalSuccessOrConflict++
		}
	}
	if totalSuccessOrConflict != 10 {
		t.Errorf("Expected all 10 requests to succeed or return conflict, got %d", totalSuccessOrConflict)
	}
}

func TestMiddleware_ExpiredRecord(t *testing.T) {
	store := NewInMemoryStore()

	store.mu.Lock()
	store.records["expired-key"] = &entry{
		RequestHash: HashRequest("POST", "/test", []byte(`{"test":"data"}`)),
		Status:      http.StatusOK,
		Response:    json.RawMessage(`{"old":"response"}`),
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
		InFlight:    false,
	}
	store.mu.Unlock()

	handler := Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"new": "response"})
	}))

	body := []byte(`{"test":"data"}`)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", "expired-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expired request status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["new"] != "response" {
		t.Errorf("Expired request response = %v, want new response", resp)
	}
}

func TestMiddleware_InFlight(t *testing.T) {
	store := NewInMemoryStore()

	store.mu.Lock()
	store.records["inflight-key"] = &entry{
		RequestHash: HashRequest("POST", "/test", []byte(`{"test":"data"}`)),
		InFlight:    true,
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}
	store.mu.Unlock()

	handler := Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))

	body := []byte(`{"test":"data"}`)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", "inflight-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("In-flight request status = %d, want %d", w.Code, http.StatusConflict)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"].(map[string]interface{})["code"] != "request_in_flight" {
		t.Errorf("In-flight error code = %v, want request_in_flight", resp["error"])
	}
}
