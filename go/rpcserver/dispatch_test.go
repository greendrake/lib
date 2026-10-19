package rpcserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// --- Test helpers ---

type mockNonceStore struct {
	seen map[string]bool
	err  error
}

func newMockNonceStore() *mockNonceStore {
	return &mockNonceStore{seen: make(map[string]bool)}
}

func (m *mockNonceStore) CheckAndSet(_ context.Context, nonce string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	if m.seen[nonce] {
		return false, nil
	}
	m.seen[nonce] = true
	return true, nil
}

type mockAuthResolver struct {
	users map[string]*AuthInfo
}

func newMockAuthResolver() *mockAuthResolver {
	return &mockAuthResolver{users: make(map[string]*AuthInfo)}
}

func (m *mockAuthResolver) ResolveToken(_ context.Context, token string) (*AuthInfo, error) {
	if ai, ok := m.users[token]; ok {
		return ai, nil
	}
	return nil, NewAPIError(CodeUnauthenticated)
}

func testMethods() map[string]MethodDef {
	return map[string]MethodDef{
		"app.ping": {
			Handler: func(hc *HandlerContext, args json.RawMessage) (any, error) {
				return "pong", nil
			},
			Auth: AuthNone,
		},
		// Blocks until its context is cancelled. Used to test abort and concurrent
		// dispatch: callers can cancel the context (via abort) and verify that the
		// handler exits promptly with the cancellation error.
		"slow.op": {
			Handler: func(hc *HandlerContext, args json.RawMessage) (any, error) {
				<-hc.Ctx.Done()
				return nil, hc.Ctx.Err()
			},
			Auth: AuthNone,
		},
		"user.whoami": {
			Handler: func(hc *HandlerContext, args json.RawMessage) (any, error) {
				auth := GetAuth(hc.Ctx)
				return auth.UserID, nil
			},
			Auth: AuthRequired,
		},
		"plain.allowed": {
			Handler: func(hc *HandlerContext, args json.RawMessage) (any, error) {
				return "plain_ok", nil
			},
			Auth:       AuthNone,
			AllowPlain: true,
		},
		"echo.args": {
			Handler: func(hc *HandlerContext, args json.RawMessage) (any, error) {
				var msg []string
				if err := json.Unmarshal(args, &msg); err != nil || len(msg) != 1 {
					return nil, NewAPIError(CodeInvalidArgument)
				}
				return msg[0], nil
			},
			Auth: AuthNone,
		},
		"fail.api": {
			Handler: func(hc *HandlerContext, args json.RawMessage) (any, error) {
				return nil, NewAPIError("CUSTOM_ERROR")
			},
			Auth: AuthNone,
		},
		"fail.internal": {
			Handler: func(hc *HandlerContext, args json.RawMessage) (any, error) {
				return nil, context.DeadlineExceeded
			},
			Auth: AuthNone,
		},
		// Answers what the request's context carries for a handler.
		"context.read": {
			Handler: func(hc *HandlerContext, args json.RawMessage) (any, error) {
				return map[string]any{"locale": LocaleFrom(hc.Ctx), "validator": ValidatorFrom(hc.Ctx) != nil}, nil
			},
			Auth: AuthNone,
		},
		"result.unmarshalable": {
			Handler: func(hc *HandlerContext, args json.RawMessage) (any, error) {
				return func() {}, nil
			},
			Auth: AuthNone,
		},
	}
}

func hc() *HandlerContext {
	return &HandlerContext{Ctx: context.Background(), ClientIP: "127.0.0.1"}
}

// logged is a logger writing to the returned buffer.
func logged() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

// --- Dispatch tests ---

func TestDispatch_PublicMethod(t *testing.T) {
	d := NewDispatcher(testMethods())
	resp := d.Dispatch(hc(), nil, &Request{Method: "app.ping"})
	if !resp.Success || resp.Data != "pong" {
		t.Fatalf("expected pong, got %+v", resp)
	}
}

func TestDispatch_UnknownMethod(t *testing.T) {
	d := NewDispatcher(testMethods())
	resp := d.Dispatch(hc(), nil, &Request{Method: "nonexistent"})
	if resp.Success || resp.Error != CodeUnknownMethod {
		t.Fatalf("expected UNKNOWN_METHOD, got %+v", resp)
	}
}

func TestDispatch_AuthRequired_NoAuth(t *testing.T) {
	d := NewDispatcher(testMethods())
	for _, auth := range []*AuthInfo{nil, {}} {
		resp := d.Dispatch(hc(), auth, &Request{Method: "user.whoami"})
		if resp.Success || resp.Error != CodeAuthRequired {
			t.Fatalf("%+v: expected AUTH_REQUIRED, got %+v", auth, resp)
		}
	}
}

func TestDispatch_AuthRequired_WithAuth(t *testing.T) {
	d := NewDispatcher(testMethods())
	auth := &AuthInfo{UserID: "user123"}
	resp := d.Dispatch(hc(), auth, &Request{Method: "user.whoami"})
	if !resp.Success || resp.Data != "user123" {
		t.Fatalf("expected user123, got %+v", resp)
	}
}

func TestDispatch_NonceDedup(t *testing.T) {
	ns := newMockNonceStore()
	d := NewDispatcher(testMethods(), WithNonceStore(ns))

	resp1 := d.Dispatch(hc(), nil, &Request{Method: "app.ping", Nonce: "n1"})
	if !resp1.Success {
		t.Fatalf("first call should succeed: %+v", resp1)
	}

	resp2 := d.Dispatch(hc(), nil, &Request{Method: "app.ping", Nonce: "n1"})
	if resp2.Success || resp2.Error != CodeDuplicateRequest {
		t.Fatalf("duplicate nonce should fail: %+v", resp2)
	}
}

// A request refused before it runs leaves its nonce unspent, so a retry once
// the caller has what was missing goes through.
func TestDispatch_RefusedRequestKeepsItsNonce(t *testing.T) {
	d := NewDispatcher(testMethods(), WithNonceStore(newMockNonceStore()))
	for _, req := range []*Request{{Method: "nonexistent", Nonce: "n1"}, {Method: "user.whoami", Nonce: "n1"}} {
		if resp := d.Dispatch(hc(), nil, req); resp.Success {
			t.Fatalf("%s: %+v", req.Method, resp)
		}
	}
	if resp := d.Dispatch(hc(), &AuthInfo{UserID: "u1"}, &Request{Method: "user.whoami", Nonce: "n1"}); !resp.Success {
		t.Fatalf("the retry: %+v", resp)
	}
}

// A nonce store that fails is logged, and the request runs: dedup is a guard
// against a client's retry, not a precondition of serving it.
func TestDispatch_NonceStoreFailure(t *testing.T) {
	ns := newMockNonceStore()
	ns.err = errors.New("store down")
	logger, buf := logged()
	d := NewDispatcher(testMethods(), WithNonceStore(ns), WithLogger(logger))
	if resp := d.Dispatch(hc(), nil, &Request{Method: "app.ping", Nonce: "n1"}); !resp.Success {
		t.Fatalf("got %+v", resp)
	}
	if !strings.Contains(buf.String(), "store down") {
		t.Fatalf("not logged: %s", buf)
	}
}

func TestDispatch_EmptyNonce_NoDedupe(t *testing.T) {
	ns := newMockNonceStore()
	d := NewDispatcher(testMethods(), WithNonceStore(ns))

	resp1 := d.Dispatch(hc(), nil, &Request{Method: "app.ping"})
	resp2 := d.Dispatch(hc(), nil, &Request{Method: "app.ping"})
	if !resp1.Success || !resp2.Success {
		t.Fatalf("calls without nonce should not dedupe")
	}
}

func TestDispatch_IDEchoed(t *testing.T) {
	d := NewDispatcher(testMethods())
	resp := d.Dispatch(hc(), nil, &Request{ID: "req_42", Method: "app.ping"})
	if resp.ID != "req_42" {
		t.Fatalf("expected ID req_42, got %s", resp.ID)
	}
}

func TestDispatch_APIError(t *testing.T) {
	d := NewDispatcher(testMethods())
	resp := d.Dispatch(hc(), nil, &Request{Method: "fail.api"})
	if resp.Success || resp.Error != "CUSTOM_ERROR" {
		t.Fatalf("expected CUSTOM_ERROR, got %+v", resp)
	}
}

// A fault is logged under its method and answered INTERNAL_ERROR, unless the
// request's context has ended: its client is gone, and nothing went wrong.
func TestDispatch_InternalError(t *testing.T) {
	logger, buf := logged()
	d := NewDispatcher(testMethods(), WithLogger(logger))
	resp := d.Dispatch(hc(), nil, &Request{Method: "fail.internal"})
	if resp.Success || resp.Error != CodeInternal {
		t.Fatalf("expected INTERNAL_ERROR, got %+v", resp)
	}
	if !strings.Contains(buf.String(), "fail.internal") {
		t.Fatalf("not logged: %s", buf)
	}

	buf.Reset()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if resp := d.Dispatch(&HandlerContext{Ctx: ctx}, nil, &Request{Method: "slow.op"}); resp.Error != CodeInternal {
		t.Fatalf("got %+v", resp)
	}
	if buf.Len() != 0 {
		t.Fatalf("an aborted request logged: %s", buf)
	}
}

func TestDispatch_PublicMethod_OptionalAuthPresent(t *testing.T) {
	d := NewDispatcher(testMethods())
	resp := d.Dispatch(hc(), &AuthInfo{UserID: "user1"}, &Request{Method: "app.ping"})
	if !resp.Success {
		t.Fatalf("public method with optional auth should succeed: %+v", resp)
	}
}

// The locale the application resolves, and its validator, reach every handler.
func TestDispatch_Context(t *testing.T) {
	read := func(d *Dispatcher) map[string]any {
		resp := d.Dispatch(hc(), nil, &Request{Method: "context.read", Locale: "en-NZ"})
		if !resp.Success {
			t.Fatalf("got %+v", resp)
		}
		return resp.Data.(map[string]any)
	}
	if got := read(NewDispatcher(testMethods())); got["locale"] != "" || got["validator"] != false {
		t.Fatalf("bare: %v", got)
	}
	d := NewDispatcher(testMethods(),
		WithLocaleResolver(func(requested string) string { return requested + "!" }),
		WithValidator(nopValidator{}))
	if got := read(d); got["locale"] != "en-NZ!" || got["validator"] != true {
		t.Fatalf("configured: %v", got)
	}
}

type nopValidator struct{}

func (nopValidator) Struct(context.Context, any) error      { return nil }
func (nopValidator) Var(context.Context, any, string) error { return nil }

func TestDispatcher_MarshalFailure(t *testing.T) {
	logger, buf := logged()
	d := NewDispatcher(testMethods(), WithLogger(logger))
	resp := d.Dispatch(hc(), nil, &Request{ID: "7", Method: "result.unmarshalable"})
	var wire Response
	if err := json.Unmarshal(d.marshal(resp), &wire); err != nil || wire.Success || wire.Error != CodeInternal || wire.ID != "7" {
		t.Fatalf("got %+v, %v", wire, err)
	}
	if buf.Len() == 0 {
		t.Fatal("not logged")
	}
}

// An Access without a principal to ask it of, or without a gate to enforce
// it, is a mistake in the program, found at construction.
func TestNewDispatcher_RefusesUnenforcedAccess(t *testing.T) {
	gate := func(ctx context.Context, _ *AuthInfo, _ *MethodDef) (context.Context, error) { return ctx, nil }
	for name, tc := range map[string]struct {
		def  MethodDef
		opts []DispatcherOption
	}{
		"no principal": {MethodDef{Auth: AuthNone, Access: "admin"}, []DispatcherOption{WithAuthGate(gate)}},
		"no gate":      {MethodDef{Auth: AuthRequired, Access: "admin"}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected a panic")
				}
			}()
			NewDispatcher(map[string]MethodDef{"m": tc.def}, tc.opts...)
		})
	}
	NewDispatcher(map[string]MethodDef{"m": {Auth: AuthRequired, Access: "admin"}}, WithAuthGate(gate))
}

func TestMethodDef(t *testing.T) {
	d := NewDispatcher(testMethods())
	if def, ok := d.MethodDef("plain.allowed"); !ok || !def.AllowPlain {
		t.Fatalf("got %+v, %v", def, ok)
	}
	if _, ok := d.MethodDef("nonexistent"); ok {
		t.Fatal("a method the table lacks")
	}
}
