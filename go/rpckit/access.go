package rpckit

import (
	"context"
	"fmt"

	"greendrake.nz/go/rpcserver"
)

// Access is what a method asks of its principal (rpcserver.MethodDef.Access).
// A method with none asks only for a principal its host's gate admits.
type Access int

const (
	// Admin admits operators alone: a key principal, or an account its host
	// holds to be an admin.
	Admin Access = iota + 1
	// AllowPhantom admits a phantom (provisional) account beside a full one.
	AllowPhantom
)

// The codes an AccountGate refuses an account with.
const (
	CodeAdminRequired     = "ADMIN_REQUIRED"
	CodePhantomNotAllowed = "PHANTOM_NOT_ALLOWED"
)

// AccountGate admits or refuses an account principal to a method asking
// access of it — zero for a method asking nothing more than a principal — as
// an rpcserver.AuthGate does. It finds the account's standing, and has Admit
// rule on it.
type AccountGate func(ctx context.Context, auth *rpcserver.AuthInfo, access Access) (context.Context, error)

// Admit is the access policy over an account's standing: Admin admits an
// admin alone, and a phantom is admitted only where a method allows one.
func Admit(access Access, admin, phantom bool) error {
	switch {
	case access == Admin && !admin:
		return rpcserver.NewAPIError(CodeAdminRequired)
	case phantom && access != AllowPhantom:
		return rpcserver.NewAPIError(CodePhantomNotAllowed)
	}
	return nil
}

// OperatorKey is the dispatcher's auth of a host whose one principal is its
// operators' pre-shared key: the key, as principal "admin" talking plain on
// the wire, admitted to every method, and nobody else.
func OperatorKey(key string) []rpcserver.DispatcherOption {
	return OperatorKeyAs(key, rpcserver.AuthInfo{UserID: "admin", PlainTransport: true})
}

// OperatorKeyAs is OperatorKey with the principal the key resolves to: a
// host whose operator is also one of its accounts.
func OperatorKeyAs(key string, principal rpcserver.AuthInfo) []rpcserver.DispatcherOption {
	return []rpcserver.DispatcherOption{
		rpcserver.WithAuthResolver(&rpcserver.KeyAuthResolver{Key: key, AuthInfo: principal}),
		rpcserver.WithAuthGate(Gate(nil)),
	}
}

// Gate is the rpcserver.AuthGate of a host whose principals are its
// operators' keys and, where it has accounts, accounts. A key principal is an
// operator, and passes any access; an account principal goes to accounts.
// Without accounts (nil), only key principals are admitted. A method whose
// Access is not an Access is a mistake in the program, and a fault.
func Gate(accounts AccountGate) rpcserver.AuthGate {
	return func(ctx context.Context, auth *rpcserver.AuthInfo, def *rpcserver.MethodDef) (context.Context, error) {
		var access Access
		if def.Access != nil {
			a, ok := def.Access.(Access)
			if !ok {
				return nil, fmt.Errorf("rpckit: a method's Access is a %T, not an rpckit.Access", def.Access)
			}
			access = a
		}
		switch {
		case auth.Key:
			return ctx, nil
		case accounts == nil:
			return nil, rpcserver.NewAPIError(rpcserver.CodeUnauthenticated)
		}
		return accounts(ctx, auth, access)
	}
}
