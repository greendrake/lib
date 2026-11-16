// Package rpckittest dispatches a library's methods as its host — the server
// binary whose dispatcher takes them — does, for the library's own tests:
// through rpckit.Gate, over principals that carry their standing as their
// Claims rather than in an account a store holds.
package rpckittest

import (
	"context"

	"greendrake.nz/go/rpckit"
	"greendrake.nz/go/rpcserver"
	"greendrake.nz/go/rpcserver/rpcargs"
)

// Claims is a test principal's standing, as its AuthInfo.Claims. A principal
// without any is an ordinary account.
type Claims struct {
	Admin   bool
	Phantom bool
}

// Gate is rpckit.Gate over principals' Claims: a key principal is an
// operator, and an account is admitted by rpckit.Admit on its Claims.
var Gate = rpckit.Gate(func(ctx context.Context, auth *rpcserver.AuthInfo, access rpckit.Access) (context.Context, error) {
	claims, _ := auth.Claims.(Claims)
	return ctx, rpckit.Admit(access, claims.Admin, claims.Phantom)
})

// NewDispatcher is a dispatcher of methods behind Gate, validating arguments
// with rpcargs' own rules, configured further — or otherwise — by opts.
func NewDispatcher(methods map[string]rpcserver.MethodDef, opts ...rpcserver.DispatcherOption) *rpcserver.Dispatcher {
	return rpcserver.NewDispatcher(methods, append([]rpcserver.DispatcherOption{
		rpcserver.WithAuthGate(Gate),
		rpcserver.WithValidator(rpcargs.NewValidator()),
	}, opts...)...)
}

// Validating is ctx carrying a validator with rules, for a test parsing
// arguments outside a dispatcher.
func Validating(ctx context.Context, rules ...rpcargs.Rule) context.Context {
	return rpcserver.ContextWithValidator(ctx, rpcargs.NewValidator(rules...))
}
