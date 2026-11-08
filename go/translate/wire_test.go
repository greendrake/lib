package translate

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"greendrake.nz/go/translate/format"
)

var enForms, ruForms = writingFor("en-GB"), writingFor("ru")

// answered is a model's answer with one text per id.
func answered(items map[string]string) answer {
	out := answer{texts: map[string][]string{}}
	for id, text := range items {
		out.texts[id] = []string{text}
	}
	return out
}

// spelled writes each character of s as a \u escape, the way a model drifted
// into ASCII-only JSON does.
func spelled(s string) string {
	var b strings.Builder
	for _, r := range s {
		fmt.Fprintf(&b, `\u%04x`, r)
	}
	return b.String()
}

// A text spelled in \u escapes is told apart from one written as itself by
// how it was written, not by what it decodes to: JSON's own escapes — the
// control characters, \" and \\ — are writing as itself.
func TestSpelledInEscapes(t *testing.T) {
	for raw, want := range map[string]bool{
		`"Έξετε"`:                        false,
		`"` + spelled("Έξε") + `"`:       true,
		`"a ` + spelled("<") + `b> tag"`: true,
		`"` + spelled("C") + `ancel"`:    true,
		`"say \"hi\"\nand\tgo \\ x"`:     false,
		`"\u0009tab"`:                    false,
		`"a \\u0388 literally"`:          false,
		`"cut off \u03"`:                 false,
	} {
		assert.Equal(t, want, spelledInEscapes(raw), raw)
	}
}

// An item whose text came back spelled in escapes is refused by name, with
// what to do instead — which is what the retry is told.
func TestAnswer_RefusesATextSpelledInEscapes(t *testing.T) {
	rendered, err := answerOf([]completionItem{
		{ID: "ui:a.b", Text: json.RawMessage(`"` + spelled("Έξε") + `"`)},
		{ID: "ui:a.c", Text: json.RawMessage(`"Έξε"`)},
	})
	require.NoError(t, err)
	_, err = rendered.get("ui:a.b")
	assert.EqualError(t, err, `the text of ui:a.b is spelled in \u escapes: write every character as itself`)
	text, err := rendered.get("ui:a.c")
	require.NoError(t, err)
	assert.Equal(t, "Έξε", text)

	// Quoted in a retry or a failure message, a text is shown as it was
	// written: the escaped one decoded would look like nothing was wrong.
	assert.Equal(t, []string{`"` + spelled("Έξε") + `"`}, rendered.written["ui:a.b"])
	assert.Equal(t, []string{`"Έξε"`}, rendered.written["ui:a.c"])
}

// A retry asks for each unit whole, rendering by rendering, and reads each
// rendering back under the ids its items have when asked for once — an id
// that carries the separator of its own included.
func TestNumberedRenderings(t *testing.T) {
	items := []wireItem{{ID: "content:/FAQ#03e8", Text: "a"}, {ID: "ui:a.n@one", Text: "b"}}
	assert.Equal(t, items, numbered(items, 1), "asked for once, an item keeps its id")

	var ids []string
	var answered []completionItem
	for _, item := range numbered(items, 2) {
		ids = append(ids, item.ID)
		answered = append(answered, completionItem{ID: item.ID, Text: json.RawMessage(`"` + item.ID + `"`)})
	}
	assert.Equal(t, []string{"content:/FAQ#03e8#1", "ui:a.n@one#1", "content:/FAQ#03e8#2", "ui:a.n@one#2"}, ids)

	rendered, err := answerOf(answered)
	require.NoError(t, err)
	second := rendered.rendering(2)
	assert.Equal(t, map[string][]string{"content:/FAQ#03e8": {"content:/FAQ#03e8#2"}, "ui:a.n@one": {"ui:a.n@one#2"}}, second.texts)
	assert.Equal(t, []string{`"ui:a.n@one#2"`}, second.written["ui:a.n@one"])
}

func withID(unit format.Unit, id string) format.Unit {
	unit.ID = id
	return unit
}

// wiredOf is a unit as it crosses.
func wiredOf(t *testing.T, unit format.Unit) wired {
	t.Helper()
	w, err := wire(unit)
	require.NoError(t, err)
	return w
}

// restoredOf is an answer put back in the renderer's syntax.
func restoredOf(t *testing.T, w wired, text string) string {
	t.Helper()
	restored, err := w.restore(text)
	require.NoError(t, err)
	return restored
}

// A vue-i18n plural is asked for one form at a time: the source form that
// stands for the category, with the numbers that select it.
func TestWireItems_PipePluralOneFormAtATime(t *testing.T) {
	unit := withID(unitOf(vueHTML, "Showing {found} result | Showing {found} results"), "ui:a.n")
	unit.Context = "Under the search box."
	items, err := wireItems(unit, enForms, ruForms)
	require.NoError(t, err)
	require.Len(t, items, 4)
	assert.Equal(t, "ui:a.n@one", items[0].ID)
	assert.Equal(t, "Showing {found} result", items[0].Text)
	assert.Contains(t, items[0].Context, "Under the search box.\nOne form of a plural: the one used when the number is 1 (one).")
	assert.Equal(t, "ui:a.n@few", items[1].ID)
	assert.Equal(t, "Showing {found} results", items[1].Text, "the source's other form stands for every category it lacks")
	assert.Contains(t, items[1].Context, "2, 3 or 4 (few)")
	assert.Contains(t, items[3].Context, "1.5, 2.5 or 3.5 (other)")

	got, err := assemble(unit, answered(map[string]string{
		"ui:a.n@one": "Показан {found} результат", "ui:a.n@few": "Показано {found} результата",
		"ui:a.n@many": "Показано {found} результатов", "ui:a.n@other": " Показано {found} результата ",
	}), ruForms)
	require.NoError(t, err)
	assert.Equal(t, "Показан {found} результат | Показано {found} результата | Показано {found} результатов | Показано {found} результата", got)

	_, err = assemble(unit, answered(map[string]string{"ui:a.n@one": "x"}), ruForms)
	assert.EqualError(t, err, "no item with id ui:a.n@few in your answer")

	// The schema pins every id to one that was asked for, so a model that would
	// have mangled one is forced onto a legal id instead — which may be another
	// item's. Two texts that differ under one id mean one of them belongs
	// elsewhere; the same text twice is one answer given twice.
	_, err = assemble(unit, answer{texts: map[string][]string{"ui:a.n@one": {"Показан {found} результат", "Показано {found} результата"}}}, ruForms)
	assert.EqualError(t, err, "your answer has 2 items with id ui:a.n@one and they differ; send one")

	repeated := answer{texts: map[string][]string{}}
	for id, text := range map[string]string{
		"ui:a.n@one": "Показан {found} результат", "ui:a.n@few": "Показано {found} результата",
		"ui:a.n@many": "Показано {found} результатов", "ui:a.n@other": "Показано {found} результата",
	} {
		repeated.texts[id] = []string{text, text}
	}
	got, err = assemble(unit, repeated, ruForms)
	require.NoError(t, err)
	assert.Equal(t, "Показан {found} результат | Показано {found} результата | Показано {found} результатов | Показано {found} результата", got)
}

// Only vue-i18n reads `|` as a plural: in any other syntax it is a character.
func TestWireItems_PipeIsAPluralInVueI18nAlone(t *testing.T) {
	items, err := wireItems(withID(unitOf(icuPlain, "Cars | {site_name}"), "ui:seo.title"), enForms, ruForms)
	require.NoError(t, err)
	assert.Equal(t, []wireItem{{ID: "ui:seo.title", Text: "Cars | {site_name}"}}, items)
}

// An ICU plural's sentence is asked for with the plural as an opaque
// placeholder, and its cases one at a time; the argument put back is the
// source's whatever the answer calls it.
func TestWireItems_ICUPluralSentenceAndCases(t *testing.T) {
	unit := withID(unitOf(icuLine, `{match_count, plural, one {# new listing} other {# new listings}} for "{name}"`), "email:d.subject")
	items, err := wireItems(unit, enForms, ruForms)
	require.NoError(t, err)
	require.Len(t, items, 5)
	assert.Equal(t, "email:d.subject", items[0].ID)
	assert.Equal(t, `{match_count} for "{name}"`, items[0].Text)
	assert.Contains(t, items[0].Context, "{match_count} is a placeholder standing for the number and the words that agree with it")
	assert.Equal(t, "email:d.subject@one", items[1].ID)
	assert.Equal(t, "{match_count} new listing", items[1].Text, "`#` is one more token to copy, so it crosses as the placeholder too")
	assert.Equal(t, "{match_count} new listings", items[2].Text)
	assert.Contains(t, items[2].Context, "the form used when {match_count} is 2, 3 or 4 (few)")
	assert.Contains(t, items[2].Context, "with {match_count} where the number goes")
	assert.True(t, strings.HasPrefix(items[0].Context, "One line."), "a one-line unit says so in every item it is asked as")

	got, err := assemble(unit, answered(map[string]string{
		"email:d.subject":       `{match_count} по запросу "{name}"`,
		"email:d.subject@one":   "{match_count} новое объявление",
		"email:d.subject@few":   "{match_count} новых объявления",
		"email:d.subject@many":  "{match_count} новых объявлений",
		"email:d.subject@other": "{match_count} новых объявления",
	}), ruForms)
	require.NoError(t, err)
	assert.Equal(t, `{match_count, plural, one {# новое объявление} few {# новых объявления} many {# новых объявлений} other {# новых объявления}} по запросу "{name}"`, got)

	// A case that writes the number out instead — which an exact category is
	// told it may — keeps what it wrote.
	got, err = assemble(unit, answered(map[string]string{
		"email:d.subject":       `{match_count} по запросу "{name}"`,
		"email:d.subject@one":   "одно новое объявление",
		"email:d.subject@few":   "{match_count} новых объявления",
		"email:d.subject@many":  "{match_count} новых объявлений",
		"email:d.subject@other": "{match_count} новых объявления",
	}), ruForms)
	require.NoError(t, err)
	assert.Contains(t, got, "one {одно новое объявление}")

	// The placeholder is located by name, so a model that renames it, writes
	// the plural out, or drops it is told to put it back.
	for _, broken := range []string{
		`по запросу "{name}"`,
		`{count} по запросу "{name}"`,
		`{match_count, plural, one {# a} other {# b}} по запросу "{name}"`,
	} {
		_, err = assemble(unit, answered(map[string]string{"email:d.subject": broken}), ruForms)
		assert.EqualError(t, err, "no {match_count} in your answer: it stands for the number and the words that agree with it, and must appear exactly as written", broken)
	}

	// A sentence that also carries the plural's own placeholder could not be
	// restored: both would take the plural.
	_, err = wireItems(unitOf(icuPlain, "{n, plural, one {# a} other {# b}} of {n}"), enForms, ruForms)
	assert.ErrorContains(t, err, "carries both a plural on {n} and a placeholder {n}")

	_, err = wireItems(unitOf(icuPlain, "{n, plural, =0 {none} one {# a} other {# b}}"), enForms, ruForms)
	assert.ErrorContains(t, err, "=N case")
	_, err = wireItems(unitOf(icuPlain, "{n, plural, one {# a} some {# b}}"), enForms, ruForms)
	assert.ErrorContains(t, err, `unknown plural keyword "some"`)
}

// A message that is the plural and nothing else — or the plural and
// punctuation — has no sentence to ask for: handed the placeholder alone, a
// model fills the plural in rather than keeping it. Only the forms are asked,
// and the frame is the source's own.
func TestWireItems_ICUPluralWithNothingAroundIt(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"{count, plural, one {# listing} other {# listings}}", "{count, plural, one {# a} few {# b} many {# c} other {# d}}"},
		{"{count, plural, one {# listing} other {# listings}}.", "{count, plural, one {# a} few {# b} many {# c} other {# d}}."},
	} {
		unit := withID(unitOf(icuPlain, tc.text), "ui:seo.n")
		items, err := wireItems(unit, enForms, ruForms)
		require.NoError(t, err)
		require.Len(t, items, 4, "forms only")
		for _, item := range items {
			assert.NotEqual(t, "ui:seo.n", item.ID)
		}
		got, err := assemble(unit, answered(map[string]string{
			"ui:seo.n@one": "# a", "ui:seo.n@few": "# b", "ui:seo.n@many": "# c", "ui:seo.n@other": "# d",
		}), ruForms)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got)
	}
}

// A form one count selects is told so, since that is what lets the language
// write the number as a word; a form many counts select is not.
func TestWireItems_ExactFormCarriesItsLicence(t *testing.T) {
	unit := withID(unitOf(vueHTML, "{n} listing | {n} listings"), "ui:a.n")

	arabic, err := wireItems(unit, enForms, sixForms)
	require.NoError(t, err)
	assert.Contains(t, arabic[1].Context, "It renders for 1 and no other number, so write the number as a word, or leave it out")
	assert.Contains(t, arabic[2].Context, "It renders for 2 and no other number")
	assert.NotContains(t, arabic[3].Context, "no other number", "Arabic's `few` takes 3 to 10")

	russian, err := wireItems(unit, enForms, ruForms)
	require.NoError(t, err)
	assert.NotContains(t, russian[0].Context, "no other number", "Russian's `one` takes 1, 21 and 31")
}

// A reference crosses the wire as the placeholder it behaves like — the model
// reads `@:brand.name` as a placeholder by another name and writes
// `{site_name}` in its place — and the renderer's syntax comes back.
func TestWireItems_ReferencesCrossAsPlaceholders(t *testing.T) {
	unit := withID(unitOf(vueHTML, "Sign in to @:brand.name with {label}"), "ui:a.b")
	items, err := wireItems(unit, enForms, ruForms)
	require.NoError(t, err)
	assert.Equal(t, "Sign in to {brand_name} with {label}", items[0].Text)

	w := wiredOf(t, unit)
	assert.Equal(t, []string{"{brand_name}"}, w.whole.tokens)
	text, err := assemble(w.unit, answered(map[string]string{"ui:a.b": "Войдите в {brand_name} с {label}"}), ruForms)
	require.NoError(t, err)
	require.NoError(t, validate(w, text, ruForms), "the reference is checked as the placeholder it was shown as")
	assert.Equal(t, "Войдите в @:brand.name с {label}", restoredOf(t, w, text))

	// A plural's forms carry it too, and a placeholder the source genuinely
	// has is left alone by the restoration.
	plural := withID(unitOf(vueHTML, "{n} listing on @:brand.name | {n} listings on @:brand.name"), "ui:a.n")
	items, err = wireItems(plural, enForms, ruForms)
	require.NoError(t, err)
	assert.Equal(t, "{n} listing on {brand_name}", items[0].Text)
	assert.Equal(t, "Объявления на @:brand.name, {n} из {total}", restoredOf(t, wiredOf(t, plural), "Объявления на {brand_name}, {n} из {total}"))

	// Restoring a source that carries both would ship one as the other.
	_, err = wireItems(unitOf(vueHTML, "@:brand.name is {brand_name}"), enForms, ruForms)
	assert.ErrorContains(t, err, "carries both @:brand.name and the placeholder {brand_name}")

	// Only vue-i18n has references: elsewhere `@:` is text.
	items, err = wireItems(unitOf(icuPlain, "Write to support@:help"), enForms, ruForms)
	require.NoError(t, err)
	assert.Equal(t, "Write to support@:help", items[0].Text)
}

// A tag's attributes are a line of CSS or a URL with nothing to translate,
// which a model nonetheless has to copy character for character — and
// duplicates a clause of. They cross the wire as a marker naming the element.
func TestWireItems_MarkupAttributesCrossAsMarkers(t *testing.T) {
	source := `<p>Here is a copy of your question on <a href="{public_fe_url}{listing_url}">{listing_title}</a>:</p>` + "\n" +
		`<div style="margin:16px 0;padding:8px 12px;border-left:3px solid #dddddd">{message}</div>` + "\n<p>Reference: {thread_id}</p>"
	unit := withID(unitOf(icuHTML, source), "email:t.body")

	items, err := wireItems(unit, enForms, ruForms)
	require.NoError(t, err)
	assert.Equal(t, `<p>Here is a copy of your question on <a attr1>{listing_title}</a>:</p>`+"\n"+
		`<div attr2>{message}</div>`+"\n<p>Reference: {thread_id}</p>", items[0].Text)

	w := wiredOf(t, unit)
	assert.Equal(t, []string{"<a attr1>", "<div attr2>"}, w.whole.tokens, "each tag crosses whole, and is checked back as it crossed")
	answerText := `<p>להלן עותק של השאילתה שלך בנוגע ל<a attr1>{listing_title}</a>:</p>` + "\n" +
		`<div attr2>{message}</div>` + "\n<p>סימוכין: {thread_id}</p>"
	require.NoError(t, validate(w, answerText, hebrew))
	assert.Equal(t, strings.Replace(strings.Replace(source,
		"Here is a copy of your question on ", "להלן עותק של השאילתה שלך בנוגע ל", 1),
		"Reference:", "סימוכין:", 1), restoredOf(t, w, answerText))

	// A tag whose marker the model dropped is a tag that no longer matches
	// the source's — checked in the terms the model was shown.
	assert.ErrorContains(t, validate(w, strings.Replace(answerText, "<a attr1>", "<a>", 1), hebrew), "HTML tag mismatch")

	// A source tag that already carries an attrN attribute is hidden like any
	// other, so it comes back as the source wrote it — even where it is
	// another tag's wire form.
	own := withID(unitOf(icuHTML, `<i attr2>{n}</i> and <i class="x">{m}</i>`), "email:t.own")
	items, err = wireItems(own, enForms, ruForms)
	require.NoError(t, err)
	assert.Equal(t, `<i attr1>{n}</i> and <i attr2>{m}</i>`, items[0].Text)
	assert.Equal(t, `<i class="x">{m}</i> und <i attr2>{n}</i>`, restoredOf(t, wiredOf(t, own), `<i attr2>{m}</i> und <i attr1>{n}</i>`))

	// Text with no markup has no tags, so nothing in it is hidden.
	items, err = wireItems(unitOf(label, `<b style="x">bold</b>`), enForms, ruForms)
	require.NoError(t, err)
	assert.Equal(t, `<b style="x">bold</b>`, items[0].Text)
}

// A tag crosses as plain HTML, with nothing in it that reads as a placeholder:
// shown `<a {attr2}>{site_name} user page</a>`, Gemma 4 31B rendered the
// Romanian as `<a {attr2}pagina ta de utilizator {site_name}</a>` twice
// running, carrying the sentence on after what it took for one more
// placeholder.
func TestWireItems_TagsCrossAsPlainHTML(t *testing.T) {
	source := `<p>As per your request, we reviewed your {provider_label} profile and confirmed the ownership proof.</p>` + "\n" +
		`<p>Your <a href="{profile_url}">{provider_label}</a> profile now appears as a verified link on your <a href="{user_url}">{site_name} user page</a>.</p>`
	unit := withID(unitOf(icuHTML, source), "email:validation_approved.body")

	items, err := wireItems(unit, enForms, ruForms)
	require.NoError(t, err)
	assert.NotContains(t, items[0].Text, "{attr")
	assert.Contains(t, items[0].Text, `<a attr1>{provider_label}</a> profile now appears as a verified link on your <a attr2>{site_name} user page</a>`)

	w := wiredOf(t, unit)
	romanian := `<p>Conform solicitării tale, am revizuit profilul tău de {provider_label} și am confirmat dovada de proprietate.</p>` + "\n" +
		`<p>Profilul tău de <a attr1>{provider_label}</a> apare acum ca un link verificat pe <a attr2>pagina ta de utilizator {site_name}</a>.</p>`
	require.NoError(t, validate(w, romanian, ruForms))
	assert.Contains(t, restoredOf(t, w, romanian),
		`<a href="{profile_url}">{provider_label}</a> apare acum ca un link verificat pe <a href="{user_url}">pagina ta de utilizator {site_name}</a>`)

	// The slip itself, in the new form, is still refused.
	assert.ErrorContains(t, validate(w, strings.Replace(romanian, "<a attr2>pagina", "<a attr2pagina", 1), ruForms), "HTML tag mismatch")
}

// A limit reaches the item as a note in its context, in the words the prompt's
// rule refers to.
func TestWireItems_LimitIsNoted(t *testing.T) {
	unit := withID(unitOf(subtitle, "List anything, anywhere"), "store:listing.subtitle")
	unit.Context = "The subtitle."
	items, err := wireItems(unit, enForms, ruForms)
	require.NoError(t, err)
	assert.Equal(t, "The subtitle.\nOne line.\nAt most 30 characters.", items[0].Context)
}

// A plural its source keys by category is asked for one form per category of
// the target, each showing the source's form for it or its `other`, and comes
// back keyed by the target's categories — line breaks and all.
func TestWireItems_KeyedPluralAcrossCategories(t *testing.T) {
	unit := withID(pluralOf(catalog, map[string]string{"one": "%lld song", "other": "%lld songs\nin the list"}), "ios:songs")
	unit.Context = "The playlist's length."

	russian, err := wireItems(unit, enForms, ruForms)
	require.NoError(t, err)
	require.Len(t, russian, 4)
	assert.Equal(t, wireItem{ID: "ios:songs@one", Text: "{p1} song", Context: "The playlist's length.\nOne form of a plural: the one used when the number is 1 (one). Write this form alone."}, russian[0])
	assert.Equal(t, "ios:songs@few", russian[1].ID)
	assert.Equal(t, "{p1} songs\nin the list", russian[1].Text, "the source's other stands for every category it lacks")
	assert.Contains(t, russian[1].Context, "2, 3 or 4 (few)")

	got, err := assemble(wiredOf(t, unit).unit, answered(map[string]string{
		"ios:songs@one": "{p1} песня", "ios:songs@few": "{p1} песни", "ios:songs@many": "{p1} песен", "ios:songs@other": " {p1} песни\nв списке ",
	}), ruForms)
	require.NoError(t, err)
	assert.Equal(t, `{"one":"{p1} песня","few":"{p1} песни","many":"{p1} песен","other":"{p1} песни\nв списке"}`, got)
	assert.Equal(t, `{"one":"%lld песня","few":"%lld песни","many":"%lld песен","other":"%lld песни\nв списке"}`, restoredOf(t, wiredOf(t, unit), got))
	require.NoError(t, validate(wiredOf(t, unit), got, ruForms))

	ja, err := wireItems(unit, enForms, japanese)
	require.NoError(t, err)
	require.Len(t, ja, 1, "Japanese has one form")
	assert.Equal(t, "ios:songs@other", ja[0].ID)
	got, err = assemble(wiredOf(t, unit).unit, answered(map[string]string{"ios:songs@other": "{p1} 曲"}), japanese)
	require.NoError(t, err)
	assert.Equal(t, `{"other":"%lld 曲"}`, restoredOf(t, wiredOf(t, unit), got))

	_, err = assemble(wiredOf(t, unit).unit, answered(map[string]string{"ios:songs@one": "x"}), ruForms)
	assert.EqualError(t, err, "no item with id ios:songs@few in your answer")
	_, err = wiredOf(t, unit).restore("not forms")
	assert.ErrorContains(t, err, "not a plural's forms")
}

// A printf conversion crosses as {pN}, N its argument, and comes back as the
// source wrote it while the arguments keep the source's order; reordered,
// repeated or with one left out, every conversion comes back positional. A
// literal percent crosses as the `%` it renders as, and a substitution as a
// placeholder of its own name.
func TestWire_PrintfCrossingAndRestoring(t *testing.T) {
	for _, tc := range []struct {
		name, source, wire, answer, restored string
	}{
		{"in order, as written", "%@ sent %d photos", "{p1} sent {p2} photos", "{p1} schickte {p2} Fotos", "%@ schickte %d Fotos"},
		{"positional, as written", "%2$@ from %1$@", "{p2} from {p1}", "{p2} von {p1}", "%2$@ von %1$@"},
		{"reordered", "%@ sent %d photos", "{p1} sent {p2} photos", "{p2} Fotos von {p1}", "%2$d Fotos von %1$@"},
		{"repeated", "%@ replied", "{p1} replied", "{p1} hat geantwortet, {p1}", "%1$@ hat geantwortet, %1$@"},
		{"the last left out", "%@ sent %d photos", "{p1} sent {p2} photos", "{p1} schickte Fotos", "%1$@ schickte Fotos"},
		{"flags, width and length kept", "%-8lld|%05.2f", "{p1}|{p2}", "{p2}|{p1}", "%2$05.2f|%1$-8lld"},
		{"a literal percent", "%d%% off", "{p1}% off", "{p1} % Rabatt", "%d %% Rabatt"},
		{"plain text", "100% free", "100% free", "100 % gratis", "100 % gratis"},
		{"substitutions", "%#@files@ in %#@folders@", "{files} in {folders}", "{folders}: {files}", "%#@folders@: %#@files@"},
		{"a substitution's own argument", "%arg files", "{p1} files", "{p1} {p1} Dateien", "%arg %arg Dateien"},
		{"a format string's every flag", "% d of %(d by %@", "{p1} of {p2} by {p3}", "{p3}: {p1} of {p2}", "%3$@: %1$ d of %2$(d"},
		{"a percent sign in prose", "100% secure", "100% secure", "100 % sicher", "100 % sicher"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := wiredOf(t, withID(unitOf(catalog, tc.source), "ios:x"))
			assert.Equal(t, tc.wire, w.unit.Text)
			assert.Equal(t, tc.restored, restoredOf(t, w, tc.answer))
		})
	}
	assert.Equal(t, []string{"{files}", "{folders}"}, wiredOf(t, unitOf(catalog, "%#@files@ in %#@folders@")).whole.tokens, "a substitution is checked back by identity")

	// A source the wire could not restore fails its batch.
	for source, want := range map[string]string{
		"%1$@ and %@":   "its conversions mix stated positions with none",
		"%d %! off":     "\"%!\" is a `%` that starts no conversion in a format string",
		"%d of {p1}":    "it carries both %d and the placeholder {p1}",
		"%#@n@ and {n}": "it carries both %#@n@ and the placeholder {n}",
		"%s %d %.*f":    "\"%.*f\" is a `%` that starts no conversion",
	} {
		_, err := wire(withID(unitOf(catalog, source), "ios:x"))
		assert.ErrorContains(t, err, "ios:x: "+want, source)
	}
}

// An <xliff:g> span crosses whole as one placeholder named for its id, and
// nothing in it reaches the model — neither a literal it protects nor a
// conversion. A span's conversions count in the arguments' order at its
// place: moved after a bare conversion, both come back positional, the rest of
// the span as written.
func TestWire_ProtectedSpans(t *testing.T) {
	unit := withID(unitOf(resource, `Open <xliff:g id="app" example="Gmail">Gmail</xliff:g> for <b><xliff:g id="count" example="3">%d</xliff:g></b> new in %s`), "app:x")
	w := wiredOf(t, unit)
	assert.Equal(t, `Open {app} for <b>{count}</b> new in {p2}`, w.unit.Text)
	assert.Equal(t, []string{"{app}", "{count}"}, w.whole.tokens)

	assert.Equal(t, `Öffne <xliff:g id="app" example="Gmail">Gmail</xliff:g> für <b><xliff:g id="count" example="3">%d</xliff:g></b> Neue in %s`,
		restoredOf(t, w, `Öffne {app} für <b>{count}</b> Neue in {p2}`), "in the source's order, every span as written")
	assert.Equal(t, `Dans %2$s, <b><xliff:g id="count" example="3">%1$d</xliff:g></b> nouveaux : ouvrez <xliff:g id="app" example="Gmail">Gmail</xliff:g>`,
		restoredOf(t, w, `Dans {p2}, <b>{count}</b> nouveaux : ouvrez {app}`), "moved after a bare conversion, both positional")

	require.NoError(t, validate(w, `Dans {p2}, <b>{count}</b> nouveaux : ouvrez {app}`, twoForms))
	assert.ErrorContains(t, validate(w, `Dans {p2}, <b></b> nouveaux : ouvrez {app}`, twoForms), "{count} appears 1 time(s) in the source and 0 in the translation")
	assert.ErrorContains(t, validate(w, `{app} {app} <b>{count}</b> {p2}`, twoForms), "{app} appears 1 time(s) in the source and 2 in the translation")

	// A span without an id is numbered; two spans one id names must say the
	// same, and format the same arguments, and a span may not take a
	// conversion's placeholder.
	assert.Equal(t, "{xliff1} and {xliff2}", wiredOf(t, unitOf(resource, `<xliff:g>km</xliff:g> and <xliff:g id="9">mi</xliff:g>`)).unit.Text)
	assert.Equal(t, "{u} or {u}", wiredOf(t, unitOf(resource, `<xliff:g id="u">km</xliff:g> or <xliff:g id="u">km</xliff:g>`)).unit.Text)
	assert.Equal(t, "{n} of {n}", wiredOf(t, unitOf(resource, `<xliff:g id="n">%1$d</xliff:g> of <xliff:g id="n">%1$d</xliff:g>`)).unit.Text)
	_, err := wire(unitOf(resource, `<xliff:g id="u">km</xliff:g> or <xliff:g id="u">mi</xliff:g>`))
	assert.ErrorContains(t, err, "which would cross the wire as one placeholder {u}")
	_, err = wire(unitOf(resource, `<xliff:g id="n">%d</xliff:g> of <xliff:g id="n">%d</xliff:g>`))
	assert.ErrorContains(t, err, `it carries <xliff:g id="n">%d</xliff:g> (argument 1) and <xliff:g id="n">%d</xliff:g> (argument 2), which would cross the wire as one placeholder {n}`)
	_, err = wire(unitOf(resource, `<xliff:g id="p1">x</xliff:g> %d`))
	assert.ErrorContains(t, err, "which would cross the wire as one placeholder {p1}")

	// Literal text hides its spans too, and has no conversions to number.
	plain := wiredOf(t, unitOf(format.Checks{Syntax: format.SyntaxNone, Markup: format.MarkupHTML, Shape: format.ShapeText}, `100% <xliff:g id="u">km</xliff:g>`))
	assert.Equal(t, "100% {u}", plain.unit.Text)
	assert.Equal(t, `100 % <xliff:g id="u">km</xliff:g>`, restoredOf(t, plain, "100 % {u}"))
}

// A plain unit is one item, unchanged.
func TestWireItems_PlainUnit(t *testing.T) {
	unit := withID(unitOf(vueHTML, "Submit"), "ui:a.b")
	unit.Context = "A button."
	items, err := wireItems(unit, enForms, ruForms)
	require.NoError(t, err)
	assert.Equal(t, []wireItem{{ID: "ui:a.b", Text: "Submit", Context: "A button."}}, items)
	got, err := assemble(unit, answered(map[string]string{"ui:a.b": "Отправить"}), ruForms)
	require.NoError(t, err)
	assert.Equal(t, "Отправить", got)
}
