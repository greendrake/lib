package translate

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"greendrake.nz/go/translate/format"
)

// A text's arguments on the wire: what the caller supplies, and what a span
// protects.
//
// A printf conversion — %[n$][flags][width][.precision][length]conversion —
// crosses as the engine's own placeholder {pN}, N being the argument it
// formats: its stated position, or its place among the text's conversions.
// The model moves, repeats or, where a form's licence allows, leaves out the
// placeholder, and never sees the conversion it stands for, which is put back
// as the source wrote it. A `%%` crosses as the bare `%` it renders as, and an
// iOS %#@name@ substitution as the placeholder {name}, checked back by
// identity.
//
// An <xliff:g> span is what a string resource protects from translation: an
// argument with its example, or a literal like `km` or `Gmail`. It crosses
// whole as one placeholder — {id}, after its id — checked back by identity, and
// nothing in it reaches the model.
//
// A translation whose arguments come in the source's order comes back as the
// source wrote it. Any other — reordered, repeated, or with one left out — comes
// back with every conversion positional, %2$@ … %1$@, which both platforms
// read: a span's conversions with the rest, the span otherwise as written. No
// string comes back mixing the two.

// printfTokens match what starts at a `%`: a literal percent, a substitution,
// the argument of a String Catalog substitution's own forms (%arg), a
// conversion. Plain text is read with flags no prose writes after a percent
// sign; a format string with every flag both platforms read, the space and
// Java's `(` among them — read with those, "100% secure" would be a
// conversion.
var printfTokens = map[bool]*regexp.Regexp{
	false: printfToken(`-+#0',`),
	true:  printfToken(`-+#0', (`),
}

func printfToken(flags string) *regexp.Regexp {
	return regexp.MustCompile(`^%(?:(%)|(#@[A-Za-z0-9_]+@)|arg|(?:([1-9][0-9]*)\$)?[` + regexp.QuoteMeta(flags) + `]*[0-9]*(?:\.[0-9]+)?(?:hh|ll|[hlqztjL])?[@diuoxXfFeEgGaAcCsSp])`)
}

var (
	// protectedSpan matches an <xliff:g> span, and idAttribute its id.
	protectedSpan = regexp.MustCompile(`(?s)<xliff:g(?:\s[^<>]*)?>.*?</xliff:g>`)
	idAttribute   = regexp.MustCompile(`\sid\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	identifier    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	// wirePlaceholder matches any placeholder the wire may carry.
	wirePlaceholder = regexp.MustCompile(`\{[A-Za-z_][A-Za-z0-9_]*\}`)
)

// conversion is one printf conversion as the source wrote it.
type conversion struct {
	text       string
	arg        int
	positional bool
}

// positionalForm is the conversion with its argument stated. A substitution's
// own %arg has no position to state: it is always the one argument.
func (c conversion) positionalForm() string {
	if c.positional || c.text == "%arg" {
		return c.text
	}
	return "%" + strconv.Itoa(c.arg) + "$" + c.text[1:]
}

// segment is a run of a text: literal text, a substitution as written, or a
// conversion.
type segment struct {
	literal      string
	substitution bool
	conv         *conversion
}

// render writes segments back, every conversion positional or as written.
func render(segments []segment, positional bool) string {
	var out strings.Builder
	for _, s := range segments {
		switch {
		case s.conv == nil:
			out.WriteString(s.literal)
		case positional:
			out.WriteString(s.conv.positionalForm())
		default:
			out.WriteString(s.conv.text)
		}
	}
	return out.String()
}

// args are the arguments segments format, in order.
func args(segments []segment) []int {
	var out []int
	for _, s := range segments {
		if s.conv != nil {
			out = append(out, s.conv.arg)
		}
	}
	return out
}

// arguments is a text's argument layer as it crosses.
type arguments struct {
	// format says the text is a printf format string — it has a conversion, a
	// `%%` or a substitution — whose literal percent is written `%%`.
	format bool
	// sequence is every argument in the order the text formats them, a span's
	// conversions at the span's place.
	sequence []int
	// first is each argument's first conversion, bare is each one's first
	// outside a span.
	first, bare map[int]conversion
	// spans are the protected spans, cut at their conversions, by the
	// placeholder each crosses as.
	spans map[string][]segment
	// substitutions are the substitutions by placeholder.
	substitutions map[string]string
}

// tokens are the placeholders the wire checks back by identity.
func (a arguments) tokens() []string {
	out := append(slices.Collect(maps.Keys(a.spans)), slices.Collect(maps.Keys(a.substitutions))...)
	slices.Sort(out)
	return out
}

// conversionPlaceholder is how argument n crosses.
func conversionPlaceholder(n int) string { return "{p" + strconv.Itoa(n) + "}" }

// hideArguments is text with its spans — in tagged text — and its printf
// arguments hidden, and the layer that puts them back.
func hideArguments(unit format.Unit, text string) (string, arguments, error) {
	a := arguments{first: map[int]conversion{}, bare: map[int]conversion{}, spans: map[string][]segment{}, substitutions: map[string]string{}}
	failed := func(err error) (string, arguments, error) { return "", arguments{}, fmt.Errorf("%s: %w", unit.ID, err) }
	printf := unit.Checks.Syntax == format.SyntaxPrintf
	var bounds [][]int
	if hidesAttributes(unit) {
		bounds = protectedSpan.FindAllStringIndex(text, -1)
	}
	// The text's stretches outside a span and its spans whole, each cut at
	// its printf tokens; numbered once every conversion is known. A text found
	// to be a format string is cut again with every flag a format string takes.
	type stretch struct {
		segments []segment
		span     string
	}
	var stretches []stretch
	var conversions []*conversion
	stray := ""
	cutAll := func(format bool) {
		stretches, conversions, stray = nil, nil, ""
		cut := func(s, span string) {
			segments, convs, formatted, found := cutPrintf(s, printf, span != "", printfTokens[format])
			a.format = a.format || formatted
			if stray == "" {
				stray = found
			}
			conversions = append(conversions, convs...)
			stretches = append(stretches, stretch{segments: segments, span: span})
		}
		end := 0
		for _, at := range bounds {
			cut(text[end:at[0]], "")
			cut(text[at[0]:at[1]], text[at[0]:at[1]])
			end = at[1]
		}
		cut(text[end:], "")
	}
	cutAll(false)
	if a.format {
		cutAll(true)
	}
	if a.format && stray != "" {
		return failed(fmt.Errorf("%q is a `%%` that starts no conversion in a format string — a literal percent is written %%%%", stray))
	}
	if err := number(conversions); err != nil {
		return failed(err)
	}

	// What each placeholder stands for, so that no two things cross as one
	// and nothing the source writes literally is taken for one.
	stands := map[string]string{}
	claim := func(token, what, identity string) error {
		if strings.Contains(text, token) {
			return fmt.Errorf("it carries both %s and the placeholder %s it crosses the wire as", what, token)
		}
		if other, ok := stands[token]; ok && other != identity {
			return fmt.Errorf("it carries %s and %s, which would cross the wire as one placeholder %s", other, identity, token)
		}
		stands[token] = identity
		return nil
	}
	firstOf := func(m map[int]conversion, c conversion) {
		if _, ok := m[c.arg]; !ok {
			m[c.arg] = c
		}
	}
	var wire strings.Builder
	unnamed := 0
	for _, r := range stretches {
		if r.span != "" {
			// A span is the same span again only where it formats the same
			// arguments: the same text numbered as two arguments is two spans
			// one placeholder cannot restore.
			token := spanPlaceholder(r.span, &unnamed)
			identity := r.span
			if formatted := args(r.segments); len(formatted) > 0 {
				identity += fmt.Sprintf(" (argument %s)", strings.Trim(fmt.Sprint(formatted), "[]"))
			}
			if err := claim(token, r.span, identity); err != nil {
				return failed(err)
			}
			a.spans[token] = r.segments
			a.sequence = append(a.sequence, args(r.segments)...)
			for _, s := range r.segments {
				if s.conv != nil {
					firstOf(a.first, *s.conv)
				}
			}
			wire.WriteString(token)
			continue
		}
		for _, s := range r.segments {
			switch {
			case s.substitution:
				token := "{" + strings.Trim(s.literal[2:], "@") + "}"
				if err := claim(token, s.literal, s.literal); err != nil {
					return failed(err)
				}
				a.substitutions[token] = s.literal
				wire.WriteString(token)
			case s.conv != nil:
				token := conversionPlaceholder(s.conv.arg)
				if err := claim(token, s.conv.text, fmt.Sprintf("argument %d", s.conv.arg)); err != nil {
					return failed(err)
				}
				a.sequence = append(a.sequence, s.conv.arg)
				firstOf(a.first, *s.conv)
				firstOf(a.bare, *s.conv)
				wire.WriteString(token)
			default:
				wire.WriteString(s.literal)
			}
		}
	}
	return wire.String(), a, nil
}

// cutPrintf cuts a run of text at the printf tokens token matches. A literal
// percent becomes the `%` it renders as, except in a span, which the engine
// renders nothing of; a substitution and a conversion become segments of their
// own, a substitution unnumbered. It reports whether the run is written as a
// format string, and the first `%` in it that starts nothing.
func cutPrintf(text string, printf, span bool, token *regexp.Regexp) (segments []segment, convs []*conversion, formatted bool, stray string) {
	if !printf {
		return []segment{{literal: text}}, nil, false, ""
	}
	var literal strings.Builder
	flush := func() {
		if literal.Len() > 0 {
			segments = append(segments, segment{literal: literal.String()})
			literal.Reset()
		}
	}
	for i := 0; i < len(text); {
		if text[i] != '%' {
			literal.WriteByte(text[i])
			i++
			continue
		}
		match := token.FindStringSubmatchIndex(text[i:])
		if match == nil {
			if stray == "" {
				stray = strings.SplitN(text[i:], " ", 2)[0]
			}
			literal.WriteByte('%')
			i++
			continue
		}
		matched := text[i : i+match[1]]
		i += match[1]
		formatted = true
		switch {
		case match[2] >= 0 && span:
			literal.WriteString(matched)
		case match[2] >= 0:
			literal.WriteByte('%')
		case match[4] >= 0:
			flush()
			segments = append(segments, segment{literal: matched, substitution: true})
		default:
			flush()
			c := &conversion{text: matched}
			if match[6] >= 0 {
				c.arg, _ = strconv.Atoi(matched[match[6]:match[7]])
				c.positional = true
			}
			convs = append(convs, c)
			segments = append(segments, segment{conv: c})
		}
	}
	flush()
	return segments, convs, formatted, stray
}

// number gives each conversion that states no argument its place among them,
// refusing a text that mixes the two, as both platforms do.
func number(conversions []*conversion) error {
	positional := 0
	for _, c := range conversions {
		if c.positional {
			positional++
		}
	}
	if positional > 0 && positional < len(conversions) {
		return fmt.Errorf("its conversions mix stated positions with none — state them all, as %%1$@, or none")
	}
	for i, c := range conversions {
		if !c.positional {
			c.arg = i + 1
		}
	}
	return nil
}

// spanPlaceholder is the placeholder a span crosses as: its id, where that is
// a name, and xliff<N> for the Nth span without one.
func spanPlaceholder(original string, unnamed *int) string {
	open := original[:strings.Index(original, ">")]
	if m := idAttribute.FindStringSubmatch(open); m != nil {
		if id := m[1] + m[2]; identifier.MatchString(id) {
			return "{" + id + "}"
		}
	}
	*unnamed++
	return "{xliff" + strconv.Itoa(*unnamed) + "}"
}

// restore puts an answer's arguments back: positional where its arguments
// are not the source's sequence exactly.
func (a arguments) restore(answer string) string {
	if a.format {
		answer = strings.ReplaceAll(answer, "%", "%%")
	}
	positional := !slices.Equal(a.answered(answer), a.sequence)
	return wirePlaceholder.ReplaceAllStringFunc(answer, func(token string) string {
		if segments, ok := a.spans[token]; ok {
			return render(segments, positional)
		}
		if original, ok := a.substitutions[token]; ok {
			return original
		}
		if n, ok := a.argument(token); ok {
			if positional {
				return a.first[n].positionalForm()
			}
			if c, ok := a.bare[n]; ok {
				return c.text
			}
		}
		return token
	})
}

// answered is the sequence of arguments an answer formats.
func (a arguments) answered(answer string) []int {
	var out []int
	for _, token := range wirePlaceholder.FindAllString(answer, -1) {
		if segments, ok := a.spans[token]; ok {
			out = append(out, args(segments)...)
		} else if n, ok := a.argument(token); ok {
			out = append(out, n)
		}
	}
	return out
}

// argument is the source's argument a {pN} placeholder stands for.
func (a arguments) argument(token string) (int, bool) {
	digits, ok := strings.CutPrefix(strings.Trim(token, "{}"), "p")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0, false
	}
	_, known := a.first[n]
	return n, known
}

// kept refuses an answer that leaves out an argument and keeps a later one: a
// C-style formatter reads its arguments in order, knowing each one's type only
// from its conversion, so it cannot skip one it is never told about.
func (a arguments) kept(answer string) error {
	present := map[int]bool{}
	for _, n := range a.answered(answer) {
		present[n] = true
	}
	formatted := slices.Sorted(maps.Keys(a.first))
	for _, missing := range formatted {
		if present[missing] {
			continue
		}
		for _, later := range formatted {
			if later > missing && present[later] {
				return fmt.Errorf("%s is left out while %s is kept: arguments are read in order, so only the last may be left out — keep %s",
					conversionPlaceholder(missing), conversionPlaceholder(later), conversionPlaceholder(missing))
			}
		}
	}
	return nil
}
