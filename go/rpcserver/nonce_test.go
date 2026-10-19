package rpcserver

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

// A nonce is a duplicate within NonceTTL and new again after it; the sweep
// forgets the expired ones.
func TestMemoryNonceStore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NewMemoryNonceStore()
		check := func(nonce string, want bool) {
			t.Helper()
			if got, err := s.CheckAndSet(context.Background(), nonce); err != nil || got != want {
				t.Fatalf("CheckAndSet(%q) = %v, %v; want %v", nonce, got, err, want)
			}
		}
		check("a", true)
		check("a", false)
		time.Sleep(NonceTTL)
		check("a", false)
		check("b", true)
		time.Sleep(time.Second)
		check("a", true)
		time.Sleep(NonceTTL + time.Second)
		check("c", true)
		if _, kept := s.seen["b"]; kept || len(s.seen) != 1 {
			t.Fatalf("after the sweep: %v", s.seen)
		}
	})
}
