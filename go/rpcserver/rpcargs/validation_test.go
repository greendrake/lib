package rpcargs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/go-playground/validator/v10"

	"greendrake.nz/go/rpcserver"
)

type sampleArgs struct {
	Name  string `json:"name"  validate:"required,min=2"`
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age"   validate:"gte=18,lte=120"`
}

// validating is a context carrying a validator of rules, as a dispatcher's
// requests do.
func validating(rules ...Rule) context.Context {
	return rpcserver.ContextWithValidator(context.Background(), NewValidator(rules...))
}

// rules is each failed field's rule.
func rules(details []FieldError) map[string]string {
	got := map[string]string{}
	for _, fe := range details {
		got[fe.Field] = fe.Rule
	}
	return got
}

func wantRules(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for field, rule := range want {
		if got[field] != rule {
			t.Errorf("%s: got %q, want %q (all: %v)", field, got[field], rule, got)
		}
	}
}

func TestParseAndValidate_Success(t *testing.T) {
	v, err := ParseAndValidate[sampleArgs](validating(), json.RawMessage(`{"name":"Alice","email":"a@x.io","age":30}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Name != "Alice" || v.Email != "a@x.io" || v.Age != 30 {
		t.Fatalf("unexpected value: %+v", v)
	}
}

func TestParseAndValidate_FieldErrors(t *testing.T) {
	_, err := ParseAndValidate[sampleArgs](validating(), json.RawMessage(`{"name":"A","email":"not-an-email","age":5}`))
	wantRules(t, rules(invalidArgument(t, err)), map[string]string{"name": "min", "email": "email", "age": "gte"})
}

// A field of an embedded struct is its parent's on the wire, and its path
// says so; a nested one keeps its own segment.
func TestParseAndValidate_EmbeddedFieldPaths(t *testing.T) {
	type ref struct {
		ID string `json:"id" validate:"required"`
	}
	type page struct {
		Limit int `json:"limit" validate:"max=1000"`
	}
	type args struct {
		ref
		page
		Inner struct {
			Code string `json:"code" validate:"required"`
		} `json:"inner"`
		Ignored string `json:"-" validate:"required"`
	}
	_, err := ParseAndValidate[args](validating(), json.RawMessage(`{"limit":5000}`))
	// A field the wire never carries has no name there: it goes by its own.
	wantRules(t, rules(invalidArgument(t, err)), map[string]string{"id": "required", "limit": "max", "inner.code": "required", "Ignored": "required"})
}

// A request without arguments validates as their zero value: what they require is
// reported missing.
func TestParseAndValidate_Absent(t *testing.T) {
	_, err := ParseAndValidate[sampleArgs](validating(), nil)
	wantRules(t, rules(invalidArgument(t, err)), map[string]string{"name": "required", "email": "required", "age": "gte"})
}

func TestParseAndValidate_Unparsable(t *testing.T) {
	_, err := ParseAndValidate[sampleArgs](validating(), json.RawMessage(`{"name":1}`))
	if details := invalidArgument(t, err); details != nil {
		t.Fatalf("a parse failure names no field: %+v", details)
	}
}

// Without a validator, and for a T validator cannot check, there is no answer
// to give the client: either is a fault of the program.
func TestParseAndValidate_Faults(t *testing.T) {
	var apiErr *rpcserver.APIError
	if _, err := ParseAndValidate[sampleArgs](context.Background(), nil); err == nil || errors.As(err, &apiErr) {
		t.Fatalf("no validator: %v", err)
	}
	if _, err := ParseAndValidate[string](validating(), json.RawMessage(`["x"]`)); err == nil || errors.As(err, &apiErr) {
		t.Fatalf("not a struct: %v", err)
	}
	if _, err := ParseAndValidateVar[string](context.Background(), json.RawMessage(`["x"]`), "required"); err == nil || errors.As(err, &apiErr) {
		t.Fatalf("no validator: %v", err)
	}
}

func TestParseAndValidateVar(t *testing.T) {
	v, err := ParseAndValidateVar[string](validating(), json.RawMessage(`["alice@example.com"]`), "required,email")
	if err != nil || v != "alice@example.com" {
		t.Fatalf("got %q, %v", v, err)
	}

	_, err = ParseAndValidateVar[string](validating(), json.RawMessage(`["nope"]`), "required,email")
	if got := invalidArgument(t, err); len(got) != 1 || got[0] != (FieldError{Rule: "email"}) {
		t.Fatalf("details %+v", got)
	}

	_, err = ParseAndValidateVar[string](validating(), json.RawMessage(`[1]`), "required")
	invalidArgument(t, err)
}

func TestOneOf(t *testing.T) {
	if err := OneOf("kind", "b", []string{"a", "b"}); err != nil {
		t.Fatalf("a member: %v", err)
	}
	want := FieldError{Field: "kind", Rule: "oneof", Param: "a b"}
	if got := invalidArgument(t, OneOf("kind", "c", []string{"a", "b"})); len(got) != 1 || got[0] != want {
		t.Fatalf("details %+v, want %+v", got, want)
	}
}

func TestTrimmedNonempty(t *testing.T) {
	for input, ok := range map[string]bool{"hello": true, " x ": true, "   ": false, "\t\n": false, "": false} {
		raw, _ := json.Marshal([]string{input})
		_, err := ParseAndValidateVar[string](validating(), raw, "trimmed_nonempty")
		if (err == nil) != ok {
			t.Errorf("%q: ok %v, got %v", input, ok, err)
		}
	}
}

// A rule reads the request's context.
func TestRule_ReadsTheContext(t *testing.T) {
	type ctxKey struct{}
	ctx := validating(Rule{Tag: "ctxonly", Fn: func(ctx context.Context, fl validator.FieldLevel) bool {
		v, _ := ctx.Value(ctxKey{}).(string)
		return v == "expected"
	}})
	type ctxArgs struct {
		Token string `json:"token" validate:"ctxonly"`
	}
	raw := json.RawMessage(`{"token":"anything"}`)

	if _, err := ParseAndValidate[ctxArgs](context.WithValue(ctx, ctxKey{}, "expected"), raw); err != nil {
		t.Fatalf("with matching ctx: %v", err)
	}
	_, err := ParseAndValidate[ctxArgs](ctx, raw)
	invalidArgument(t, err)
}

func TestNewValidator_RuleTwice(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a tag given twice must panic")
		}
	}()
	NewValidator(Rule{Tag: "trimmed_nonempty", Fn: func(context.Context, validator.FieldLevel) bool { return true }})
}

func TestNewValidator_BadTag(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a tag validator refuses must panic")
		}
	}()
	NewValidator(Rule{Tag: "", Fn: func(context.Context, validator.FieldLevel) bool { return true }})
}
