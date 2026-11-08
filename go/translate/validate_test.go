package translate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"greendrake.nz/go/translate/format"
)

// The checks a unit resolves to, as the sources of a typical product declare
// them.
var (
	vueHTML  = format.Checks{Syntax: format.SyntaxVueI18n, Markup: format.MarkupHTML, Shape: format.ShapeText}
	icuHTML  = format.Checks{Syntax: format.SyntaxICU, Markup: format.MarkupHTML, Shape: format.ShapeText}
	icuLine  = format.Checks{Syntax: format.SyntaxICU, Markup: format.MarkupNone, Shape: format.ShapeLine}
	icuPlain = format.Checks{Syntax: format.SyntaxICU, Markup: format.MarkupNone, Shape: format.ShapeText}
	prose    = format.Checks{Syntax: format.SyntaxBraces, Markup: format.MarkupMarkdown, Shape: format.ShapeText}
	label    = format.Checks{Syntax: format.SyntaxNone, Markup: format.MarkupNone, Shape: format.ShapeLine}
	literal  = format.Checks{Syntax: format.SyntaxNone, Markup: format.MarkupHTML, Shape: format.ShapeText}
	subtitle = format.Checks{Syntax: format.SyntaxBraces, Markup: format.MarkupNone, Shape: format.ShapeLine, Limit: 30}
	// resource is an Android string resource, catalog an iOS String
	// Catalog's string.
	resource = format.Checks{Syntax: format.SyntaxPrintf, Markup: format.MarkupHTML, Shape: format.ShapeText}
	catalog  = format.Checks{Syntax: format.SyntaxPrintf, Markup: format.MarkupNone, Shape: format.ShapeText}
)

// pluralOf is a keyed plural of forms.
func pluralOf(checks format.Checks, forms map[string]string) format.Unit {
	unit := unitOf(checks, format.EncodeForms(forms))
	unit.Forms = forms
	return unit
}

func unitOf(checks format.Checks, text string) format.Unit {
	return format.Unit{ID: "p:x", Pass: "p", Key: "x", Text: text, Checks: checks}
}

// writingFor is a locale's writing, for fixtures that name only locales whose
// script the engine knows.
func writingFor(locale string) writing {
	w, err := writingOf(locale)
	if err != nil {
		panic(err)
	}
	return w
}

var twoForms = writingFor("de")
var fourForms = writingFor("ru")

// Arabic distinguishes six, three of which a single count selects: zero, one
// and two.
var sixForms = writingFor("ar")

var japanese, hebrew, greek = writingFor("ja"), writingFor("he"), writingFor("el")
var icelandic, maltese, hawaiian, nepali = writingFor("is"), writingFor("mt"), writingFor("haw"), writingFor("ne")

// arabicShowing is the source below rendered for Arabic, with the singular and
// the dual writing the count in words as the language does.
const arabicShowing = "عرض {found} نتيجة من أصل {total} | عرض نتيجة واحدة من أصل {total} | عرض نتيجتين من أصل {total} | " +
	"عرض {found} نتائج من أصل {total} | عرض {found} نتيجة من أصل {total} | عرض {found} نتيجة من أصل {total}"

// checked validates a candidate the way a run does: in the wire's terms, the
// candidate being what the model answered.
func checked(unit format.Unit, candidate string, target writing) error {
	w, err := wire(unit)
	if err != nil {
		return err
	}
	return validate(w, candidate, target)
}

func TestValidate_Accepts(t *testing.T) {
	cases := []struct {
		name      string
		unit      format.Unit
		candidate string
		target    writing
	}{
		{"plain string", unitOf(vueHTML, "Submit"), "Absenden", twoForms},
		{"placeholder kept", unitOf(vueHTML, "Hello, {name}"), "Hallo, {name}", twoForms},
		{"placeholder moved", unitOf(vueHTML, "{count} new this week"), "Diese Woche {count} neue", twoForms},
		// The renderer substitutes by name, so naming one again where the
		// language has no pronoun for "them" renders.
		{"placeholder repeated", unitOf(vueHTML, "{name} will no longer be able to message you, and you will not be able to message them."),
			"{name} kommer inte längre att kunna skicka meddelanden till dig, och du kommer inte att kunna skicka meddelanden till {name}.", twoForms},
		{"reference kept as it was shown", unitOf(vueHTML, "Welcome to @:brand.name"), "Willkommen bei {brand_name}", twoForms},
		{"html kept", unitOf(vueHTML, `Enter your <i>current</i> password:`), `Gib dein <i>aktuelles</i> Passwort ein:`, twoForms},
		{"attributes kept as they were shown", unitOf(vueHTML, `<a href="/x" target="_blank">go</a>`), `<a attr1>los</a>`, twoForms},
		{"inline elements reordered", unitOf(icuHTML, `<p>Copy of your <strong>{amount}</strong> offer on <a href="{url}">{title}</a>:</p>`),
			`<p><a attr1>{title}</a>への<strong>{amount}</strong>のオファーの控え:</p>`, japanese},
		{"void tag", unitOf(icuHTML, "Line<br>{code}"), "Zeile<br>{code}", twoForms},
		{"pipe plural, two forms", unitOf(vueHTML, "{n} listing | {n} listings"), "{n} Anzeige | {n} Anzeigen", twoForms},
		{"pipe plural, four forms", unitOf(vueHTML, "{n} listing | {n} listings"), "{n} a | {n} b | {n} c | {n} d", fourForms},
		{"icu plural recased", unitOf(icuLine, "{count, plural, one {# item} other {# items}}"),
			"{count, plural, one {# a} few {# b} many {# c} other {# d}}", fourForms},
		{"markdown link kept", unitOf(prose, "see [an example](/U7DGN95PR51)"), "siehe [ein Beispiel](/U7DGN95PR51)", twoForms},
		{"heading level kept", unitOf(prose, "## How to list stuff?"), "## Wie inseriert man?", twoForms},
		{"list kept", unitOf(prose, " - One\n - Two"), " - Eins\n - Zwei", twoForms},
		{"label", unitOf(label, "listing"), "Anzeige", twoForms},
		// Literal text has no placeholders, so braces in it are words.
		{"braces in literal text", unitOf(label, "Size {EU}"), "Größe {EU-Norm}", twoForms},
		// A `|` is punctuation to a renderer that does not read it as forms,
		// and a translation keeps the source's.
		{"literal pipe kept", unitOf(icuPlain, "Cars | {site_name}"), "Autos | {site_name}", twoForms},
		// A form only one count selects renders for that number and no other,
		// so the language may write it as a word — or, in the dual, not at all.
		{"exact form writes its count in words", unitOf(vueHTML, "Showing {found} result out of {total} | Showing {found} results out of {total}"),
			arabicShowing, sixForms},
		// Latin is every locale's for the names that stay as their owners write
		// them; a script the source carries is the source's to keep; and a
		// letter of no script in particular is anyone's.
		{"latin name in greek", unitOf(vueHTML, "Sign in with eBay"), "Σύνδεση με eBay", greek},
		{"kana with kanji and the long-vowel mark", unitOf(vueHTML, "Computer settings"), "コンピューターの設定", japanese},
		{"okina", unitOf(label, "Interface language"), "ʻŌlelo o ka papahana", hawaiian},
		{"a script the source carries", unitOf(vueHTML, "Say Ελλάδα"), "Sag Ελλάδα", twoForms},
		{"a backslash the source has", unitOf(label, `Folder C:\temp`), `Ordner C:\temp`, twoForms},
		// A format string's arguments move, repeat and keep its percent signs
		// as a sentence needs.
		{"printf arguments moved", unitOf(catalog, "%@ sent %d photos"), "{p2} Fotos von {p1}", twoForms},
		{"printf argument repeated", unitOf(catalog, "%@ replied"), "{p1} hat geantwortet, {p1}", twoForms},
		{"printf literal percent kept", unitOf(catalog, "%d%% off"), "{p1} % Rabatt", twoForms},
		{"percent in plain text kept", unitOf(catalog, "100% free"), "100 % gratis", twoForms},
		{"substitutions kept", unitOf(catalog, "%#@files@ in %#@folders@"), "{folders}: {files}", twoForms},
		{"protected span kept", unitOf(resource, `<xliff:g id="app">Gmail</xliff:g> sync`), "{app}-Synchronisierung", twoForms},
		{"keyed plural, a form one count selects drops it", pluralOf(catalog, map[string]string{"one": "%d song", "other": "%d songs"}),
			format.EncodeForms(map[string]string{"one": "ein Lied", "other": "{p1} Lieder"}), twoForms},
		{"keyed plural, the last argument left out", pluralOf(catalog, map[string]string{"one": "%1$@ sent %2$d photo", "other": "%1$@ sent %2$d photos"}),
			format.EncodeForms(map[string]string{"one": "{p1} schickte ein Foto", "other": "{p1} schickte {p2} Fotos"}), twoForms},
		{"keyed plural, every form held to its own", pluralOf(catalog, map[string]string{"one": "One song", "other": "%d songs"}),
			format.EncodeForms(map[string]string{"one": "Одна песня", "few": "{p1} песни", "many": "{p1} песен", "other": "{p1} песни"}), fourForms},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, checked(tc.unit, tc.candidate, tc.target))
		})
	}
}

func TestValidate_Rejects(t *testing.T) {
	cases := []struct {
		name       string
		unit       format.Unit
		candidate  string
		target     writing
		wantSubstr string
	}{
		{"empty", unitOf(vueHTML, "Submit"), "  ", twoForms, "empty translation"},
		{"placeholder dropped", unitOf(vueHTML, "Hello, {name}"), "Hallo!", twoForms, "placeholder mismatch"},
		{"placeholder translated", unitOf(vueHTML, "Hello, {name}"), "Hallo, {Name}", twoForms, "placeholder mismatch"},
		{"placeholder invented", unitOf(vueHTML, "About"), "O {site_name}", twoForms, "the source has none, so the translation can have none — write it without {site_name}"},
		{"reference spelled out", unitOf(vueHTML, "Welcome to @:brand.name"), "Willkommen bei Acme", twoForms, "{brand_name} appears 1 time(s) in the source and 0 in the translation"},
		{"reference repeated", unitOf(vueHTML, "Welcome to @:brand.name"), "{brand_name}: Willkommen bei {brand_name}", twoForms, "{brand_name} appears 1 time(s) in the source and 2 in the translation"},
		{"html dropped", unitOf(vueHTML, `Your <i>current</i> password`), "Dein aktuelles Passwort", twoForms, "HTML tag mismatch"},
		{"html mis-nested", unitOf(icuHTML, `<p><strong>{a}</strong> on <a href="{u}">{t}</a></p>`), `<p><strong><a attr1>{t}</strong></a> {a}</p>`, twoForms, "not nested properly"},
		{"html left open", unitOf(icuHTML, `<p>Hello</p>`), `<p>Hallo<p>`, twoForms, "never closed"},
		{"link target changed", unitOf(prose, "see [an example](/U7DGN95PR51)"), "siehe [ein Beispiel](/beispiel)", twoForms, "markdown link target mismatch"},
		{"too few plural forms", unitOf(vueHTML, "{n} listing | {n} listings"), "{n} Anzeige | {n} Anzeigen", fourForms, "has 4 form(s)"},
		{"too many plural forms", unitOf(vueHTML, "{n} listing | {n} listings"), "{n} a | {n} b | {n} c", twoForms, "has 2 form(s)"},
		{"plural form loses its placeholder", unitOf(vueHTML, "{n} listing | {n} listings"), "{n} Anzeige | viele Anzeigen", twoForms, "the other form"},
		{"icu cases not the locale's", unitOf(icuLine, "{count, plural, one {# item} other {# items}}"),
			"{count, plural, one {# a} other {# b}}", fourForms, "names the cases one, few, many, other, in that order; this one names one, other"},
		{"icu selector that is not a case", unitOf(icuLine, "{count, plural, one {# item} other {# items}}"),
			"{count, plural, one {# a} own {# b} other {# c}}", fourForms, `unknown plural keyword "own"`},
		{"unbalanced brace in a rendered message", unitOf(icuHTML, "Your code is below"), "Dein Code ist {unten", twoForms, "does not parse"},
		// Another renderer's syntax a model carries over from the strings
		// around it: an ICU renderer prints it, a vue-i18n one acts on it.
		{"vue plural in an ICU string", unitOf(icuPlain, "Listings"), "Anzeige | Anzeigen", twoForms, "`|` mismatch: the source has none"},
		{"pipe invented in a vue-i18n string", unitOf(vueHTML, "Results"), "Ergebnis | Ergebnisse", twoForms, "`|` mismatch: the source has none"},
		{"reference invented in an ICU string", unitOf(icuPlain, "About us"), "Über @:brand.name", twoForms, "reference mismatch: the source has none, so the translation can have none — write it without @:brand.name"},
		{"reference invented in a vue-i18n string", unitOf(vueHTML, "Welcome"), "Willkommen bei @:brand.name", twoForms, "reference mismatch"},
		// Text with no markup shows a tag's angle brackets to its reader.
		{"markup invented in a push body", unitOf(icuLine, "Offer received"), "<b>Angebot</b>", twoForms, "HTML tag mismatch"},
		{"markup invented in a label", unitOf(label, "Cars"), "<i>Autos</i>", twoForms, "HTML tag mismatch"},
		{"multi-line label", unitOf(label, "Cars"), "Autos\nund mehr", twoForms, "one line"},
		{"multi-line subject", unitOf(icuLine, "Activate"), "Aktivieren\njetzt", twoForms, "one line"},
		{"heading level changed", unitOf(prose, "## How to list"), "### Wie inseriert man", twoForms, "heading level mismatch"},
		{"list turned into prose", unitOf(prose, " - One\n - Two"), "Eins und zwei", twoForms, "list marker mismatch"},
		// Markdown reads ASCII digits alone as a list's numbers.
		{"list numbered in the language's own digits", unitOf(prose, "1. Sign in.\n2. Post."), "१. साइन इन गर्नुहोस्।\n२. पोस्ट गर्नुहोस्।", nepali,
			"list numbered १.: markdown reads a list only when it is numbered in ASCII digits"},
		// The licence to write the count in words belongs to a form one count
		// selects, and covers that count alone: Russian's `one` takes 21 and
		// 31 as well, and no form may drop what still varies.
		{"a form many counts select keeps the number", unitOf(vueHTML, "{n} listing | {n} listings"),
			"одно объявление | {n} объявления | {n} объявлений | {n} объявлений", fourForms, "the one form: placeholder mismatch"},
		{"exact form drops what still varies", unitOf(vueHTML, "Showing {found} result out of {total} | Showing {found} results out of {total}"),
			strings.Replace(arabicShowing, "عرض نتيجة واحدة من أصل {total}", "عرض نتيجة واحدة", 1), sixForms,
			"the placeholder holding that number may be left out — but not {found} and {total}"},
		{"exact form invents a placeholder", unitOf(vueHTML, "Showing {found} result out of {total} | Showing {found} results out of {total}"),
			strings.Replace(arabicShowing, "عرض نتيجة واحدة من أصل {total}", "عرض نتيجة واحدة من أصل {count}", 1), sixForms,
			"{count} is not in the source"},
		{"placeholder invented beside a repeated one", unitOf(vueHTML, "{name} replied"), "{name} ({name}) antwortete {site_name}", twoForms,
			"placeholder mismatch: the source has [{name}] and the translation [{name} {site_name}]"},
		// The licence covers the count's placeholder and nothing the wire put
		// there: a reference is never the count.
		{"exact form drops a reference", unitOf(vueHTML, "{n} listing on @:brand.name | {n} listings on @:brand.name"),
			"إعلان واحد | إعلان واحد على {brand_name} | {n} على {brand_name} | {n} على {brand_name} | {n} على {brand_name} | {n} على {brand_name}", sixForms,
			"the zero form: placeholder mismatch: {brand_name} appears 1 time(s) in the source and 0"},
		// Literal text has no placeholder check, yet its markup's attributes
		// still cross the wire as markers, and one written outside its tag
		// would reach the reader as a word.
		{"attribute marker outside its tag", unitOf(literal, `<div style="margin:0">Hi</div>`), `<div attr1>Hallo attr1</div>`, twoForms,
			"attribute marker outside its tag: attr1 belongs only inside its HTML tag"},
		{"tag duplicated", unitOf(icuHTML, `<p><a href="{u}">{t}</a></p>`), `<p><a attr1>{t}</a> <a attr1>{t}</a></p>`, twoForms,
			"<a attr1> appears 1 time(s) in the source and 2 in the translation"},
		// A word of another language, in its own script.
		{"cyrillic word in icelandic", unitOf(prose, "as little personal data as possible"), "eins litlum persónuupplýsingum og можливо", icelandic,
			"written in Cyrillic, which this language is not: можливо"},
		{"devanagari in maltese", unitOf(prose, "is still"), "għad दहu m’għadux", maltese, "written in Devanagari, which this language is not: दहu"},
		// Barred from \u escapes, a model can still spell one out as text.
		{"escape spelled out", unitOf(vueHTML, "Greece"), spelled("Ελλάδα"), greek, "`\\` mismatch: the source has none"},
		// A format string's arguments are the caller's: none dropped, none
		// invented, and a C-style formatter cannot skip one it is never told
		// the type of.
		{"printf argument dropped", unitOf(catalog, "%@ sent %d photos"), "{p1} schickte Fotos", twoForms, "placeholder mismatch: the source has [{p1} {p2}] and the translation [{p1}]"},
		{"printf argument invented", unitOf(catalog, "%d photos"), "{p1} Fotos von {p2}", twoForms, "placeholder mismatch"},
		{"printf left out before a kept one", pluralOf(catalog, map[string]string{"one": "%1$@ sent %2$d photo", "other": "%1$@ sent %2$d photos"}),
			format.EncodeForms(map[string]string{"one": "ein Foto: {p2}", "other": "{p1} schickte {p2} Fotos"}), twoForms,
			"the one form: {p1} is left out while {p2} is kept: arguments are read in order, so only the last may be left out — keep {p1}"},
		{"percent invented", unitOf(catalog, "%d items"), "{p1} % Artikel", twoForms, "`%` mismatch: the source has none"},
		{"percent dropped", unitOf(catalog, "%d%% off"), "{p1} Rabatt", twoForms, "`%` mismatch: the translation has none"},
		{"conversion written raw", unitOf(catalog, "%d items"), "%d Artikel", twoForms, "`%` mismatch: the source has none"},
		{"substitution dropped", unitOf(catalog, "%#@files@ in %#@folders@"), "{files}", twoForms, "{folders} appears 1 time(s) in the source and 0 in the translation"},
		{"protected span dropped", unitOf(resource, `<xliff:g id="app">Gmail</xliff:g> sync`), "Gmail-Synchronisierung", twoForms, "{app} appears 1 time(s) in the source and 0 in the translation"},
		{"protected span doubled", unitOf(resource, `<xliff:g id="app">Gmail</xliff:g> sync`), "{app} mit {app}", twoForms, "{app} appears 1 time(s) in the source and 2 in the translation"},
		{"keyed plural form empty", pluralOf(catalog, map[string]string{"one": "%d song", "other": "%d songs"}),
			format.EncodeForms(map[string]string{"one": " ", "other": "{p1} Lieder"}), twoForms, "the one form: empty translation"},
		{"keyed plural form missing", pluralOf(catalog, map[string]string{"one": "%d song", "other": "%d songs"}),
			format.EncodeForms(map[string]string{"other": "{p1} Lieder"}), twoForms, "the one form: empty translation"},
		{"keyed plural not forms", pluralOf(catalog, map[string]string{"one": "%d song", "other": "%d songs"}), "{p1} Lieder", twoForms, "not a plural's forms"},
		{"keyed plural form on two lines", pluralOf(icuLine, map[string]string{"one": "{n} song", "other": "{n} songs"}),
			format.EncodeForms(map[string]string{"one": "ein Lied", "other": "{n}\nLieder"}), twoForms, "the other form: this item is one line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checked(tc.unit, tc.candidate, tc.target)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantSubstr)
		})
	}
}

// A limit counts characters, not bytes, and names both numbers when it is
// exceeded, so a retry knows how much to lose.
func TestWithinLimit(t *testing.T) {
	limited := unitOf(subtitle, "List anything, anywhere")
	require.NoError(t, withinLimit(limited, strings.Repeat("x", 30)))
	require.NoError(t, withinLimit(limited, strings.Repeat("я", 30)), "thirty letters, sixty bytes")
	assert.EqualError(t, withinLimit(limited, "Inserieren Sie alles und überall"), "this item is 32 characters long and may be at most 30: say it more briefly")
	require.NoError(t, withinLimit(unitOf(label, "Cars"), strings.Repeat("x", 1000)), "no limit declared, none held")
}

// A rejected item is retried with the complaint attached, so the complaint has
// to name everything that is wrong: a model told only that it invented
// {site_name} removes it and leaves the reference gone.
func TestValidate_ReportsEveryTokenProblem(t *testing.T) {
	err := checked(unitOf(vueHTML, "Sign in to @:brand.name with {label}"), "היכנס ל-{site_name} באמצעות {label}", hebrew)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{brand_name} appears 1 time(s) in the source and 0")
	assert.Contains(t, err.Error(), "placeholder mismatch: the source has [{label}] and the translation [{site_name} {label}]")
}
