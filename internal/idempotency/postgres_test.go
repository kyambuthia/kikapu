package idempotency

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresStore_ReplayReturnsSameResponse(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db, err := sql.Open("postgres", testDBURL())
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	store := NewPostgresStore(db)
	ctx := context.Background()

	key := "test-replay-key"
	requestHash := "POST:/v1/checkout:{\"cart_id\":\"123\"}"

	_, _, err = store.Begin(ctx, key, requestHash)
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}

	responseBody := json.RawMessage(`{"order_id":"ord_123","status":"paid"}`)
	err = store.Finish(ctx, key, http.StatusOK, responseBody)
	if err != nil {
		t.Fatalf("Finish() error: %v", err)
	}

	rec2, replay2, err := store.Begin(ctx, key, requestHash)
	if err != nil {
		t.Fatalf("Second Begin() error: %v", err)
	}
	if !replay2 {
		t.Error("Second Begin() should return replay=true")
	}
	if rec2.Status != http.StatusOK {
		t.Errorf("Replay status = %d, want %d", rec2.Status, http.StatusOK)
	}
	if string(rec2.Response) != string(responseBody) {
		t.Errorf("Replay response = %s, want %s", rec2.Response, responseBody)
	}
}

func TestPostgresStore_SameKeyDifferentHashReturnsConflict(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db, err := sql.Open("postgres", testDBURL())
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	store := NewPostgresStore(db)
	ctx := context.Background()

	key := "test-conflict-key"
	requestHash1 := "POST:/v1/checkout:{\"cart_id\":\"123\"}"
	requestHash2 := "POST:/v1/checkout:{\"cart_id\":\"456\"}"

	_, _, err = store.Begin(ctx, key, requestHash1)
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}

	err = store.Finish(ctx, key, http.StatusOK, json.RawMessage(`{"order_id":"ord_123"}`))
	if err != nil {
		t.Fatalf("Finish() error: %v", err)
	}

	_, _, err = store.Begin(ctx, key, requestHash2)
	if err != ErrConflict {
		t.Errorf("Begin() with different hash error = %v, want %v", err, ErrConflict)
	}
}

func TestPostgresStore_InFlightKeyReturnsError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db, err := sql.Open("postgres", testDBURL())
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	store := NewPostgresStore(db)
	ctx := context.Background()

	key := "test-inflight-key"
	requestHash := "POST:/v1/checkout:{\"cart_id\":\"123\"}"

	_, _, err = store.Begin(ctx, key, requestHash)
	if err != nil {
		t.Fatalf("First Begin() error: %v", err)
	}

	_, _, err = store.Begin(ctx, key, requestHash)
	if err != ErrInFlight {
		t.Errorf("Second Begin() error = %v, want %v", err, ErrInFlight)
	}
}

func TestPostgresStore_ExpiredRecordNotReplayed(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db, err := sql.Open("postgres", testDBURL())
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	store := NewPostgresStore(db)
	store.ttl = 100 * time.Millisecond
	ctx := context.Background()

	key := "test-expired-key"
	requestHash := "POST:/v1/checkout:{\"cart_id\":\"123\"}"

	_, _, err = store.Begin(ctx, key, requestHash)
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}

	err = store.Finish(ctx, key, http.StatusOK, json.RawMessage(`{"order_id":"ord_123"}`))
	if err != nil {
		t.Fatalf("Finish() error: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	_, replay, err := store.Begin(ctx, key, requestHash)
	if err != nil {
		t.Fatalf("Begin() after expiry error: %v", err)
	}
	if replay {
		t.Error("Expired record should not be replayed")
	}
}

func TestPostgresStore_ConcurrentSameKeyExactlyOneExecutes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db, err := sql.Open("postgres", testDBURL())
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	store := NewPostgresStore(db)
	ctx := context.Background()

	key := "test-concurrent-key"
	requestHash := "POST:/v1/checkout:{\"cart_id\":\"123\"}"

	var successful int32
	var wg sync.WaitGroup
	errChan := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, replay, err := store.Begin(ctx, key, requestHash)
			if err == ErrInFlight {
				return
			}
			if err != nil {
				errChan <- err
				return
			}
			if replay {
				return
			}
			atomic.AddInt32(&successful, 1)
			time.Sleep(50 * time.Millisecond)
			store.Finish(ctx, key, http.StatusOK, json.RawMessage(`{"order_id":"ord_123"}`))
		}()
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("Begin() error: %v", err)
	}

	if successful != 1 {
		t.Errorf("Exactly one goroutine should succeed, got %d", successful)
	}
}

func TestMiddleware_MissingHeaderReturns400(t *testing.T) {
	store := &mockStore{}
	middleware := Middleware(store)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/v1/checkout", nil)
	rw := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rw.Code, http.StatusBadRequest)
	}
}

func TestMiddleware_ReplayReturnsStoredResponse(t *testing.T) {
	key := "test-key"
	_ = computeRequestHash("POST", "/v1/checkout", nil)
	body := json.RawMessage(`{"order_id":"ord_123"}`)

	store := &mockStore{
		beginFunc: func(ctx context.Context, k, h string) (Record, bool, error) {
			return Record{
				Key:         k,
				RequestHash: h,
				Status:      http.StatusOK,
				Response:    body,
			}, true, nil
		},
	}

	middleware := Middleware(store)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called on replay")
	})

	req := httptest.NewRequest("POST", "/v1/checkout", nil)
	req.Header.Set("Idempotency-Key", key)
	rw := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rw.Code, http.StatusOK)
	}
}

func TestMiddleware_ConflictReturns409(t *testing.T) {
	store := &mockStore{
		beginFunc: func(ctx context.Context, k, h string) (Record, bool, error) {
			return Record{}, false, ErrConflict
		},
	}

	middleware := Middleware(store)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest("POST", "/v1/checkout", nil)
	req.Header.Set("Idempotency-Key", "test-key")
	rw := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Errorf("Status = %d, want %d", rw.Code, http.StatusConflict)
	}
}

func TestMiddleware_InFlightReturns409WithRetryAfter(t *testing.T) {
	store := &mockStore{
		beginFunc: func(ctx context.Context, k, h string) (Record, bool, error) {
			return Record{}, false, ErrInFlight
		},
	}

	middleware := Middleware(store)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest("POST", "/v1/checkout", nil)
	req.Header.Set("Idempotency-Key", "test-key")
	rw := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Errorf("Status = %d, want %d", rw.Code, http.StatusConflict)
	}
	if rw.Header().Get("Retry-After") != "30" {
		t.Errorf("Retry-After header = %s, want 30", rw.Header().Get("Retry-After"))
	}
}

type mockStore struct {
	beginFunc  func(ctx context.Context, key, requestHash string) (Record, bool, error)
	finishFunc func(ctx context.Context, key string, status int, body []byte) error
}

func (m *mockStore) Begin(ctx context.Context, key, requestHash string) (Record, bool, error) {
	if m.beginFunc != nil {
		return m.beginFunc(ctx, key, requestHash)
	}
	return Record{}, false, nil
}

func (m *mockStore) Finish(ctx context.Context, key string, status int, body []byte) error {
	if m.finishFunc != nil {
		return m.finishFunc(ctx, key, status, body)
	}
	return nil
}

func (m *mockStore) Cleanup(ctx context.Context) error {
	return nil
}

func TestPostgresStore_ExpiredPendingRecordAllowsNewRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db, err := sql.Open("postgres", testDBURL())
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	store := NewPostgresStore(db)
	store.requestTimeout = 100 * time.Millisecond
	ctx := context.Background()

	key := "test-expired-pending-key"
	requestHash := "POST:/v1/checkout:{\"cart_id\":\"123\"}"

	_, _, err = store.Begin(ctx, key, requestHash)
	if err != nil {
		t.Fatalf("First Begin() error: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	_, replay, err := store.Begin(ctx, key, requestHash)
	if err != nil {
		t.Fatalf("Begin() after timeout error: %v", err)
	}
	if replay {
		t.Error("Expired pending record should not be replayed")
	}
}

func testDBURL() string {
	return "postgres://postgres:postgres@localhost:5432/kikapu_test?sslmode=disable"
}
