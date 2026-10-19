package rpcserver

import (
	"context"
	"crypto/subtle"
)

// KeyAuthResolver is an AuthResolver for a single pre-shared secret key —
// the "static API key" pattern: a trusted non-browser caller (an admin SPA
// behind a webserver-level auth gate, an MCP server, tooling) authenticates
// with one configured key instead of a per-user token. The identity a match
// yields is application-specific, so the caller supplies the full AuthInfo
// (UserID, PlainTransport, …).
//
// It can serve as the sole resolver, or as the first branch of a composite
// resolver via Match (falling through to e.g. JWT validation on mismatch).
type KeyAuthResolver struct {
	// Key is the expected secret. An empty Key never matches any token —
	// an unconfigured key must not turn into a match-everything hole.
	Key string
	// AuthInfo is returned (as a copy) for a matching token.
	AuthInfo AuthInfo
}

// Match reports whether token equals the configured key, returning a copy of
// the configured AuthInfo, marked as a key principal, on success. Composite
// resolvers use this to try the key branch first and run their own token
// handling on mismatch. The comparison takes the same time however much of
// the key a token gets right.
func (r *KeyAuthResolver) Match(token string) (*AuthInfo, bool) {
	if r.Key == "" || subtle.ConstantTimeCompare([]byte(token), []byte(r.Key)) != 1 {
		return nil, false
	}
	info := r.AuthInfo
	info.Key = true
	return &info, true
}

// ResolveToken implements AuthResolver. A mismatch yields an error, which
// the HTTP/WS transports translate into an anonymous principal — same
// treatment as any other unresolvable token.
func (r *KeyAuthResolver) ResolveToken(_ context.Context, token string) (*AuthInfo, error) {
	if info, ok := r.Match(token); ok {
		return info, nil
	}
	return nil, NewAPIError(CodeUnauthenticated)
}
