package translate

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"greendrake.nz/go/i18n/messageformat"
	"greendrake.nz/go/translate/format"
)

// What a translation must preserve. Every rule here is also stated in the
// prompt: the prompt is how a model usually gets it right, this is how a wrong
// one is caught before it reaches a file. Nothing degrades to the master's
// language on failure — a rendered string nobody validated is worse than a
// run that stops.

var (
	// icuArgument matches a value substitution and the head of a plural
	// construct alike: `{name}`, `{count, plural, …}`. What must survive is the
	// set of names, since those are what the caller supplies.
	icuArgument = regexp.MustCompile(`\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*[,}]`)
	// linkedMessage matches vue-i18n's `@:some.key` reference.
	linkedMessage = regexp.MustCompile(`@:[A-Za-z0-9_.]+`)
	// htmlTag matches an element's opening or closing tag.
	htmlTag = regexp.MustCompile(`</?[A-Za-z][A-Za-z0-9]*(\s[^<>]*)?/?>`)
	// markdownLink matches the target of an inline link.
	markdownLink = regexp.MustCompile(`\]\(([^)]*)\)`)
	// pluralKeyword matches an ICU plural case selector.
	pluralKeyword = regexp.MustCompile(`(?:^|\s)(zero|one|two|few|many|other)\s*\{`)
	// headingPrefix and listMarker are the markdown skeleton a translated block
	// must reproduce. A marker is matched in any script's digits, so a list a
	// model numbered in its language's own is named for what it is rather
	// than as a list gone missing.
	headingPrefix = regexp.MustCompile(`(?m)^(#{1,6})\s`)
	listMarker    = regexp.MustCompile(`(?m)^\s*([-*+]|\p{Nd}+\.)\s`)
)

// validate checks one candidate translation against its source, both in the
// wire's terms: tokens the wire put in the source must come back as they were.
//
// A plural of forms is several messages in one, and the target locale decides
// how many. Its tokens are therefore compared form by form: comparing the
// whole would count a Russian message's four `{n}` against an English one's
// two and call the translation wrong for being correct. A vue-i18n plural's
// forms are each held to its first, every form of it saying the same thing
// about the same values, differing only in grammar; a keyed plural's to the
// source's form each was asked for against.
func validate(w wired, candidate string, target writing) error {
	unit := w.unit
	switch {
	case unit.Forms != nil:
		got, err := format.DecodeForms(candidate)
		if err != nil {
			return err
		}
		return validateForms(unit, target, got, func(category string) (string, crossing) {
			shown := shownCategory(unit.Forms, category)
			return unit.Forms[shown], w.forms[shown]
		})
	case pipePlural(unit):
		got := splitForms(candidate)
		if err := formCount(got, target.categories); err != nil {
			return err
		}
		forms := make(map[string]string, len(got))
		for i, category := range target.categories {
			forms[category] = got[i]
		}
		reference := strings.TrimSpace(strings.Split(unit.Text, "|")[0])
		return validateForms(unit, target, forms, func(string) (string, crossing) { return reference, w.whole })
	}
	if err := validateText(unit, w.whole, unit.Text, candidate, target, placeholdersPreserved); err != nil {
		return err
	}
	if unit.Checks.Syntax == format.SyntaxICU {
		// The renderer treats every brace as syntax, so an unbalanced one is
		// not a cosmetic defect but a render-time failure. Its parser goes
		// before the plural-case check because it names what that check cannot
		// see: a selector that is not a case at all.
		if err := messageformat.Validate(candidate); err != nil {
			return fmt.Errorf("does not parse as a message: %w", err)
		}
		if err := validateICUPlural(unit, candidate, target.categories); err != nil {
			return err
		}
	}
	if unit.Checks.Markup == format.MarkupMarkdown {
		return joined(
			compare("heading level", submatches(headingPrefix, unit.Text), submatches(headingPrefix, candidate), true),
			compare("list marker", listKinds(unit.Text), listKinds(candidate), true),
			listNumerals(candidate))
	}
	return nil
}

// validateText holds one text of a translation — the whole, or one form — to
// the source text it answers.
func validateText(unit format.Unit, c crossing, source, candidate string, target writing, placeholdersCheck func(source, candidate string) error) error {
	if strings.TrimSpace(candidate) == "" {
		return errors.New("empty translation")
	}
	if unit.Checks.Shape == format.ShapeLine && strings.Contains(candidate, "\n") {
		return errors.New("this item is one line: write it without a line break")
	}
	if err := scriptsHeld(source, candidate, target.scripts); err != nil {
		return err
	}
	return joined(tokensPreserved(unit, c.tokens, source, candidate, placeholdersCheck), c.args.kept(candidate))
}

// validateForms holds a plural's forms, one per category of the target
// locale, each to the source text reference gives for it. A form one count
// selects may leave that count's placeholder out.
func validateForms(unit format.Unit, target writing, forms map[string]string, reference func(category string) (string, crossing)) error {
	for _, category := range target.categories {
		check := placeholdersPreserved
		if _, exact := target.exact[category]; exact {
			check = placeholdersInExactForm
		}
		source, c := reference(category)
		if err := validateText(unit, c, source, forms[category], target, check); err != nil {
			return fmt.Errorf("the %s form: %w", category, err)
		}
	}
	return nil
}

// withinLimit holds a translation to its unit's limit, counted in characters
// rather than bytes: a store counts what a reader sees.
func withinLimit(unit format.Unit, written string) error {
	if unit.Checks.Limit == 0 {
		return nil
	}
	if length := utf8.RuneCountInString(written); length > unit.Checks.Limit {
		return fmt.Errorf("this item is %d characters long and may be at most %d: say it more briefly", length, unit.Checks.Limit)
	}
	return nil
}

// tokensPreserved holds a translation to everything of its source that is not
// language: the tokens the wire put in it, the values the caller substitutes,
// the markup it carries and the links it points at.
//
// Some of it is held whatever the unit declares. An HTML tag, a vue-i18n
// `@:reference` and a `|` are syntax to one renderer or another, and a model
// that has just translated a hundred strings full of them writes them into a
// string that has none: where the unit's renderer reads them, one invented
// changes what renders — a `|` makes a message a plural, a reference pulls in
// another message — and where it does not, the reader gets the angle brackets
// and the pipe. A backslash is where a model that may not escape a character
// spells the escape out instead: `\\u03B5` is valid JSON for a backslash, a u
// and ε's hex, not for ε. Either way the translation carries exactly the
// source's. A printf text holds its `%` the same way: a `%` a model writes is
// a conversion to the formatter, and one it drops is a percent sign lost. The
// rest follows the unit's syntax and markup.
//
// Nothing here is held to the source's order. A sentence's word order is the
// translation's own, and the elements in it move with the words — Japanese
// puts the listing before the amount where English has them the other way —
// so the markup is compared as a multiset and then checked to nest, which is
// the property that actually matters to a renderer.
func tokensPreserved(unit format.Unit, tokens []string, source, candidate string, placeholdersCheck func(source, candidate string) error) error {
	errs := []error{
		wireTokens(tokens, source, candidate),
		compare("HTML tag", normalisedTags(source), normalisedTags(candidate), false),
		compare("reference", linkedMessage.FindAllString(source, -1), linkedMessage.FindAllString(candidate, -1), false),
	}
	for _, held := range heldCharacters(unit) {
		errs = append(errs, compare("`"+held+"`", occurrences(source, held), occurrences(candidate, held), false))
	}
	if unit.Checks.Placeholders() {
		// The wire's tokens are checked above and only there: a placeholder
		// check would call one both dropped and, under a form's licence,
		// allowed to be.
		errs = append(errs, placeholdersCheck(withoutTokens(source, tokens), withoutTokens(candidate, tokens)))
	}
	if unit.Checks.Tagged() {
		errs = append(errs, wellNested(candidate), markersInTags(source, candidate))
	}
	if unit.Checks.Markup == format.MarkupMarkdown {
		errs = append(errs, compare("markdown link target", submatches(markdownLink, source), submatches(markdownLink, candidate), false))
	}
	return joined(errs...)
}

// wireTokens holds a translation to the tokens the wire put in its source,
// each as often as the source has it. They are the engine's own encoding and
// are restored by literal replacement, so one dropped is a reference or an
// attribute lost, and one repeated is one duplicated — whatever the host's
// syntax says a placeholder is, and even where a form's licence lets the
// count's own placeholder go.
func wireTokens(tokens []string, source, candidate string) error {
	var problems []string
	for _, token := range distinct(tokens) {
		if want, got := strings.Count(source, token), strings.Count(candidate, token); want != got {
			problems = append(problems, fmt.Sprintf("%s appears %d time(s) in the source and %d in the translation", token, want, got))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("placeholder mismatch: %s — reproduce each exactly as often as the source has it", strings.Join(problems, "; "))
}

// heldCharacters are the characters tokensPreserved holds to the source's
// count.
func heldCharacters(unit format.Unit) []string {
	if unit.Checks.Syntax == format.SyntaxPrintf {
		return []string{"|", `\`, "%"}
	}
	return []string{"|", `\`}
}

// occurrences is one entry per occurrence of a character, for compare to count.
func occurrences(s, character string) []string {
	return slices.Repeat([]string{character}, strings.Count(s, character))
}

// withoutTokens is text with the wire's tokens taken out.
func withoutTokens(text string, tokens []string) string {
	for _, token := range tokens {
		text = strings.ReplaceAll(text, token, "")
	}
	return text
}

func distinct(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

// joined reports every problem at once. A rejected item is retried with the
// complaint attached, and naming only the first leaves the model fixing one
// thing while another stays broken: @:brand.name written as {site_name} is an
// invented placeholder and a dropped reference, and a model told only about
// the placeholder removes it and leaves the reference gone.
func joined(errs ...error) error {
	var problems []string
	for _, err := range errs {
		if err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}

// placeholders are a message's value substitutions, quoted with their braces:
// a complaint is read back by the model, and a name shown bare — "source has
// [n]" — is not reliably read as the {n} in the string it renamed.
func placeholders(s string) []string {
	names := submatches(icuArgument, s)
	for i, name := range names {
		names[i] = "{" + name + "}"
	}
	return names
}

// placeholdersPreserved requires every value the caller substitutes, as a set:
// a sentence may put them where it needs them and name one twice — Swedish
// writes {name} again where English says "them" — and may neither drop nor
// invent one. The renderer substitutes by name, so a repeat renders; unlike a
// wire token, which is restored by literal replacement and counted.
func placeholdersPreserved(source, candidate string) error {
	return compare("placeholder", distinct(placeholders(source)), distinct(placeholders(candidate)), false)
}

// placeholdersInExactForm holds the form of a category a single count selects.
// The count is a constant there, and the singular and dual forms of several
// languages write it as a word or not at all — "نتيجة واحدة", one result — so
// one placeholder may be missing. Every other placeholder still varies with
// the data, and nothing may be invented.
func placeholdersInExactForm(source, candidate string) error {
	want := distinct(placeholders(source))
	got := distinct(placeholders(candidate))
	for _, name := range got {
		if !slices.Contains(want, name) {
			return fmt.Errorf("placeholder mismatch: %s is not in the source [%s]", name, strings.Join(want, " "))
		}
	}
	var dropped []string
	for _, name := range want {
		if !slices.Contains(got, name) {
			dropped = append(dropped, name)
		}
	}
	if len(dropped) > 1 {
		sort.Strings(dropped)
		return fmt.Errorf("placeholder mismatch: this form renders for one number, so the placeholder holding that number may be left out — but not %s", strings.Join(dropped, " and "))
	}
	return nil
}

// voidTags are the HTML elements that take no closing tag.
var voidTags = map[string]bool{"br": true, "hr": true, "img": true, "input": true, "meta": true, "link": true}

// wellNested requires a candidate's tags to close in the order they opened:
// an element may move, a closing tag may not drift across another element's.
func wellNested(s string) error {
	var open []string
	for _, tag := range htmlTag.FindAllString(s, -1) {
		inner := strings.Trim(tag, "<>")
		name := strings.ToLower(strings.Fields(strings.Trim(inner, "/ "))[0])
		switch {
		case voidTags[name] || strings.HasSuffix(inner, "/"):
		case strings.HasPrefix(inner, "/"):
			if len(open) == 0 || open[len(open)-1] != name {
				return fmt.Errorf("HTML tags are not nested properly: </%s> closes nothing that is open", name)
			}
			open = open[:len(open)-1]
		default:
			open = append(open, name)
		}
	}
	if len(open) > 0 {
		return fmt.Errorf("HTML tags are not nested properly: <%s> is never closed", open[len(open)-1])
	}
	return nil
}

// markersInTags requires every attribute marker to stay in its tag, where it
// is restored: counted with the tag, one written in the text is not.
func markersInTags(source, candidate string) error {
	outside := func(s string) []string { return strayMarker.FindAllString(htmlTag.ReplaceAllString(s, ""), -1) }
	if sameTokens(outside(source), outside(candidate), false) {
		return nil
	}
	return fmt.Errorf("attribute marker outside its tag: %s belongs only inside its HTML tag, as the source writes it — write it nowhere else",
		strings.Join(outside(candidate), " "))
}

// validateICUPlural holds an ICU plural to the locale's own categories. An ICU
// message names its cases, so the set of names must be the locale's set.
func validateICUPlural(unit format.Unit, candidate string, categories []string) error {
	if !icuPlural(unit) {
		return nil
	}
	got := submatches(pluralKeyword, candidate)
	if sameTokens(categories, got, false) {
		return nil
	}
	// Both lists as they stand: the locale's in its own order, which the prompt
	// asks for, and the candidate's as written.
	return fmt.Errorf("a plural message in this locale names the cases %s, in that order; this one names %s", strings.Join(categories, ", "), strings.Join(got, ", "))
}

// splitForms cuts a form list on `|`, trimming each.
func splitForms(list string) []string {
	forms := strings.Split(list, "|")
	for i, form := range forms {
		forms[i] = strings.TrimSpace(form)
	}
	return forms
}

// formCount requires exactly one form per category of the locale.
func formCount(forms, categories []string) error {
	if len(forms) != len(categories) {
		return fmt.Errorf("a plural message in this locale has %d form(s) separated by `|` (%s), got %d",
			len(categories), strings.Join(categories, ", "), len(forms))
	}
	return nil
}

// compare holds a translation to the source's tokens, as a sequence where
// order is meaning (heading levels) and as a multiset where it is not (a
// sentence may legitimately reorder its placeholders and its elements).
func compare(what string, source, candidate []string, ordered bool) error {
	if sameTokens(source, candidate, ordered) {
		return nil
	}
	// The complaint is what a retry is given to work from, so each case says
	// what to do rather than tabling two lists: an empty one reads as nothing
	// in particular, and a model that invented a placeholder to complete a
	// sentence its language cannot end on ("About" → "O {site_name}") will
	// invent it again when told only that the source has [].
	switch {
	case len(source) == 0:
		return fmt.Errorf("%s mismatch: the source has none, so the translation can have none — write it without %s, wording it so the language does not need one", what, strings.Join(candidate, " "))
	case len(candidate) == 0:
		return fmt.Errorf("%s mismatch: the translation has none — reproduce %s exactly as the source writes it", what, strings.Join(source, " "))
	}
	return fmt.Errorf("%s mismatch: the source has [%s] and the translation [%s] — reproduce the source's exactly", what, strings.Join(source, " "), strings.Join(candidate, " "))
}

// sameTokens reports whether two token lists agree, as sequences or as
// multisets.
func sameTokens(a, b []string, ordered bool) bool {
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	if !ordered {
		sort.Strings(x)
		sort.Strings(y)
	}
	return strings.Join(x, "\x00") == strings.Join(y, "\x00")
}

func submatches(re *regexp.Regexp, s string) []string {
	var out []string
	for _, match := range re.FindAllStringSubmatch(s, -1) {
		out = append(out, match[1])
	}
	return out
}

// normalisedTags reduces each tag to its name and its attributes in a fixed
// order, so an attribute reordering is not a failure but a dropped `href` is.
func normalisedTags(s string) []string {
	var out []string
	for _, tag := range htmlTag.FindAllString(s, -1) {
		inner := strings.Trim(tag, "<>/ ")
		fields := strings.Fields(inner)
		if len(fields) == 0 {
			continue
		}
		rest := append([]string(nil), fields[1:]...)
		sort.Strings(rest)
		out = append(out, strings.ToLower(fields[0])+" "+strings.Join(rest, " "))
	}
	return out
}

// listKinds reduces list markers to their kind — bullet or ordered — because a
// translation may renumber an ordered list but must not turn a list into prose.
func listKinds(s string) []string {
	var out []string
	for _, marker := range submatches(listMarker, s) {
		if strings.HasSuffix(marker, ".") {
			out = append(out, "ordered")
			continue
		}
		out = append(out, "bullet")
	}
	return out
}

// listNumerals requires a numbered list's markers in ASCII digits, the only
// ones markdown reads as a list: numbered १. or ١. in a language's own digits,
// the list renders as a paragraph.
func listNumerals(candidate string) error {
	for _, marker := range submatches(listMarker, candidate) {
		if number, ordered := strings.CutSuffix(marker, "."); ordered && strings.Trim(number, "0123456789") != "" {
			return fmt.Errorf("list numbered %s: markdown reads a list only when it is numbered in ASCII digits — write 1., 2., 3. whatever digits the language otherwise writes", marker)
		}
	}
	return nil
}
