// Package negotiate answers which shipped locale a requested tag resolves to —
// for a server resolving every request's locale, and, through the table it
// generates, for a browser resolving its own before anything else loads. One
// oracle, so the two can never disagree.
package negotiate

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/text/language"
)

// Locale negotiation: one oracle, one acceptance rule, and a table generated
// from it for the frontend.
//
// The knowledge a correct match takes — that MX is inside 419, that zh-TW
// implies Hant, that `no` and `tl` are `nb` and `fil` — lives in x/text's
// matcher. The browser cannot reproduce it (Intl.Locale.maximize turns es-MX
// into es-Latn-MX, which is not es-Latn-419, and whether an engine folds tl
// into fil is the engine's business), so the frontend reads the matcher's
// verdicts out of a generated table rather than deriving its own.

// Negotiator matches a requested tag against the shipped set.
type Negotiator struct {
	matcher language.Matcher
	shipped []string
	def     string
}

// New builds the oracle over one shipped set. The default locale must be among
// them — it is the answer whenever nothing matches.
func New(shipped []string, def string) (*Negotiator, error) {
	if len(shipped) == 0 {
		return nil, fmt.Errorf("negotiate: no shipped locales")
	}
	tags := make([]language.Tag, 0, len(shipped))
	for _, locale := range shipped {
		tag, err := language.Parse(locale)
		if err != nil {
			return nil, fmt.Errorf("negotiate: shipped locale %q: %w", locale, err)
		}
		// Canonical form, as every other reader of a locale tag demands: the
		// table is keyed by these strings and compared against them, so an
		// `en_US` that parses but does not round-trip would key a row nothing
		// ever looks up.
		if tag.String() != locale {
			return nil, fmt.Errorf("negotiate: shipped locale %q is not canonical BCP-47 (%q is)", locale, tag.String())
		}
		tags = append(tags, tag)
	}
	found := false
	for _, locale := range shipped {
		if locale == def {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("negotiate: default locale %q is not shipped", def)
	}
	return &Negotiator{matcher: language.NewMatcher(tags), shipped: shipped, def: def}, nil
}

// Match resolves a requested tag to a shipped locale.
//
// A match is accepted when the matcher is certain (Exact), or confident and
// talking about the same language (High with the same base). The second
// condition is the one that matters: the matcher happily offers a *neighbouring*
// language at High confidence — with `nb` shipped it answers a Danish request
// with Norwegian, with `es-ES` shipped it answers a Galician one with Spanish —
// and a language the viewer did not ask for is not a better answer than the
// default. Everything else, the default.
func (n *Negotiator) Match(requested string) string {
	if strings.TrimSpace(requested) == "" {
		return n.def
	}
	desired, err := language.Parse(requested)
	if err != nil {
		// Not a tag at all: a caller with no usable preference, not a fault.
		return n.def
	}
	_, index, confidence := n.matcher.Match(desired)
	if index < 0 || index >= len(n.shipped) {
		return n.def
	}
	matched := n.shipped[index]
	switch confidence {
	case language.Exact:
		return matched
	case language.High:
		if sameBase(desired, matched) {
			return matched
		}
	}
	return n.def
}

func sameBase(desired language.Tag, matched string) bool {
	other, err := language.Parse(matched)
	if err != nil {
		return false
	}
	a, _ := desired.Base()
	b, _ := other.Base()
	return a == b
}

// LanguageEntry is one language's verdicts: where the bare code lands, and
// which scripts and regions land somewhere else. Only differences are
// recorded — a region that resolves to Default says nothing the default does
// not already say.
type LanguageEntry struct {
	Default  string            `json:"default"`
	ByScript map[string]string `json:"by_script,omitempty"`
	ByRegion map[string]string `json:"by_region,omitempty"`
}

// LanguageTable is the frontend's copy of the oracle, keyed by language subtag.
// A tag whose language is absent resolves to the default locale; present, the
// rule is exact shipped tag, then by_script, then by_region, then Default.
type LanguageTable map[string]LanguageEntry

// scriptProbes are the scripts worth asking about for one language: the ones
// its own shipped tags state.
//
// Per language, not per product, and stated rather than inferred. A language
// whose siblings differ by region has no script axis at all, and giving it a
// script map would make a tag that merely names the script the language is
// written in — `es-Latn-MX` — take the script branch and never reach its
// region. `de` is Latn by inference and states nothing, so it too gets no map.
func scriptProbes(shipped []string, lang string) []string {
	seen := map[string]bool{}
	var out []string
	for _, locale := range shipped {
		tag, err := language.Parse(locale)
		if err != nil {
			continue
		}
		if base, _ := tag.Base(); base.String() != lang {
			continue
		}
		script, confidence := tag.Script()
		if confidence == language.Exact && !seen[script.String()] {
			seen[script.String()] = true
			out = append(out, script.String())
		}
	}
	sort.Strings(out)
	return out
}

// regionProbes is every region code a browser could send: the two-letter
// countries and the three-digit macro-regions (419 Latin America, 150 Europe),
// which is how a request reaches a sibling whose tag names a macro-region.
func regionProbes() []string {
	var out []string
	for a := 'A'; a <= 'Z'; a++ {
		for b := 'A'; b <= 'Z'; b++ {
			out = append(out, string([]rune{a, b}))
		}
	}
	for n := 0; n <= 999; n++ {
		out = append(out, fmt.Sprintf("%03d", n))
	}
	return out
}

// languageProbes is every two- and three-letter language subtag. Probing them
// all is what finds the aliases without a hand-kept list: `no`, `tl`, `iw`,
// `in` and the rest resolve onto a shipped language through the matcher's own
// canonicalisation, and a list maintained here would be a second oracle.
func languageProbes() []string {
	var out []string
	for a := 'a'; a <= 'z'; a++ {
		for b := 'a'; b <= 'z'; b++ {
			out = append(out, string([]rune{a, b}))
			for c := 'a'; c <= 'z'; c++ {
				out = append(out, string([]rune{a, b, c}))
			}
		}
	}
	return out
}

// Table generates the frontend's lookup table by asking the oracle every
// question a browser could ask.
func (n *Negotiator) Table() LanguageTable {
	table := LanguageTable{}
	regions := regionProbes()

	for _, lang := range languageProbes() {
		// `und` is the absence of a language, which the frontend's "no entry"
		// branch already answers.
		if lang == "und" {
			continue
		}
		resolved := n.Match(lang)
		if resolved == n.def && !n.shippedIs(lang, resolved) {
			// The language reaches nothing of its own; the frontend's "no
			// entry ⇒ default" covers it without a row.
			continue
		}
		entry := LanguageEntry{Default: resolved}
		for _, script := range scriptProbes(n.shipped, Language(resolved)) {
			if got := n.Match(lang + "-" + script); got != resolved {
				if entry.ByScript == nil {
					entry.ByScript = map[string]string{}
				}
				entry.ByScript[script] = got
			}
		}
		for _, region := range regions {
			if got := n.Match(lang + "-" + region); got != resolved {
				if entry.ByRegion == nil {
					entry.ByRegion = map[string]string{}
				}
				entry.ByRegion[region] = got
			}
		}
		table[lang] = entry
	}
	return table
}

// shippedIs reports whether resolving lang to the default was a real match
// rather than the fallback — the default locale's own language must still get
// a row, or an en browser would fall through the frontend's "no entry" branch
// to the same answer by accident rather than by rule.
func (n *Negotiator) shippedIs(lang, resolved string) bool {
	tag, err := language.Parse(lang)
	if err != nil {
		return false
	}
	return sameBase(tag, resolved)
}

// Language is a tag's language, canonicalised. Canonical rather than the text
// before the first hyphen, because a content language and a locale can name the
// same language differently — a detector says `tl`, the shipped locale is `fil`
// — and the whole point of comparing languages is to find out whether they are
// the same one.
func Language(tag string) string {
	parsed, err := language.Parse(tag)
	if err != nil {
		return ""
	}
	base, _ := parsed.Base()
	return base.String()
}
