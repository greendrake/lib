package rpcserver

import (
	"context"
	"fmt"
)

// AuthInfo is the principal of a request or connection, as the host's
// AuthResolver resolved it.
type AuthInfo struct {
	// UserID is the principal's id, of the host's own id type.
	UserID any
	// Key marks a principal authenticated by a pre-shared key
	// (KeyAuthResolver) rather than by a token the host issued.
	Key bool
	// PlainTransport, when set by the auth resolver, marks a trusted,
	// non-browser principal that runs neither the client's encoder nor its
	// decoder — e.g. a pre-shared key (a dashboard, tooling). It opts the
	// principal out of the transport's Encoding in BOTH directions: the
	// transport accepts a plain request body even under
	// Encoding.RequireEncodedRequest, and skips response encoding even with
	// Encoding.EncodeResponse on. It is a per-principal transport hint, not an
	// authorization grant — encoding is the only thing it affects.
	PlainTransport bool
	// Claims is whatever else the host's resolver establishes about the
	// principal, of the host's own type. This package reads none of it.
	Claims any
}

// AuthGate is the dispatcher's one authorisation hook: it admits or refuses
// every principal that reaches a method requiring one, key principals
// included, before the method's handler runs. The policy is wholly the
// host's: def.Access says what the method asks, in the host's own terms. The
// ctx it returns is the handler's, so a gate can attach what it loaded (the
// principal's account) for the handler to read instead of loading it again.
// A refusal is answered like a handler's error: an *APIError by its code,
// anything else as CodeInternal.
type AuthGate func(ctx context.Context, auth *AuthInfo, def *MethodDef) (context.Context, error)

// AuthResolver resolves a bearer token to its principal. An error, or a
// principal without a UserID, leaves the caller anonymous.
type AuthResolver interface {
	ResolveToken(ctx context.Context, token string) (*AuthInfo, error)
}

type authContextKey struct{}

// WithAuth returns a context carrying the given AuthInfo.
func WithAuth(ctx context.Context, auth *AuthInfo) context.Context {
	return context.WithValue(ctx, authContextKey{}, auth)
}

// GetAuth retrieves the AuthInfo from a context. Returns nil if absent.
func GetAuth(ctx context.Context) *AuthInfo {
	val, _ := ctx.Value(authContextKey{}).(*AuthInfo)
	return val
}

// UserID returns the request principal's id as the host's id type: an
// AUTH_REQUIRED APIError when the request carries no principal, and an error
// when the principal's id is not a T — the host's resolver and handler
// disagree on the id type.
func UserID[T any](hc *HandlerContext) (T, error) {
	var zero T
	auth := GetAuth(hc.Ctx)
	if auth == nil || auth.UserID == nil {
		return zero, NewAPIError(CodeAuthRequired)
	}
	id, ok := auth.UserID.(T)
	if !ok {
		return zero, fmt.Errorf("principal id is %T, not %T", auth.UserID, zero)
	}
	return id, nil
}

// localeContextKey carries the request's resolved locale down the call chain.
// In the context rather than an argument threaded through every layer, for the
// same reason AuthInfo is: it is ambient request metadata that only the few
// call sites which render a label actually read.
type localeContextKey struct{}

// WithLocale returns a context carrying the request's resolved locale.
func WithLocale(ctx context.Context, locale string) context.Context {
	return context.WithValue(ctx, localeContextKey{}, locale)
}

// LocaleFrom reads the request's resolved locale, or "" where none was set
// (an app that installs no resolver, or a background task).
func LocaleFrom(ctx context.Context) string {
	locale, _ := ctx.Value(localeContextKey{}).(string)
	return locale
}
