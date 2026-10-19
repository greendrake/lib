package rpcargs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/go-playground/validator/v10"

	"greendrake.nz/go/rpcserver"
)

// Rule is a validation tag and the check it names, which may read the
// request's context.
type Rule struct {
	Tag string
	Fn  validator.FuncCtx
}

// trimmedNonempty is the one tag beyond validator's own that every validator
// has: a string that is not empty once strings.TrimSpace has had it.
var trimmedNonempty = Rule{Tag: "trimmed_nonempty", Fn: func(_ context.Context, fl validator.FieldLevel) bool {
	return strings.TrimSpace(fl.Field().String()) != ""
}}

// NewValidator is go-playground/validator's, as a dispatcher takes it
// (rpcserver.WithValidator): validator's own tags, trimmed_nonempty, and
// rules. Its failures are INVALID_ARGUMENT answers whose details name each
// field (as the client sent it), its rule and the rule's parameter. A tag
// given twice is a mistake in the program, and panics.
func NewValidator(rules ...Rule) rpcserver.Validator {
	v := validator.New(validator.WithRequiredStructEnabled())
	// Surface json-tag names (not Go struct field names) in FieldError
	// payloads, so the client's error display matches what it sent.
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		switch {
		case name == "" && fld.Anonymous:
			return embedded
		case name == "-":
			return ""
		}
		return name
	})
	registered := map[string]bool{}
	for _, r := range append([]Rule{trimmedNonempty}, rules...) {
		if registered[r.Tag] {
			panic(fmt.Sprintf("rpcargs: rule %s given twice", r.Tag))
		}
		registered[r.Tag] = true
		if err := v.RegisterValidationCtx(r.Tag, r.Fn); err != nil {
			panic(fmt.Sprintf("rpcargs: register %s: %v", r.Tag, err))
		}
	}
	return validate{v}
}

type validate struct {
	v *validator.Validate
}

func (val validate) Struct(ctx context.Context, v any) error {
	return answer(val.v.StructCtx(ctx, v))
}

func (val validate) Var(ctx context.Context, v any, rules string) error {
	return answer(val.v.VarCtx(ctx, v, rules))
}

// answer is validator's verdict as the wire says it: none, INVALID_ARGUMENT
// with a FieldError per failed rule, or, for anything validator could not
// check at all, the fault it is.
func answer(err error) error {
	if err == nil {
		return nil
	}
	var ve validator.ValidationErrors
	if !errors.As(err, &ve) {
		return err
	}
	details := make([]FieldError, 0, len(ve))
	for _, fe := range ve {
		details = append(details, FieldError{Field: jsonFieldName(fe), Rule: fe.Tag(), Param: fe.Param()})
	}
	return rpcserver.NewAPIErrorWithDetails(rpcserver.CodeInvalidArgument, details)
}

// FieldError describes a single failed validation rule. It is the
// element type of the slice attached to an INVALID_ARGUMENT response's
// error_details payload.
type FieldError struct {
	Field string `json:"field"` // JSON field path as the client sent it; "" for a positional argument
	Rule  string `json:"rule"`  // Tag that failed (required, email, gt, …)
	Param string `json:"param,omitempty"`
}

// ParseAndValidate parses a request's raw arguments into a T and runs struct
// validation with the request's validator (rpcserver.ValidatorFrom), whose
// absence is a fault. T must be a struct type — for primitive arguments, use
// ParseAndValidateVar instead. A request without arguments is T's zero value,
// validated like any other.
func ParseAndValidate[T any](ctx context.Context, raw json.RawMessage) (T, error) {
	var v T
	val, err := validatorOf(ctx)
	if err != nil {
		return v, err
	}
	if err := ParseArguments(raw, &v); err != nil {
		return v, err
	}
	return v, val.Struct(ctx, v)
}

// ParseAndValidateVar parses a request's raw arguments into a single
// primitive (or slice) value and validates it against rules, in the syntax of
// struct tags: `required,email`. Use this for a method whose one argument is
// a primitive — e.g. `["someEmail@x"]`.
func ParseAndValidateVar[T any](ctx context.Context, raw json.RawMessage, rules string) (T, error) {
	var v T
	val, err := validatorOf(ctx)
	if err != nil {
		return v, err
	}
	if err := ParseArguments(raw, &v); err != nil {
		return v, err
	}
	return v, val.Var(ctx, v, rules)
}

func validatorOf(ctx context.Context) (rpcserver.Validator, error) {
	val := rpcserver.ValidatorFrom(ctx)
	if val == nil {
		return nil, errors.New("rpcargs: the request carries no validator (rpcserver.WithValidator)")
	}
	return val, nil
}

// OneOf refuses value, an argument's field, outside allowed — a set known only
// at run time, which a `oneof` tag cannot name — as the tag would.
func OneOf(field, value string, allowed []string) error {
	if slices.Contains(allowed, value) {
		return nil
	}
	return rpcserver.NewAPIErrorWithDetails(rpcserver.CodeInvalidArgument, []FieldError{{Field: field, Rule: "oneof", Param: strings.Join(allowed, " ")}})
}

// embedded names an anonymous struct field with no JSON name of its own:
// on the wire its fields are its parent's, so a path skips it.
const embedded = "\x00"

// jsonFieldName returns the dotted JSON path for a failed field, "" for a
// lone value's. The validator's RegisterTagNameFunc replaces Go struct field
// names with json-tag names; here we strip the top-level struct segment, and
// every embedded one, so the path matches what the client sent.
func jsonFieldName(fe validator.FieldError) string {
	segments := strings.Split(fe.Namespace(), ".")
	if len(segments) == 1 {
		return fe.Field()
	}
	return strings.Join(slices.DeleteFunc(segments[1:], func(s string) bool { return s == embedded }), ".")
}
