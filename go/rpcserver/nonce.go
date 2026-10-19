package rpcserver

import (
	"context"
	"sync"
	"time"
)

// NonceStore handles nonce deduplication.
type NonceStore interface {
	// CheckAndSet returns true if the nonce is new (first seen), false if duplicate.
	CheckAndSet(ctx context.Context, nonce string) (bool, error)
}

// NonceTTL is how long a nonce stays seen: a call retried within it is a
// duplicate.
const NonceTTL = 5 * time.Minute

// MemoryNonceStore is an in-process NonceStore — enough for a server that
// runs as one process. Its nonces do not survive a restart.
type MemoryNonceStore struct {
	mu        sync.Mutex
	seen      map[string]time.Time
	lastSweep time.Time
}

func NewMemoryNonceStore() *MemoryNonceStore {
	return &MemoryNonceStore{seen: map[string]time.Time{}, lastSweep: time.Now()}
}

// CheckAndSet returns true if the nonce is new (not seen within NonceTTL).
// Expired nonces are swept at most once per NonceTTL.
func (s *MemoryNonceStore) CheckAndSet(_ context.Context, nonce string) (bool, error) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if now.Sub(s.lastSweep) > NonceTTL {
		for n, t := range s.seen {
			if now.Sub(t) > NonceTTL {
				delete(s.seen, n)
			}
		}
		s.lastSweep = now
	}
	if t, ok := s.seen[nonce]; ok && now.Sub(t) <= NonceTTL {
		return false, nil
	}
	s.seen[nonce] = now
	return true, nil
}
