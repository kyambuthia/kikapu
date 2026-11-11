package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

var (
	ErrConflict = errors.New("idempotency: key reused with a different request")
	ErrInFlight = errors.New("idempotency: request with this key is in flight")
	ErrNotFound = errors.New("idempotency: key not found")
	ErrExpired  = errors.New("idempotency: key expired")
)

type Record struct {
	Key         string
	RequestHash string
	Status      int
	Response    json.RawMessage
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

type Status string

const (
	StatusPending Status = "pending"
	StatusDone    Status = "done"
)

func HashRequest(method, path string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte(path))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

type Store interface {
	Begin(ctx context.Context, key, requestHash string) (Record, bool, error)
	Finish(ctx context.Context, key string, status int, body []byte) error
}

type entry struct {
	RequestHash string
	Status      int
	Response    json.RawMessage
	CreatedAt   time.Time
	ExpiresAt   time.Time
	InFlight    bool
}

type InMemoryStore struct {
	mu      sync.Mutex
	records map[string]*entry
	now     func() time.Time
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		records: make(map[string]*entry),
		now:     time.Now,
	}
}

func (s *InMemoryStore) Begin(ctx context.Context, key, requestHash string) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ent, exists := s.records[key]; exists {
		if ent.ExpiresAt.Before(s.now()) {
			delete(s.records, key)
		} else {
			if ent.RequestHash != requestHash {
				return Record{}, false, ErrConflict
			}
			if ent.InFlight {
				return Record{}, false, ErrInFlight
			}
			return Record{
				Key:         key,
				RequestHash: ent.RequestHash,
				Status:      ent.Status,
				Response:    ent.Response,
				CreatedAt:   ent.CreatedAt,
				ExpiresAt:   ent.ExpiresAt,
			}, true, nil
		}
	}

	s.records[key] = &entry{
		RequestHash: requestHash,
		InFlight:    true,
		CreatedAt:   s.now(),
		ExpiresAt:   s.now().Add(24 * time.Hour),
	}

	return Record{}, false, nil
}

func (s *InMemoryStore) Finish(ctx context.Context, key string, status int, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ent, exists := s.records[key]; exists {
		ent.Status = status
		ent.Response = json.RawMessage(body)
		ent.InFlight = false
		ent.ExpiresAt = s.now().Add(24 * time.Hour)
	} else {
		s.records[key] = &entry{
			Status:    status,
			Response:  json.RawMessage(body),
			InFlight:  false,
			CreatedAt: s.now(),
			ExpiresAt: s.now().Add(24 * time.Hour),
		}
	}

	return nil
}
