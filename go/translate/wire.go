package translate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"greendrake.nz/go/i18n/plurals"
	"greendrake.nz/go/translate/format"
)

// How units cross the wire.
//
// A model translates language and keeps placeholders; what it does not do
// reliably is restructure — produce the four forms of a Russian plural from the
// English two, in either plural syntax. Shown the English pair it writes a
// pair back, whatever it is told. So a plural is never asked for whole. A
// vue-i18n plural, and a plural its source keys by category, are asked for one
// form at a time, each with the numbers that select it; an ICU plural's
// sentence is asked with the plural as an opaque placeholder and its cases one
// at a time; and the pipeline puts the forms back in the renderer's own syntax,
// or keyed by the target's categories. The unit's text, digest and file keep
// that syntax throughout; only the prompt and the answer carry forms.

// wireItem is one thing the model is asked for.
type wireItem struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Context string `json:"context,omitempty"`
}

// answer is what came back for one request: the texts returned under each id
// asked for. A slice per id because one may come back twice — the schema pins
// every id to one that was asked for, so a model that would have mangled an id
// is forced onto a legal one instead, and that can be another item's. Two
// texts that agree are one answer given twice; two that differ mean at least
// one of them belongs to a different string, which is unknowable, so neither
// is used.
type answer struct {
	texts map[string][]string
	// written are the same texts as the model wrote them — JSON strings, quoted
	// and escaped as they came back — which is how a retry and a failure
	// message quote them: a text of several lines stays one quotation, and a
	// text spelled in escapes shows what was wrong with it, where decoded it
	// would show nothing.
	written map[string][]string
	// truncated says the completion was cut off at the cap: the items it holds
	// are whole, and every other it never finished. cut is the one it was
	// writing when it was cut, where the cut came after its id.
	truncated bool
	cut       *cutItem
}

// cutItem is the item a completion was writing when it was cut off: its id,
// and its text as far as the model wrote it — what a model looping inside a
// string looped on.
type cutItem struct {
	id      string
	written string
}

// cutQuoteMax bounds what a complaint quotes of a cut-off text: enough to show
// the loop, not the cap's worth of it.
const cutQuoteMax = 200

// answerOf reads a completion's items, keeping each text as it was written.
func answerOf(items []completionItem) (answer, error) {
	out := answer{texts: map[string][]string{}, written: map[string][]string{}}
	for _, item := range items {
		var text string
		if err := json.Unmarshal(item.Text, &text); err != nil {
			return answer{}, err
		}
		out.texts[item.ID] = append(out.texts[item.ID], text)
		out.written[item.ID] = append(out.written[item.ID], string(item.Text))
	}
	return out, nil
}

// cutAnswerOf reads a completion cut off at the cap, as the grammar has every
// completion written — `{"items": [{"id": …, "text": …}, …` — as far as it got:
// every item it finished, and the one it was cut off writing. False when it
// holds nothing to keep — no item finished, and none whose id it got to —
// which leaves no part of the request to keep and no one item to blame.
func cutAnswerOf(data json.RawMessage) (answer, bool, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	for _, want := range []json.Token{json.Delim('{'), "items", json.Delim('[')} {
		if got, err := dec.Token(); err != nil || got != want {
			return answer{}, false, nil
		}
	}
	var items []completionItem
	var cut *cutItem
	for dec.More() {
		start := dec.InputOffset()
		var item completionItem
		if err := dec.Decode(&item); err != nil {
			cut = cutItemOf(bytes.TrimLeft(data[start:], ", "))
			break
		}
		items = append(items, item)
	}
	if len(items) == 0 && cut == nil {
		return answer{}, false, nil
	}
	out, err := answerOf(items)
	if err != nil {
		return answer{}, false, err
	}
	out.truncated, out.cut = true, cut
	return out, true, nil
}

// cutItemOf reads the item a completion was cut off writing, from its opening
// brace: nil when the cut came before its id was whole.
func cutItemOf(item []byte) *cutItem {
	dec := json.NewDecoder(bytes.NewReader(item))
	var tokens []json.Token
	for len(tokens) < 4 {
		token, err := dec.Token()
		if err != nil {
			break
		}
		tokens = append(tokens, token)
	}
	if len(tokens) < 3 || tokens[0] != json.Delim('{') || tokens[1] != "id" {
		return nil
	}
	id, ok := tokens[2].(string)
	if !ok {
		return nil
	}
	cut := &cutItem{id: id}
	if len(tokens) == 4 {
		written := strings.TrimLeft(string(item[dec.InputOffset():]), ": ")
		if len(written) > cutQuoteMax {
			written = strings.ToValidUTF8(written[:cutQuoteMax], "") + "…"
		}
		cut.written = written
	}
	return cut
}

func (a answer) get(id string) (string, error) {
	texts := a.texts[id]
	if len(texts) == 0 {
		return "", fmt.Errorf("no item with id %s in your answer", id)
	}
	if slices.ContainsFunc(a.written[id], spelledInEscapes) {
		return "", fmt.Errorf("the text of %s is spelled in \\u escapes: write every character as itself", id)
	}
	for _, text := range texts[1:] {
		if text != texts[0] {
			return "", fmt.Errorf("your answer has %d items with id %s and they differ; send one", len(texts), id)
		}
	}
	return texts[0], nil
}

// spelledInEscapes reports a JSON string, as written, that spells a printable
// character as a \u escape. A model that drifts into ASCII-only JSON — as much
// of what it learned from is — writes a script like Greek at several times the
// tokens, loses its fluency doing so, and runs into the cap mid-string; what
// JSON itself requires escaping is below U+0020, or \" and \\.
func spelledInEscapes(raw string) bool {
	for i := 0; i+1 < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		if raw[i+1] == 'u' && i+6 <= len(raw) {
			if code, err := strconv.ParseUint(string(raw[i+2:i+6]), 16, 32); err == nil && code >= 0x20 {
				return true
			}
		}
		i++ // past the escaped character, so `\\u` is a backslash and a u
	}
	return false
}

// formSeparator joins a unit id to the category of the form asked for:
// ui:landing.unread_nudge@few.
const formSeparator = "@"

// renderingSeparator joins an item's id to the number of the rendering asked
// for, where a retry asks for several: store:listing.subtitle#2.
const renderingSeparator = "#"

// renderingSuffix is what the kth rendering's ids end in.
func renderingSuffix(k int) string { return renderingSeparator + strconv.Itoa(k) }

// numbered is a unit's items asked for n times over, each time under ids
// numbered for it — the whole unit per rendering, so a plural's forms of one
// rendering are asked for together. Asked for once, the items keep their ids.
func numbered(items []wireItem, n int) []wireItem {
	if n == 1 {
		return items
	}
	out := make([]wireItem, 0, n*len(items))
	for k := 1; k <= n; k++ {
		for _, item := range items {
			item.ID += renderingSuffix(k)
			out = append(out, item)
		}
	}
	return out
}

// rendering is the kth of the renderings asked for, under the ids its items
// would have had asked for once. Every id of an answer to several renderings
// carries the one suffix numbering it, so cutting that suffix is
// unambiguous, whatever else an id carries.
func (a answer) rendering(k int) answer {
	suffix := renderingSuffix(k)
	out := answer{texts: map[string][]string{}, written: map[string][]string{}}
	for id, texts := range a.texts {
		if base, ok := strings.CutSuffix(id, suffix); ok {
			out.texts[base] = texts
			out.written[base] = a.written[id]
		}
	}
	return out
}

// writing is how a locale writes, as far as its translations are held to it:
// what its plurals need on the wire — its categories in order, for each the
// numbers that select it, and for those a single number selects, that
// number — and the letters it is written in.
type writing struct {
	categories []string
	examples   map[string][]string
	exact      map[string]string
	scripts    []*unicode.RangeTable
}

func writingOf(locale string) (writing, error) {
	scripts, err := scriptsOf(locale)
	return writing{
		categories: plurals.Categories(locale),
		examples:   plurals.Examples(locale),
		exact:      plurals.Exact(locale),
		scripts:    scripts,
	}, err
}

// exactNote is the licence a form carries when a single count selects its
// category: the number is a constant there, which is why the singular and dual
// forms of several languages write it as a word or not at all. Empty for a
// category many numbers select, where the placeholder has to survive.
func (f writing) exactNote(category, licence string) string {
	n, ok := f.exact[category]
	if !ok {
		return ""
	}
	return fmt.Sprintf(" It renders for %s and no other number, so %s.", n, licence)
}

// selects says which numbers select a category, the way a translator is told.
func (f writing) selects(category string) string {
	examples := f.examples[category]
	switch len(examples) {
	case 0:
		return "any other number"
	case 1:
		return examples[0]
	}
	return strings.Join(examples[:len(examples)-1], ", ") + " or " + examples[len(examples)-1]
}

// What a translation must carry over untouched crosses the wire in a short,
// uniform form, because copying a long opaque token is what a model gets wrong.
//
// Two kinds of it. A reference — vue-i18n's `@:some.key`, which the renderer
// resolves to another message — is the one piece of a renderer's syntax a
// model half-recognises: it reads `@:brand.name` as a placeholder by another
// name and writes `{site_name}` in its place, so it crosses as the placeholder
// `{brand_name}`, covered by the rule the model follows best. And a tag's
// attributes are a line of CSS or a URL with nothing in them to translate,
// which a model nonetheless has to reproduce character for character, and
// duplicates a clause of now and then; they cross as one bare attribute naming
// the element, `<a attr1>`. That keeps the tag a plain HTML tag, which a model
// reproduces as markup, where `<a {attr1}>` reads as one more placeholder in
// running text: the sentence carries on straight after it and the tag's `>` is
// dropped. The renderer's syntax is restored on the way back, as it is for
// a plural. The tokens — the reference placeholders, and each tag as it
// crosses — are the engine's own encoding, so the engine checks them back by
// identity.

// linkPlaceholder is how a reference is written on the wire.
func linkPlaceholder(link string) string {
	return "{" + strings.ReplaceAll(strings.TrimPrefix(link, "@:"), ".", "_") + "}"
}

// attrMarker is the attribute a tag's own cross the wire as.
func attrMarker(n int) string { return fmt.Sprintf("attr%d", n) }

// strayMarker matches an attribute marker; written anywhere but in its tag, it
// is restored nowhere and reaches the reader as a word.
var strayMarker = regexp.MustCompile(`\battr\d+\b`)

// hiddenTag is a tag whose attributes the wire hides: as it crosses, and as
// the source wrote it.
type hiddenTag struct{ wire, original string }

// hideAttributes replaces each tag's attributes with its marker, and reports
// the tags so written, in the order they appear.
func hideAttributes(text string) (string, []hiddenTag) {
	var tags []hiddenTag
	var out strings.Builder
	end := 0
	for _, match := range htmlTag.FindAllStringSubmatchIndex(text, -1) {
		start, stop := match[2], match[3]
		if start < 0 || strings.TrimSpace(text[start:stop]) == "" {
			continue
		}
		wire := text[match[0]:start] + " " + attrMarker(len(tags)+1) + text[stop:match[1]]
		tags = append(tags, hiddenTag{wire: wire, original: text[match[0]:match[1]]})
		out.WriteString(text[end:match[0]])
		out.WriteString(wire)
		end = match[1]
	}
	out.WriteString(text[end:])
	return out.String(), tags
}

// hidesLinks and hidesAttributes say what a unit's syntax and markup have the
// wire hide; tagged text also hides its protected spans (arguments.go).
func hidesLinks(unit format.Unit) bool      { return unit.Checks.Syntax == format.SyntaxVueI18n }
func hidesAttributes(unit format.Unit) bool { return unit.Checks.Tagged() }

// crossing is one text on the wire: what the model is shown, the tokens the
// engine put in it — checked back by identity — its arguments, and how an
// answer in those terms is put back in the renderer's syntax.
type crossing struct {
	wire    string
	tokens  []string
	args    arguments
	restore func(answer string) string
}

// cross is one of a unit's texts as it crosses, the innermost syntax hidden
// first: its protected spans and arguments, its references, its tags'
// attributes. It comes back innermost last, and only what the source carries
// is put back, so a placeholder the source genuinely has keeps its braces. A
// source carrying both something hidden and the placeholder it is hidden
// behind could not be restored, so it fails its batch rather than shipping
// one of them as the other.
func cross(unit format.Unit, text string) (crossing, error) {
	wire, args, err := hideArguments(unit, text)
	if err != nil {
		return crossing{}, err
	}
	c := crossing{args: args, tokens: args.tokens()}
	var links []string
	if hidesLinks(unit) {
		links = linkedMessage.FindAllString(wire, -1)
		for _, link := range links {
			token := linkPlaceholder(link)
			if strings.Contains(text, token) {
				return crossing{}, fmt.Errorf("%s carries both %s and the placeholder %s it crosses the wire as", unit.ID, link, token)
			}
			c.tokens = append(c.tokens, token)
		}
		wire = linkedMessage.ReplaceAllStringFunc(wire, linkPlaceholder)
	}
	var pairs []string
	if hidesAttributes(unit) {
		var tags []hiddenTag
		wire, tags = hideAttributes(wire)
		for _, tag := range tags {
			c.tokens = append(c.tokens, tag.wire)
			pairs = append(pairs, tag.wire, tag.original)
		}
	}
	// In one pass: a tag the source wrote with an attrN of its own is another
	// tag's wire form, and must not be replaced again.
	attributes := strings.NewReplacer(pairs...)
	c.wire = wire
	c.restore = func(answer string) string {
		answer = attributes.Replace(answer)
		for _, link := range links {
			answer = strings.ReplaceAll(answer, linkPlaceholder(link), link)
		}
		return args.restore(answer)
	}
	return c, nil
}

// wired is a unit as the model is shown it, and how its text crossed: whole,
// or a keyed plural form by form.
type wired struct {
	unit  format.Unit
	whole crossing
	// forms are a keyed plural's crossings, by the source's categories.
	forms map[string]crossing
}

// wire is the unit as the model sees it.
func wire(unit format.Unit) (wired, error) {
	if unit.Forms == nil {
		c, err := cross(unit, unit.Text)
		if err != nil {
			return wired{}, err
		}
		unit.Text = c.wire
		return wired{unit: unit, whole: c}, nil
	}
	w := wired{forms: map[string]crossing{}}
	shown := map[string]string{}
	for category, form := range unit.Forms {
		c, err := cross(unit, form)
		if err != nil {
			return wired{}, fmt.Errorf("%w (the %s form)", err, category)
		}
		w.forms[category], shown[category] = c, c.wire
	}
	unit.Forms, unit.Text = shown, format.EncodeForms(shown)
	w.unit = unit
	return w, nil
}

// restore puts a translation in the wire's terms back in the renderer's
// syntax: a keyed plural's every form as the source's form it was shown.
func (w wired) restore(text string) (string, error) {
	if w.forms == nil {
		return w.whole.restore(text), nil
	}
	forms, err := format.DecodeForms(text)
	if err != nil {
		return "", err
	}
	for category, form := range forms {
		forms[category] = w.forms[shownCategory(w.forms, category)].restore(form)
	}
	return format.EncodeForms(forms), nil
}

// shownCategory is the source's category a target category's form is asked
// for against: the same category where the source has it, `other` — the one
// every count beyond the source's own distinctions takes — where it does not.
func shownCategory[V any](source map[string]V, category string) string {
	if _, ok := source[category]; ok {
		return category
	}
	return "other"
}

// pipePlural reports a vue-i18n plural: forms separated by `|`, selected by
// position.
func pipePlural(unit format.Unit) bool {
	return unit.Checks.Syntax == format.SyntaxVueI18n && strings.Contains(unit.Text, "|")
}

// icuPlural reports an ICU `{count, plural, …}`.
func icuPlural(unit format.Unit) bool {
	return unit.Checks.Syntax == format.SyntaxICU && strings.Contains(unit.Text, pluralMarker)
}

// wireItems is what one unit is asked for.
func wireItems(unit format.Unit, source, target writing) ([]wireItem, error) {
	w, err := wire(unit)
	if err != nil {
		return nil, err
	}
	unit = w.unit
	context := unit.Context
	if unit.Checks.Shape == format.ShapeLine {
		context = withNote(context, "One line.")
	}
	if unit.Checks.Limit > 0 {
		context = withNote(context, fmt.Sprintf("At most %d characters.", unit.Checks.Limit))
	}
	switch {
	case unit.Forms != nil:
		return formItems(unit, context, unit.Forms, target, "Write this form alone."), nil
	case pipePlural(unit):
		english := splitForms(unit.Text)
		if len(english) != len(source.categories) {
			return nil, fmt.Errorf("%s: the source plural has %d forms where its locale writes %d", unit.ID, len(english), len(source.categories))
		}
		forms := make(map[string]string, len(english))
		for i, category := range source.categories {
			forms[category] = english[i]
		}
		return formItems(unit, context, forms, target, "Write this form alone, without `|`."), nil
	case icuPlural(unit):
		p, ok := findPlural(unit.Text)
		if !ok || strings.Contains(unit.Text[p.end:], pluralMarker) {
			return nil, fmt.Errorf("%s: exactly one plural per message is supported", unit.ID)
		}
		cases, err := parseCases(p.inner)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", unit.ID, err)
		}
		bodies := map[string]string{}
		for _, c := range cases {
			bodies[c.keyword] = c.body
		}
		if _, ok := bodies["other"]; !ok {
			return nil, fmt.Errorf("%s: the plural has no other case", unit.ID)
		}
		// The placeholder stands for the plural in the sentence and for `#`
		// inside a case, so a source that already carries it anywhere could
		// not be restored.
		if strings.Contains(unit.Text[:p.start]+unit.Text[p.end:]+p.inner, opaque(p.arg)) {
			return nil, fmt.Errorf("%s carries both a plural on {%s} and a placeholder %s, which is how the plural crosses the wire", unit.ID, p.arg, opaque(p.arg))
		}
		sentence := unit.Text[:p.start] + opaque(p.arg) + unit.Text[p.end:]
		var items []wireItem
		// A sentence that is the plural and nothing else has nothing to ask
		// for: handed the placeholder alone, a model fills the plural in
		// rather than keeping it. Its frame is the source's own.
		if translatable(unit.Text[:p.start] + unit.Text[p.end:]) {
			items = append(items, wireItem{
				ID:      unit.ID,
				Text:    sentence,
				Context: withNote(context, opaque(p.arg)+" is a placeholder standing for the number and the words that agree with it: keep it exactly as written. Those words are separate items."),
			})
		}
		for _, category := range target.categories {
			body, ok := bodies[category]
			if !ok {
				body = bodies["other"]
			}
			items = append(items, wireItem{
				ID: unit.ID + formSeparator + category,
				// `#` is the number the renderer substitutes — one more thing
				// the model has to copy and therefore one more thing it
				// rewrites, expanding it into the argument's own name. It
				// crosses as that placeholder and comes back as `#`.
				Text: strings.ReplaceAll(body, "#", opaque(p.arg)),
				Context: withNote(context, fmt.Sprintf("One case of the plural %s in: %s — the form used when %s is %s (%s). Write this case's text alone, with %s where the number goes.%s",
					opaque(p.arg), sentence, opaque(p.arg), target.selects(category), category, opaque(p.arg),
					target.exactNote(category, fmt.Sprintf("write the number as a word in place of %s where that is what this language does", opaque(p.arg))))),
			})
		}
		return items, nil
	default:
		return []wireItem{{ID: unit.ID, Text: unit.Text, Context: context}}, nil
	}
}

// formItems asks for a plural one form per target category, each showing the
// source's form for it with the numbers that select it.
func formItems(unit format.Unit, context string, source map[string]string, target writing, alone string) []wireItem {
	items := make([]wireItem, 0, len(target.categories))
	for _, category := range target.categories {
		items = append(items, wireItem{
			ID:   unit.ID + formSeparator + category,
			Text: source[shownCategory(source, category)],
			Context: withNote(context, fmt.Sprintf("One form of a plural: the one used when the number is %s (%s). %s%s",
				target.selects(category), category, alone,
				target.exactNote(category, "write the number as a word, or leave it out, where that is what this language does — every other placeholder stays"))),
		})
	}
	return items
}

// withNote appends an instruction to a unit's context.
func withNote(context, note string) string {
	return strings.TrimSpace(context + "\n" + note)
}

// translatable reports whether a piece of text has language in it — a letter
// — as opposed to only placeholders, punctuation and space.
func translatable(text string) bool {
	return strings.ContainsFunc(text, unicode.IsLetter)
}

// assemble is a unit's translation from what came back, in the renderer's own
// syntax — or what is missing from the answer, said the way the model is told.
func assemble(unit format.Unit, rendered answer, target writing) (string, error) {
	form := func(category string) (string, error) {
		text, err := rendered.get(unit.ID + formSeparator + category)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(text), nil
	}
	switch {
	case unit.Forms != nil || pipePlural(unit):
		forms := make(map[string]string, len(target.categories))
		parts := make([]string, 0, len(target.categories))
		for _, category := range target.categories {
			text, err := form(category)
			if err != nil {
				return "", err
			}
			forms[category] = text
			parts = append(parts, text)
		}
		if unit.Forms != nil {
			return format.EncodeForms(forms), nil
		}
		return strings.Join(parts, " | "), nil
	case icuPlural(unit):
		src, _ := findPlural(unit.Text)
		// The frame around the plural: the answer's where one was asked for,
		// the source's own where there was nothing in it to translate.
		before, after := unit.Text[:src.start], unit.Text[src.end:]
		if translatable(before + after) {
			sentence, err := rendered.get(unit.ID)
			if err != nil {
				return "", err
			}
			token := opaque(src.arg)
			at := strings.Index(sentence, token)
			if at < 0 {
				return "", fmt.Errorf("no %s in your answer: it stands for the number and the words that agree with it, and must appear exactly as written", token)
			}
			before, after = sentence[:at], sentence[at+len(token):]
		}
		cases := make([]string, 0, len(target.categories))
		for _, category := range target.categories {
			text, err := form(category)
			if err != nil {
				return "", err
			}
			cases = append(cases, category+" {"+strings.ReplaceAll(text, opaque(src.arg), "#")+"}")
		}
		// The argument is the source's whatever the answer calls it: syntax
		// the pipeline owns, like the keywords.
		return before + "{" + src.arg + ", plural, " + strings.Join(cases, " ") + "}" + after, nil
	default:
		return rendered.get(unit.ID)
	}
}

// --- the ICU plural, located and taken apart ---

const pluralMarker = ", plural,"

// opaque is a sentence's plural as the model sees it: an ordinary placeholder,
// standing for the number and the words that agree with it. Written with its
// cases elided — `{n, plural, …}` — it invites the model to tidy it back into
// the familiar `{n}`, which is the one thing it must not do to it.
func opaque(arg string) string { return "{" + arg + "}" }

// pluralSpan locates one ICU plural in a message: text[start:end] is the whole
// `{arg, plural, …}`, and inner is what sits between the marker and its
// closing brace.
type pluralSpan struct {
	start, end int
	arg, inner string
}

// findPlural locates the first ICU plural of a message.
func findPlural(text string) (pluralSpan, bool) {
	marker := strings.Index(text, pluralMarker)
	if marker < 0 {
		return pluralSpan{}, false
	}
	open := strings.LastIndex(text[:marker], "{")
	if open < 0 {
		return pluralSpan{}, false
	}
	end := matchBrace(text, open)
	if end < 0 {
		return pluralSpan{}, false
	}
	return pluralSpan{
		start: open,
		end:   end + 1,
		arg:   strings.TrimSpace(text[open+1 : marker]),
		inner: strings.TrimSpace(text[marker+len(pluralMarker) : end]),
	}, true
}

// matchBrace returns the index of the brace closing the one at open, or -1.
// Byte indexes are safe here: no multi-byte UTF-8 sequence contains an ASCII
// byte, so a brace found by byte is a brace.
func matchBrace(text string, open int) int {
	depth := 0
	for i := open; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// pluralCase is one `keyword {body}` of an ICU plural.
type pluralCase struct{ keyword, body string }

// parseCases reads the cases of a plural in order. Exact `=N` cases have no
// form to ask for and are refused.
func parseCases(inner string) ([]pluralCase, error) {
	var cases []pluralCase
	rest := inner
	for {
		rest = strings.TrimLeft(rest, " \t\n")
		if rest == "" {
			if len(cases) == 0 {
				return nil, fmt.Errorf("the plural has no cases")
			}
			return cases, nil
		}
		open := strings.Index(rest, "{")
		if open < 0 {
			return nil, fmt.Errorf("case %q has no body", rest)
		}
		keyword := strings.TrimSpace(rest[:open])
		if strings.HasPrefix(keyword, "=") {
			return nil, fmt.Errorf("an =N case cannot be asked for as a form")
		}
		if !pluralKeyword.MatchString(" " + keyword + " {") {
			return nil, fmt.Errorf("unknown plural keyword %q", keyword)
		}
		end := matchBrace(rest, open)
		if end < 0 {
			return nil, fmt.Errorf("unterminated case %q", keyword)
		}
		cases = append(cases, pluralCase{keyword: keyword, body: rest[open+1 : end]})
		rest = rest[end+1:]
	}
}
