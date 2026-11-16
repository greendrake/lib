package prefix

import (
	"slices"
	"testing"

	"greendrake.nz/go/sixid"
)

var table = Table{"user": 'U', "order": 'O'}

func TestPrefixedRoundTrip(t *testing.T) {
	c := New(table)
	id := sixid.SixID{1, 2, 3, 4, 5, 6}
	s := c.Encode("user", id)
	if s != "U"+id.String() || len(s) != 11 {
		t.Fatalf("Encode = %q", s)
	}
	got, err := c.Decode(s, "user")
	if err != nil || got != id {
		t.Fatalf("Decode = %v, %v", got, err)
	}
	e, got, err := c.DecodeAny("O" + id.String())
	if err != nil || e != "order" || got != id {
		t.Fatalf("DecodeAny = %q, %v, %v", e, got, err)
	}
	if c.Letter("order") != 'O' {
		t.Fatal("Letter")
	}
	if want := []Entity{"order", "user"}; !slices.Equal(c.Entities(), want) {
		t.Fatalf("Entities = %v, want %v", c.Entities(), want)
	}
}

func TestPrefixedDecodeRejects(t *testing.T) {
	c := New(table)
	id := sixid.SixID{1, 2, 3, 4, 5, 6}
	for name, s := range map[string]string{
		"another entity's id": "O" + id.String(),
		"unknown prefix":      "Z" + id.String(),
		"empty":               "",
		"malformed body":      "U!!",
	} {
		if _, err := c.Decode(s, "user"); err == nil {
			t.Errorf("%s: Decode(%q) must fail", name, s)
		}
	}
	// An entity the table lacks decodes nothing.
	if _, err := c.Decode("U"+id.String(), "invoice"); err == nil {
		t.Error("an id cannot decode as an entity outside the table")
	}
}

func TestPrefixedPanicsOnMisuse(t *testing.T) {
	assertPanics := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s must panic", name)
			}
		}()
		fn()
	}
	assertPanics("shared letter", func() { New(Table{"a": 'X', "b": 'X'}) })
	assertPanics("unknown entity", func() { New(table).Encode("invoice", sixid.SixID{}) })
}

func TestPlain(t *testing.T) {
	id := sixid.SixID{1, 2, 3, 4, 5, 6}
	s := Plain.Encode("anything", id)
	if s != id.String() {
		t.Fatalf("Encode = %q", s)
	}
	got, err := Plain.Decode(s, "whatever")
	if err != nil || got != id {
		t.Fatalf("Decode = %v, %v", got, err)
	}
}
