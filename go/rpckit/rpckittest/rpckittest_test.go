package rpckittest_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-playground/validator/v10"

	"greendrake.nz/go/rpckit"
	"greendrake.nz/go/rpckit/rpckittest"
	"greendrake.nz/go/rpcserver"
	"greendrake.nz/go/rpcserver/rpcargs"
)

func TestDispatcher(t *testing.T) {
	ok := func(*rpcserver.HandlerContext, json.RawMessage) (any, error) { return true, nil }
	d := rpckittest.NewDispatcher(map[string]rpcserver.MethodDef{
		"user":    {Handler: ok, Auth: rpcserver.AuthRequired},
		"admin":   {Handler: ok, Auth: rpcserver.AuthRequired, Access: rpckit.Admin},
		"phantom": {Handler: ok, Auth: rpcserver.AuthRequired, Access: rpckit.AllowPhantom},
		"named": {Handler: func(hc *rpcserver.HandlerContext, raw json.RawMessage) (any, error) {
			_, err := rpcargs.ParseAndValidate[struct {
				Name string `json:"name" validate:"trimmed_nonempty"`
			}](hc.Ctx, raw)
			return true, err
		}},
	})
	account := &rpcserver.AuthInfo{UserID: 1}
	admin := &rpcserver.AuthInfo{UserID: 2, Claims: rpckittest.Claims{Admin: true}}
	phantom := &rpcserver.AuthInfo{UserID: 3, Claims: rpckittest.Claims{Phantom: true}}
	key := &rpcserver.AuthInfo{UserID: "ops", Key: true}
	for _, tc := range []struct {
		who    *rpcserver.AuthInfo
		method string
		code   string
	}{
		{account, "user", ""},
		{account, "admin", rpckit.CodeAdminRequired},
		{account, "phantom", ""},
		{admin, "admin", ""},
		{key, "admin", ""},
		{phantom, "user", rpckit.CodePhantomNotAllowed},
		{phantom, "admin", rpckit.CodeAdminRequired},
		{phantom, "phantom", ""},
	} {
		resp := d.Dispatch(&rpcserver.HandlerContext{Ctx: context.Background()}, tc.who, &rpcserver.Request{Method: tc.method})
		if resp.Error != tc.code {
			t.Errorf("%+v %s: %+v, want %q", tc.who, tc.method, resp, tc.code)
		}
	}
	resp := d.Dispatch(&rpcserver.HandlerContext{Ctx: context.Background()}, nil, &rpcserver.Request{Method: "named", Arguments: json.RawMessage(`{"name":" "}`)})
	if resp.Error != rpcserver.CodeInvalidArgument {
		t.Fatalf("validated: %+v", resp)
	}
}

// The caller's options come after the defaults, so a validator given replaces
// the default one.
func TestDispatcherOptions(t *testing.T) {
	d := rpckittest.NewDispatcher(map[string]rpcserver.MethodDef{
		"loud": {Handler: func(hc *rpcserver.HandlerContext, raw json.RawMessage) (any, error) {
			return rpcargs.ParseAndValidateVar[string](hc.Ctx, raw, "loud")
		}},
	}, rpcserver.WithValidator(rpcargs.NewValidator(rpcargs.Rule{Tag: "loud", Fn: func(_ context.Context, fl validator.FieldLevel) bool {
		return fl.Field().String() == "HEY"
	}})))
	for arg, code := range map[string]string{`["HEY"]`: "", `["hey"]`: rpcserver.CodeInvalidArgument} {
		if resp := d.Dispatch(&rpcserver.HandlerContext{Ctx: context.Background()}, nil, &rpcserver.Request{Method: "loud", Arguments: json.RawMessage(arg)}); resp.Error != code {
			t.Errorf("%s: %+v", arg, resp)
		}
	}
}

func TestValidating(t *testing.T) {
	if rpcserver.ValidatorFrom(rpckittest.Validating(context.Background())) == nil {
		t.Fatal("no validator")
	}
}
