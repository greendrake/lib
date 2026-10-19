package rpcserver

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

type gateKey struct{}

// gatedMethods answer with what the gate attached to their ctx.
func gatedMethods() map[string]MethodDef {
	handler := func(hc *HandlerContext, _ json.RawMessage) (any, error) {
		return hc.Ctx.Value(gateKey{}), nil
	}
	return map[string]MethodDef{
		"public": {Auth: AuthNone, Handler: handler},
		"user":   {Auth: AuthRequired, Handler: handler},
		"admin":  {Auth: AuthRequired, Access: "admin", Handler: handler},
	}
}

func TestAuthGate(t *testing.T) {
	var asked []any
	gate := func(ctx context.Context, auth *AuthInfo, def *MethodDef) (context.Context, error) {
		asked = append(asked, def.Access)
		switch auth.UserID {
		case "barred":
			return nil, NewAPIError(CodeUnauthenticated)
		case "broken":
			return nil, errors.New("store down")
		}
		return context.WithValue(ctx, gateKey{}, auth.UserID), nil
	}
	d := NewDispatcher(gatedMethods(), WithAuthGate(gate), WithNonceStore(newMockNonceStore()))
	call := func(method string, auth *AuthInfo) *Response {
		return d.Dispatch(hc(), auth, &Request{Method: method, Nonce: method})
	}

	t.Run("every principal of a method requiring one passes the gate, which sees its Access", func(t *testing.T) {
		asked = nil
		for _, auth := range []*AuthInfo{{UserID: "u1"}, {UserID: "u1", Key: true}} {
			for _, method := range []string{"user", "admin"} {
				resp := d.Dispatch(hc(), auth, &Request{Method: method})
				if !resp.Success || resp.Data != "u1" {
					t.Fatalf("%s: the handler must see the gate's ctx: %+v", method, resp)
				}
			}
		}
		if want := []any{nil, "admin", nil, "admin"}; !slices.Equal(asked, want) {
			t.Fatalf("gate asked %v, want %v", asked, want)
		}
	})

	t.Run("public methods bypass it", func(t *testing.T) {
		asked = nil
		if resp := call("public", &AuthInfo{UserID: "u1"}); !resp.Success {
			t.Fatalf("public: %+v", resp)
		}
		if len(asked) != 0 {
			t.Fatalf("gate ran %d times, want none", len(asked))
		}
	})

	t.Run("a refusal answers by its code, leaving the nonce unspent", func(t *testing.T) {
		if resp := call("user", &AuthInfo{UserID: "barred"}); resp.Success || resp.Error != CodeUnauthenticated {
			t.Fatalf("got %+v", resp)
		}
		if resp := call("user", &AuthInfo{UserID: "u1"}); !resp.Success {
			t.Fatalf("the retry: %+v", resp)
		}
	})

	t.Run("a gate failure is an internal error", func(t *testing.T) {
		if resp := call("admin", &AuthInfo{UserID: "broken"}); resp.Success || resp.Error != CodeInternal {
			t.Fatalf("got %+v", resp)
		}
	})

	t.Run("a request without a principal never reaches it", func(t *testing.T) {
		asked = nil
		if resp := d.Dispatch(hc(), nil, &Request{Method: "user"}); resp.Error != CodeAuthRequired {
			t.Fatalf("got %+v", resp)
		}
		if len(asked) != 0 {
			t.Fatal("the gate ran for a refused request")
		}
	})
}

func TestUserID(t *testing.T) {
	ctx := WithAuth(context.Background(), &AuthInfo{UserID: "u1"})
	id, err := UserID[string](&HandlerContext{Ctx: ctx})
	if err != nil || id != "u1" {
		t.Fatalf("got %q, %v", id, err)
	}

	if _, err := UserID[int](&HandlerContext{Ctx: ctx}); err == nil {
		t.Fatal("a principal id of another type must fail")
	}

	_, err = UserID[string](&HandlerContext{Ctx: context.Background()})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != CodeAuthRequired {
		t.Fatalf("no principal: got %v", err)
	}
}
