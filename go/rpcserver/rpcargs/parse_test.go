package rpcargs

import (
	"encoding/json"
	"errors"
	"testing"

	"greendrake.nz/go/rpcserver"
)

// invalidArgument fails the test unless err is INVALID_ARGUMENT, and returns
// its field errors.
func invalidArgument(t *testing.T, err error) []FieldError {
	t.Helper()
	var apiErr *rpcserver.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != rpcserver.CodeInvalidArgument {
		t.Fatalf("expected INVALID_ARGUMENT, got %v", err)
	}
	details, _ := apiErr.Details.([]FieldError)
	return details
}

func TestParseArguments_NamedObject(t *testing.T) {
	var target struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	if err := ParseArguments(json.RawMessage(`{"name":"Alice","age":30}`), &target); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.Name != "Alice" || target.Age != 30 {
		t.Fatalf("unexpected result: %+v", target)
	}
}

func TestParseArguments_PositionalArray(t *testing.T) {
	var target string
	if err := ParseArguments(json.RawMessage(`["hello"]`), &target); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "hello" {
		t.Fatalf("expected hello, got %s", target)
	}
}

// A request without arguments, or with an empty array, is one with none: the
// target keeps its zero value.
func TestParseArguments_None(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`[]`)} {
		var target struct {
			Name string `json:"name"`
		}
		if err := ParseArguments(raw, &target); err != nil || target.Name != "" {
			t.Fatalf("%s: %+v, %v", raw, target, err)
		}
	}
}

func TestParseArguments_Invalid(t *testing.T) {
	for name, raw := range map[string]string{
		"not json":           `not_json`,
		"object of the type": `{"name":1}`,
		"broken array":       `[1,`,
		"element of a type":  `[{"name":1}]`,
		"scalar":             `"x"`,
	} {
		t.Run(name, func(t *testing.T) {
			var target struct {
				Name string `json:"name"`
			}
			invalidArgument(t, ParseArguments(json.RawMessage(raw), &target))
		})
	}
}

func TestParsePositional_AllElements(t *testing.T) {
	var name string
	var count int
	var flag bool
	if err := ParsePositional(json.RawMessage(`["x", 7, true]`), &name, &count, &flag); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "x" || count != 7 || !flag {
		t.Fatalf("unexpected results: %q %d %v", name, count, flag)
	}
}

func TestParsePositional_FewerElementsLeaveZeroValues(t *testing.T) {
	var name string
	count := 42 // pre-set: an absent element must leave the target untouched
	if err := ParsePositional(json.RawMessage(`["only"]`), &name, &count); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "only" || count != 42 {
		t.Fatalf("unexpected results: %q %d", name, count)
	}
}

func TestParsePositional_NullElementLeavesTarget(t *testing.T) {
	var class *string
	var name string
	if err := ParsePositional(json.RawMessage(`[null, "prop"]`), &class, &name); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if class != nil || name != "prop" {
		t.Fatalf("unexpected results: %v %q", class, name)
	}
}

func TestParsePositional_AbsentLeavesTargets(t *testing.T) {
	var name string
	count := 42
	if err := ParsePositional(nil, &name, &count); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" || count != 42 {
		t.Fatalf("unexpected results: %q %d", name, count)
	}
}

func TestParsePositional_ComplexTargets(t *testing.T) {
	var patch map[string]any
	var ids []string
	if err := ParsePositional(json.RawMessage(`[{"a":1}, ["x","y"]]`), &patch, &ids); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patch["a"] != float64(1) || len(ids) != 2 || ids[1] != "y" {
		t.Fatalf("unexpected results: %v %v", patch, ids)
	}
}

func TestParsePositional_Invalid(t *testing.T) {
	for name, raw := range map[string]string{
		"object":       `{"a":1}`,
		"scalar":       `"x"`,
		"not json":     `not json`,
		"wrong shape":  `[{"a":1}]`, // object into string target
		"too many":     `["a", "b"]`,
		"broken array": `["a",`,
	} {
		t.Run(name, func(t *testing.T) {
			var s string
			invalidArgument(t, ParsePositional(json.RawMessage(raw), &s))
		})
	}
}
