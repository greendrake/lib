package messageformat

import (
	"testing"

	"golang.org/x/text/currency"
	"golang.org/x/text/language"
)

func TestRender_ArgSubstitution(t *testing.T) {
	got, err := Render(language.AmericanEnglish, `Hello {name}, welcome to {place}`, map[string]any{
		"name":  "Ada",
		"place": "Acme",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "Hello Ada, welcome to Acme"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A substituted value is inserted verbatim, never re-parsed — so user-supplied
// content containing braces or '#' is safe.
func TestRender_ValueNotReparsed(t *testing.T) {
	got, err := Render(language.AmericanEnglish, `Subject: {subject}`, map[string]any{
		"subject": "weird {one, plural} # text",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "Subject: weird {one, plural} # text"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// '#' is the count inside a plural and a literal everywhere else.
func TestRender_HashLiteralOutsidePlural(t *testing.T) {
	got, err := Render(language.AmericanEnglish, `ticket #{id}`, map[string]any{"id": "42"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "ticket #42"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRender_EnglishPlural(t *testing.T) {
	const msg = `{match_count, plural, one {# new listing} other {# new listings}} for your saved search "{search_name}"`
	cases := []struct {
		count int
		want  string
	}{
		{0, `0 new listings for your saved search "boats"`},
		{1, `1 new listing for your saved search "boats"`},
		{2, `2 new listings for your saved search "boats"`},
		{1000, `1,000 new listings for your saved search "boats"`}, // locale number grouping
	}
	for _, c := range cases {
		got, err := Render(language.AmericanEnglish, msg, map[string]any{
			"match_count": c.count,
			"search_name": "boats",
		})
		if err != nil {
			t.Fatalf("count=%d: unexpected error: %v", c.count, err)
		}
		if got != c.want {
			t.Fatalf("count=%d: got %q, want %q", c.count, got, c.want)
		}
	}
}

// An "=N" selector takes precedence over the keyword categories.
func TestRender_ExactCaseWins(t *testing.T) {
	const msg = `{n, plural, =0 {No new listings} one {# new listing} other {# new listings}}`
	cases := map[int]string{
		0: "No new listings",
		1: "1 new listing",
		2: "2 new listings",
	}
	for n, want := range cases {
		got, err := Render(language.AmericanEnglish, msg, map[string]any{"n": n})
		if err != nil {
			t.Fatalf("n=%d: unexpected error: %v", n, err)
		}
		if got != want {
			t.Fatalf("n=%d: got %q, want %q", n, got, want)
		}
	}
}

// The headline guard against an English-hardcoded implementation: the SAME
// message string selects different CLDR plural categories per locale. Marker
// words stand in for the categories so the assertion is about form selection,
// not vocabulary.
func TestRender_PluralIsLocaleDriven(t *testing.T) {
	const msg = `{n, plural, one {ONE} few {FEW} many {MANY} other {OTHER}}`

	english := map[int]string{1: "ONE", 2: "OTHER", 5: "OTHER", 11: "OTHER", 21: "OTHER"}
	for n, want := range english {
		got, err := Render(language.AmericanEnglish, msg, map[string]any{"n": n})
		if err != nil {
			t.Fatalf("en n=%d: unexpected error: %v", n, err)
		}
		if got != want {
			t.Fatalf("en n=%d: got %q, want %q", n, got, want)
		}
	}

	// Russian uses one/few/many; e.g. 2→few and 5→many where English gives other.
	russian := map[int]string{1: "ONE", 2: "FEW", 5: "MANY", 11: "MANY", 21: "ONE", 22: "FEW"}
	for n, want := range russian {
		got, err := Render(language.Russian, msg, map[string]any{"n": n})
		if err != nil {
			t.Fatalf("ru n=%d: unexpected error: %v", n, err)
		}
		if got != want {
			t.Fatalf("ru n=%d: got %q, want %q", n, got, want)
		}
	}
}

// A currency.Amount argument renders as a locale-formatted, symbol-prefixed
// string (mirroring the frontend's Intl.NumberFormat(style:'currency')), with
// locale grouping and the currency's own fraction-digit count — including
// rounding for a zero-fraction currency such as JPY.
func TestRender_CurrencyAmount(t *testing.T) {
	cases := []struct {
		code string
		amt  float64
		want string
	}{
		{"NZD", 1200.5, "Offer: NZ$ 1,200.50"},
		{"USD", 1234567.89, "Offer: $ 1,234,567.89"},
		{"JPY", 1200.5, "Offer: ¥ 1,201"},
	}
	for _, c := range cases {
		got, err := Render(language.AmericanEnglish, `Offer: {offer_amount}`, map[string]any{
			"offer_amount": currency.MustParseISO(c.code).Amount(c.amt),
		})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.code, err)
		}
		if got != c.want {
			t.Fatalf("%s: got %q, want %q", c.code, got, c.want)
		}
	}
}

func TestRender_Errors(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		args map[string]any
	}{
		{"missing arg", `Hello {name}`, map[string]any{}},
		{"missing plural arg", `{n, plural, other {x}}`, map[string]any{}},
		{"non-numeric plural arg", `{n, plural, other {x}}`, map[string]any{"n": "lots"}},
		{"non-int plural arg", `{n, plural, other {x}}`, map[string]any{"n": int64(2)}},
		{"unbalanced close brace", `oops }`, map[string]any{}},
		{"unterminated placeholder", `{name`, map[string]any{"name": "x"}},
		{"unsupported format", `{n, select, other {x}}`, map[string]any{"n": 1}},
		{"unknown plural keyword", `{n, plural, lots {x} other {y}}`, map[string]any{"n": 1}},
		{"no other fallback", `{n, plural, one {x}}`, map[string]any{"n": 2}},
		{"empty plural", `{n, plural, }`, map[string]any{"n": 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Render(language.AmericanEnglish, c.msg, c.args); err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
}
