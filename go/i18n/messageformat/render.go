// Package messageformat renders a small subset of ICU MessageFormat: argument
// substitution and CLDR plural selection, nothing else. Grammar
// (pluralization) therefore lives in the locale-scoped message, driven by
// language-neutral arguments from call sites; plural-category selection uses
// CLDR rules (golang.org/x/text), so a message's own locale — not the caller —
// decides the wording.
//
// Supported syntax:
//
//	{name}                                   argument substitution
//	{name, plural, =N {…} one {…} other {…}} CLDR plural selection
//
// Inside a plural case, `#` renders the count, locale-number-formatted; an `=N`
// selector is an exact match taking precedence over the keyword categories
// (zero/one/two/few/many/other); `other` is the mandatory fallback. Outside a
// plural, `#` is a literal. There is no ICU apostrophe-quoting: `'` is always
// literal, and a literal `{`/`}` cannot be written.
package messageformat

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/text/currency"
	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// keywordForm maps an ICU plural keyword to its CLDR form.
var keywordForm = map[string]plural.Form{
	"zero":  plural.Zero,
	"one":   plural.One,
	"two":   plural.Two,
	"few":   plural.Few,
	"many":  plural.Many,
	"other": plural.Other,
}

type node interface{}

type textNode string // literal text
type argNode string  // {name}
type hashNode struct{}

type pluralCase struct {
	exact   *int        // non-nil for an "=N" selector
	keyword plural.Form // used when exact == nil
	body    []node
}

type pluralNode struct {
	arg   string
	cases []pluralCase
}

// Validate reports whether msg is syntactically renderable, without supplying
// the arguments it references. Startup checks over a catalogue need exactly
// this: Render errs on a name its args do not carry, so rendering every
// message with none would reject every legitimate {name} message.
func Validate(msg string) error {
	runes := []rune(msg)
	_, pos, err := parse(runes, 0)
	if err != nil {
		return err
	}
	if pos != len(runes) {
		return fmt.Errorf("unexpected %q at %d", string(runes[pos]), pos)
	}
	return nil
}

// Render expands MessageFormat-subset `msg` for `tag`, drawing values from
// `args`. It errors on malformed input or a name the message references but
// `args` does not supply — surfacing template/data mismatches rather than
// emitting a half-rendered string.
func Render(tag language.Tag, msg string, args map[string]any) (string, error) {
	runes := []rune(msg)
	nodes, pos, err := parse(runes, 0)
	if err != nil {
		return "", err
	}
	if pos != len(runes) {
		return "", fmt.Errorf("i18n: unbalanced '}' at position %d", pos)
	}
	var b strings.Builder
	if err := renderNodes(nodes, args, tag, message.NewPrinter(tag), nil, &b); err != nil {
		return "", err
	}
	return b.String(), nil
}

// parse reads message content until it reaches a '}' it did not open (returning
// its index, unconsumed) or the end of input. Inner placeholders are consumed
// whole via recursion, so only the brace closing the caller's case stops it.
func parse(s []rune, i int) ([]node, int, error) {
	var nodes []node
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			nodes = append(nodes, textNode(text.String()))
			text.Reset()
		}
	}
	for i < len(s) {
		switch s[i] {
		case '}':
			flush()
			return nodes, i, nil
		case '#':
			flush()
			nodes = append(nodes, hashNode{})
			i++
		case '{':
			flush()
			n, ni, err := parsePlaceholder(s, i)
			if err != nil {
				return nil, 0, err
			}
			nodes = append(nodes, n)
			i = ni
		default:
			text.WriteRune(s[i])
			i++
		}
	}
	flush()
	return nodes, i, nil
}

// parsePlaceholder parses a `{…}` starting at s[i]=='{', returning the node and
// the index just past the closing '}'.
func parsePlaceholder(s []rune, i int) (node, int, error) {
	i++ // consume '{'
	i = skipWS(s, i)
	name, i := readName(s, i)
	if name == "" {
		return nil, 0, fmt.Errorf("i18n: empty argument name at position %d", i)
	}
	i = skipWS(s, i)
	if i >= len(s) {
		return nil, 0, fmt.Errorf("i18n: unterminated '{' for %q", name)
	}
	if s[i] == '}' {
		return argNode(name), i + 1, nil
	}
	if s[i] != ',' {
		return nil, 0, fmt.Errorf("i18n: expected ',' or '}' after %q", name)
	}
	i = skipWS(s, i+1)
	kind, i := readName(s, i)
	if kind != "plural" {
		return nil, 0, fmt.Errorf("i18n: unsupported format %q for %q (only 'plural')", kind, name)
	}
	i = skipWS(s, i)
	if i >= len(s) || s[i] != ',' {
		return nil, 0, fmt.Errorf("i18n: expected ',' after 'plural' for %q", name)
	}
	cases, i, err := parsePluralCases(s, i+1)
	if err != nil {
		return nil, 0, err
	}
	if i >= len(s) || s[i] != '}' {
		return nil, 0, fmt.Errorf("i18n: unterminated plural for %q", name)
	}
	return pluralNode{arg: name, cases: cases}, i + 1, nil
}

// parsePluralCases reads selector/body pairs until the plural's closing '}'.
func parsePluralCases(s []rune, i int) ([]pluralCase, int, error) {
	var cases []pluralCase
	for {
		i = skipWS(s, i)
		if i >= len(s) {
			return nil, 0, fmt.Errorf("i18n: unterminated plural cases")
		}
		if s[i] == '}' {
			if len(cases) == 0 {
				return nil, 0, fmt.Errorf("i18n: plural has no cases")
			}
			return cases, i, nil
		}
		var c pluralCase
		if s[i] == '=' {
			num, ni, err := readInt(s, i+1)
			if err != nil {
				return nil, 0, err
			}
			c.exact = &num
			i = ni
		} else {
			kw, ni := readName(s, i)
			form, ok := keywordForm[kw]
			if !ok {
				return nil, 0, fmt.Errorf("i18n: unknown plural keyword %q", kw)
			}
			c.keyword = form
			i = ni
		}
		i = skipWS(s, i)
		if i >= len(s) || s[i] != '{' {
			return nil, 0, fmt.Errorf("i18n: expected '{' to open a plural case body")
		}
		body, ni, err := parse(s, i+1)
		if err != nil {
			return nil, 0, err
		}
		if ni >= len(s) || s[ni] != '}' {
			return nil, 0, fmt.Errorf("i18n: unterminated plural case body")
		}
		c.body = body
		i = ni + 1
		cases = append(cases, c)
	}
}

func renderNodes(nodes []node, args map[string]any, tag language.Tag, p *message.Printer, count *int, b *strings.Builder) error {
	for _, n := range nodes {
		switch v := n.(type) {
		case textNode:
			b.WriteString(string(v))
		case hashNode:
			// `#` is the enclosing plural's count; a literal outside any plural.
			if count == nil {
				b.WriteByte('#')
				continue
			}
			b.WriteString(p.Sprintf("%d", *count))
		case argNode:
			val, ok := args[string(v)]
			if !ok {
				return fmt.Errorf("i18n: missing argument %q", string(v))
			}
			b.WriteString(formatArg(p, val))
		case pluralNode:
			raw, ok := args[v.arg]
			if !ok {
				return fmt.Errorf("i18n: missing plural argument %q", v.arg)
			}
			// Counts are always int (e.g. len(...)); reject anything else loudly.
			c, ok := raw.(int)
			if !ok {
				return fmt.Errorf("i18n: plural argument %q must be an int, got %T", v.arg, raw)
			}
			body, err := selectCase(v.cases, tag, c)
			if err != nil {
				return err
			}
			if err := renderNodes(body, args, tag, p, &c, b); err != nil {
				return err
			}
		}
	}
	return nil
}

// selectCase picks the plural case for count `c`: an exact "=N" match first,
// then the CLDR keyword category, then the mandatory "other" fallback.
func selectCase(cases []pluralCase, tag language.Tag, c int) ([]node, error) {
	for _, pc := range cases {
		if pc.exact != nil && *pc.exact == c {
			return pc.body, nil
		}
	}
	form := plural.Cardinal.MatchPlural(tag, c, 0, 0, 0, 0)
	var other []node
	haveOther := false
	for _, pc := range cases {
		if pc.exact != nil {
			continue
		}
		if pc.keyword == form {
			return pc.body, nil
		}
		if pc.keyword == plural.Other {
			other, haveOther = pc.body, true
		}
	}
	if haveOther {
		return other, nil
	}
	return nil, fmt.Errorf("i18n: no plural case for form %v and no 'other' fallback", form)
}

func formatArg(p *message.Printer, v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return p.Sprintf("%d", x)
	case currency.Amount:
		// An amount-currency pair renders as a locale-formatted, symbol-prefixed
		// string with grouping and the currency's own fraction-digit count (the
		// printer's tag drives the locale) — mirroring the frontend's
		// Intl.NumberFormat(style:'currency').
		return p.Sprint(currency.Symbol(x))
	case Alternative:
		// A text render; an HTML one has had EscapeArgs take the HTML.
		return x.Text
	default:
		return fmt.Sprintf("%v", v)
	}
}

func skipWS(s []rune, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

// readName reads an [A-Za-z0-9_] token (argument names, the "plural" keyword,
// the plural category keywords).
func readName(s []rune, i int) (string, int) {
	start := i
	for i < len(s) {
		c := s[i]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			i++
			continue
		}
		break
	}
	return string(s[start:i]), i
}

func readInt(s []rune, i int) (int, int, error) {
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return 0, 0, fmt.Errorf("i18n: expected a number after '='")
	}
	n, err := strconv.Atoi(string(s[start:i]))
	if err != nil {
		return 0, 0, err
	}
	return n, i, nil
}
