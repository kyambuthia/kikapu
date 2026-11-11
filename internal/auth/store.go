package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

type InMemoryStore struct {
	mu   sync.RWMutex
	keys map[string]*Key
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		keys: make(map[string]*Key),
	}
}

func (s *InMemoryStore) FindByHash(hash []byte) (*Key, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, key := range s.keys {
		if subtle.ConstantTimeCompare(key.Hash, hash) == 1 {
			return key, nil
		}
	}
	return nil, ErrKeyNotFound
}

func (s *InMemoryStore) FindByID(id string) (*Key, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key, exists := s.keys[id]
	if !exists {
		return nil, ErrKeyNotFound
	}
	return key, nil
}

func (s *InMemoryStore) Create(name string, scopes []string, expiresAt *int64) (*Key, string, error) {
	keyStr, hash, err := GenerateKey()
	if err != nil {
		return nil, "", err
	}

	id := generateID()
	prefix := ExtractPrefix(keyStr)

	key := &Key{
		ID:     id,
		Name:   name,
		Prefix: prefix,
		Hash:   hash,
		Scopes: scopes,
	}

	if expiresAt != nil {
		t := time.Unix(*expiresAt, 0).UTC()
		key.ExpiresAt = &t
	}

	s.mu.Lock()
	s.keys[id] = key
	s.mu.Unlock()

	return key, keyStr, nil
}

func (s *InMemoryStore) List() ([]Key, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]Key, 0, len(s.keys))
	for _, key := range s.keys {
		list = append(list, Key{
			ID:        key.ID,
			Name:      key.Name,
			Prefix:    key.Prefix,
			Scopes:    key.Scopes,
			ExpiresAt: key.ExpiresAt,
			RevokedAt: key.RevokedAt,
		})
	}
	return list, nil
}

func (s *InMemoryStore) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, exists := s.keys[id]
	if !exists {
		return ErrKeyNotFound
	}

	if key.RevokedAt != nil {
		return errors.New("key already revoked")
	}

	now := time.Now().UTC()
	key.RevokedAt = &now
	return nil
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return "key_" + hex.EncodeToString(b)
}
