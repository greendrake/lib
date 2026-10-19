package rpcserver

import (
	"context"
	"errors"
	"testing"
)

func TestKeyAuthResolver_MatchReturnsConfiguredAuthInfo(t *testing.T) {
	r := &KeyAuthResolver{
		Key:      "secret-key",
		AuthInfo: AuthInfo{UserID: "op", PlainTransport: true},
	}

	info, err := r.ResolveToken(context.Background(), "secret-key")
	if err != nil {
		t.Fatalf("matching key should resolve: %v", err)
	}
	if info.UserID != "op" || !info.PlainTransport {
		t.Fatalf("resolved AuthInfo does not carry the configured identity: %+v", info)
	}
	if !info.Key {
		t.Fatal("a key match must mark the principal as a key principal")
	}
	// The returned AuthInfo is a copy — mutating it must not leak into the
	// resolver's configured identity shared across requests.
	info.PlainTransport = false
	if !r.AuthInfo.PlainTransport || r.AuthInfo.Key {
		t.Fatal("mutating the resolved AuthInfo leaked into the resolver's configuration")
	}
}

func TestKeyAuthResolver_MismatchIsUnauthenticated(t *testing.T) {
	r := &KeyAuthResolver{Key: "secret-key", AuthInfo: AuthInfo{UserID: "op"}}

	for _, token := range []string{"wrong", "secret-ke", "secret-key2"} {
		if info, ok := r.Match(token); ok || info != nil {
			t.Fatalf("%q must not match", token)
		}
		info, err := r.ResolveToken(context.Background(), token)
		var apiErr *APIError
		if info != nil || !errors.As(err, &apiErr) || apiErr.Code != CodeUnauthenticated {
			t.Fatalf("%q must resolve to UNAUTHENTICATED, not an identity: %v, %v", token, info, err)
		}
	}
}

func TestKeyAuthResolver_EmptyKeyNeverMatches(t *testing.T) {
	r := &KeyAuthResolver{AuthInfo: AuthInfo{UserID: "op"}}

	for _, token := range []string{"", "anything"} {
		if _, ok := r.Match(token); ok {
			t.Fatalf("empty configured key must never match (token %q)", token)
		}
		if _, err := r.ResolveToken(context.Background(), token); err == nil {
			t.Fatalf("empty configured key must never resolve (token %q)", token)
		}
	}
}
