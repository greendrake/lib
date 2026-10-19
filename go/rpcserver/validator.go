package rpcserver

import "context"

// Validator checks a method's parsed arguments against the rules their types
// carry: Struct a struct's field tags, Var one value against rules in the same
// syntax. The dispatcher hands its own (WithValidator) to every handler
// through the request's context; rpcserver/rpcargs parses arguments with it
// and provides one.
type Validator interface {
	Struct(ctx context.Context, v any) error
	Var(ctx context.Context, v any, rules string) error
}

type validatorContextKey struct{}

// ContextWithValidator returns a context carrying v: what the dispatcher does
// for each request, and what code parsing arguments outside one (a test) does
// itself.
func ContextWithValidator(ctx context.Context, v Validator) context.Context {
	return context.WithValue(ctx, validatorContextKey{}, v)
}

// ValidatorFrom is the validator the request's dispatcher was given, or nil.
func ValidatorFrom(ctx context.Context) Validator {
	v, _ := ctx.Value(validatorContextKey{}).(Validator)
	return v
}
