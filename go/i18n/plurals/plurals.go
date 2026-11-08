// Package plurals answers which plural categories a locale's messages carry,
// and which numbers select each — derived once, from the CLDR tables
// messageformat renders with, for every reader that needs them: a translator
// telling a model how many forms to write, a validator counting them, and a
// browser that is handed the list rather than deriving its own.
//
// The derivation has to be the renderer's: messageformat selects among
// categories with x/text's CLDR tables, so a translation carrying any other set
// of categories has forms the renderer can never reach and lacks ones it will.
// The browser's CLDR is newer and disagrees on a few locales (Hebrew lost
// `many` after the release x/text carries), which is exactly why a frontend is
// handed this list instead of computing one.
package plurals

import (
	"fmt"
	"strconv"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
)

// canonicalOrder is the order CLDR states the categories in, and therefore the
// order a translated plural message writes its forms in. Any other order would
// be a convention someone has to remember.
var canonicalOrder = []plural.Form{plural.Zero, plural.One, plural.Two, plural.Few, plural.Many, plural.Other}

var formNames = map[plural.Form]string{
	plural.Zero:  "zero",
	plural.One:   "one",
	plural.Two:   "two",
	plural.Few:   "few",
	plural.Many:  "many",
	plural.Other: "other",
}

// All returns every CLDR plural category, in canonical order.
func All() []string {
	out := make([]string, len(canonicalOrder))
	for i, form := range canonicalOrder {
		out[i] = formNames[form]
	}
	return out
}

// pluralProbeMax is the range of counts the derivation walks. Every category a
// locale reaches for an integer count is reached well inside it — the rules
// that distinguish categories look at the last one or two digits and at
// magnitude — and the counts these messages carry are integers: unread
// messages, listings in a hub, results in a search.
const pluralProbeMax = 1000

// Categories returns the plural categories a locale distinguishes, in
// canonical order, always ending in `other`.
//
// `other` is added whether or not an integer reaches it: CLDR makes it the
// mandatory fallback, and it is where the frontend sends a category this list
// does not name — a fractional count in a locale whose integers never produce
// one. A locale whose integers reach only `other` therefore gets exactly one
// form, which is right for Japanese.
func Categories(locale string) []string {
	tag := language.Make(locale)
	reached := map[plural.Form]bool{plural.Other: true}
	for n := 0; n <= pluralProbeMax; n++ {
		reached[plural.Cardinal.MatchPlural(tag, n, 0, 0, 0, 0)] = true
	}
	var out []string
	for _, form := range canonicalOrder {
		if reached[form] {
			out = append(out, formNames[form])
		}
	}
	return out
}

// Exact names the categories a single count selects, and that count: Arabic's
// `one` is 1 and nothing else, its `two` is 2, while Russian's `one` is 1, 21,
// 31 and so on. A form for one of these renders for a single number, which is
// what lets a translation write that number as a word — "نتيجة واحدة", one
// result — or leave it out, as the singular and dual forms of several
// languages do. A category any fraction reaches is not one of them: the number
// is no longer constant.
func Exact(locale string) map[string]string {
	tag := language.Make(locale)
	integers := map[string][]int{}
	fractional := map[string]bool{}
	for n := 0; n <= pluralProbeMax; n++ {
		name := formNames[plural.Cardinal.MatchPlural(tag, n, 0, 0, 0, 0)]
		integers[name] = append(integers[name], n)
		// n.5: one visible fraction digit, 5.
		fractional[formNames[plural.Cardinal.MatchPlural(tag, n, 1, 1, 5, 5)]] = true
	}
	out := map[string]string{}
	for name, counts := range integers {
		if len(counts) == 1 && !fractional[name] {
			out[name] = strconv.Itoa(counts[0])
		}
	}
	return out
}

// exampleCount bounds how many numbers illustrate a category.
const exampleCount = 3

// Examples returns, per category, the numbers that select it — what a
// translator is told when asked for one form: "the form used when the count is
// 2, 3 or 4".
//
// They are the first consecutive run from 1, not the first three matches: the
// full truth of Russian's `one` is 1, 21, 31, and a translator told that reads
// 21 and 31 as plural — 1 alone says what is meant. A category no positive
// integer reaches is illustrated with the fractions that do (Russian's `other`)
// or with 0 (Arabic's `zero`).
func Examples(locale string) map[string][]string {
	tag := language.Make(locale)
	integer := func(n int) string { return formNames[plural.Cardinal.MatchPlural(tag, n, 0, 0, 0, 0)] }
	// i.5: one visible fraction digit, 5.
	fraction := func(i int) string { return formNames[plural.Cardinal.MatchPlural(tag, i, 1, 1, 5, 5)] }
	run := func(matches func(int) bool, render func(int) string) []string {
		var out []string
		for n := 1; n <= pluralProbeMax && len(out) < exampleCount; n++ {
			if matches(n) {
				out = append(out, render(n))
			} else if len(out) > 0 {
				break
			}
		}
		return out
	}
	out := map[string][]string{}
	for _, name := range Categories(locale) {
		examples := run(func(n int) bool { return integer(n) == name }, strconv.Itoa)
		if len(examples) == 0 {
			examples = run(func(i int) bool { return fraction(i) == name }, func(i int) string { return fmt.Sprintf("%d.5", i) })
		}
		if len(examples) == 0 && integer(0) == name {
			examples = []string{"0"}
		}
		out[name] = examples
	}
	return out
}
