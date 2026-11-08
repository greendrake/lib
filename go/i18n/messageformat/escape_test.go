package messageformat

import (
	"testing"

	"golang.org/x/text/language"
)

func TestEscapeArgs(t *testing.T) {
	in := map[string]any{
		"name":    `Ada & "Bob" <x>`,
		"body":    "line one\nline two",
		"safe":    SafeHTML(`<a href="/x">link</a>`),
		"both":    Alternative{Text: "a < b", HTML: "a &lt; <i>b</i>"},
		"count":   3,
		"missing": nil,
	}
	out := EscapeArgs(in)
	if got, want := out["both"], "a &lt; <i>b</i>"; got != want {
		t.Errorf("Alternative not taken as its HTML: got %q want %q", got, want)
	}

	if got, want := out["name"], "Ada &amp; &#34;Bob&#34; &lt;x&gt;"; got != want {
		t.Errorf("string not escaped: got %q want %q", got, want)
	}
	if got, want := out["body"], "line one<br>line two"; got != want {
		t.Errorf("newline not converted: got %q want %q", got, want)
	}
	if got, want := out["safe"], `<a href="/x">link</a>`; got != want {
		t.Errorf("SafeHTML not passed through: got %q want %q", got, want)
	}
	if out["count"] != 3 {
		t.Errorf("non-string mutated: got %v", out["count"])
	}
	// The input map must be left untouched (a copy is returned).
	if in["name"] != `Ada & "Bob" <x>` {
		t.Errorf("input map mutated: %v", in["name"])
	}
}

func TestAnchor(t *testing.T) {
	if got, want := Anchor(`/x?a=1&b="2"`, "<Ada>"), `<a href="/x?a=1&amp;b=&#34;2&#34;">&lt;Ada&gt;</a>`; got != want {
		t.Errorf("Anchor = %q, want %q", got, want)
	}
}

// A text render takes an Alternative as its text.
func TestRender_AlternativeText(t *testing.T) {
	got, err := Render(language.English, "{m}", map[string]any{"m": Alternative{Text: "a < b", HTML: "a &lt; <i>b</i>"}})
	if err != nil || got != "a < b" {
		t.Fatalf("Render = %q, %v", got, err)
	}
}
