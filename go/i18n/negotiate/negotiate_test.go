package negotiate

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shipped set the negotiation pairs are stated against: siblings by script
// (zh), by macro-region (es), by region (pt), plus two single-tag languages
// that the matcher likes to offer as neighbours (nb for Danish, fil for tl).
var negotiationShipped = []string{"en-US", "de", "fr", "ru", "zh-Hans", "zh-Hant", "es-ES", "es-419", "pt-BR", "pt-PT", "nb", "fil", "sr"}

func testNegotiator(t *testing.T) *Negotiator {
	t.Helper()
	n, err := New(negotiationShipped, "en-US")
	require.NoError(t, err)
	return n
}

func TestNegotiator_Match(t *testing.T) {
	n := testNegotiator(t)
	cases := []struct{ requested, want string }{
		// Script inferred from region — a browser never sends zh-Hant.
		{"zh-TW", "zh-Hant"},
		{"zh-HK", "zh-Hant"},
		{"zh-SG", "zh-Hans"},
		{"zh-CN", "zh-Hans"},
		{"zh", "zh-Hans"},
		// Macro-region membership: MX is inside 419, ES is not.
		{"es-MX", "es-419"},
		{"es-AR", "es-419"},
		{"es-ES", "es-ES"},
		{"es", "es-ES"},
		// Region falls to the sibling that covers it.
		{"pt-AO", "pt-PT"},
		{"pt-BR", "pt-BR"},
		// Same language, different region: still that language.
		{"en-GB", "en-US"},
		{"de-AT", "de"},
		{"ru-BY", "ru"},
		// Aliases the matcher canonicalises and a bare string compare misses.
		{"no", "nb"},
		{"tl", "fil"},
		{"tl-PH", "fil"},
		// A neighbouring language is not the viewer's language.
		{"da", "en-US"},
		{"nn", "en-US"},
		{"gl", "en-US"},
		{"sh", "en-US"},
		// Nothing shipped speaks these.
		{"ja", "en-US"},
		{"sw", "en-US"},
		// No preference at all.
		{"", "en-US"},
		{"not a tag", "en-US"},
	}
	for _, tc := range cases {
		t.Run(tc.requested, func(t *testing.T) {
			assert.Equal(t, tc.want, n.Match(tc.requested))
		})
	}
}

func TestNew_Rejections(t *testing.T) {
	_, err := New(nil, "en-US")
	assert.ErrorContains(t, err, "no shipped locales")

	_, err = New([]string{"en-US"}, "de")
	assert.ErrorContains(t, err, "is not shipped")

	_, err = New([]string{"en_US"}, "en_US")
	assert.ErrorContains(t, err, "is not canonical BCP-47")

	_, err = New([]string{"not a tag"}, "not a tag")
	assert.ErrorContains(t, err, "shipped locale")
}

// The generated table must answer exactly as the oracle does — it is the
// frontend's only copy of it, and the rule it applies is the one asserted here.
func TestNegotiator_TableAgreesWithOracle(t *testing.T) {
	n := testNegotiator(t)
	table := n.Table()

	// The frontend's rule, reproduced exactly: language entry or the default,
	// then an exact shipped tag, then a script the tag states (which settles it
	// — the region is not consulted after one), then a region, then the entry's
	// default.
	lookup := func(tag string) string {
		subtags := strings.Split(tag, "-")
		entry, ok := table[subtags[0]]
		if !ok {
			return "en-US"
		}
		for _, shipped := range negotiationShipped {
			if shipped == tag {
				return shipped
			}
		}
		var script, region string
		for _, subtag := range subtags[1:] {
			switch {
			case scriptSubtag.MatchString(subtag):
				script = subtag
			case regionSubtag.MatchString(subtag):
				region = subtag
			}
		}
		if script != "" && entry.ByScript != nil {
			if got, ok := entry.ByScript[script]; ok {
				return got
			}
			return entry.Default
		}
		if got, ok := entry.ByRegion[region]; ok {
			return got
		}
		return entry.Default
	}

	for _, tag := range []string{"zh-TW", "zh-HK", "zh-SG", "zh-CN", "zh", "es-MX", "es-AR", "es-ES", "es", "pt-AO", "pt-BR", "en-GB", "de-AT", "ru-BY", "no", "tl", "da", "nn", "gl", "ja", "sw",
		// Qualified twice, where a rule that reads one qualifier would agree
		// with the matcher by accident on every tag above and disagree here.
		"zh-Hant-TW", "zh-Hans-TW", "zh-Hant-CN", "pt-Latn-AO", "es-Latn-MX"} {
		assert.Equal(t, n.Match(tag), lookup(tag), "table and oracle disagree on %s", tag)
	}
}

// Subtags as BCP-47 writes them, the same shapes the frontend's rule matches.
var (
	scriptSubtag = regexp.MustCompile(`^[A-Z][a-z]{3}$`)
	regionSubtag = regexp.MustCompile(`^([A-Z]{2}|[0-9]{3})$`)
)

// The default locale's own language needs a row: without one an `en` browser
// would reach the right answer through the frontend's "unknown language"
// branch rather than through a stated verdict, and adding a second English
// locale would silently keep answering en-US.
func TestNegotiator_TableCoversShippedLanguages(t *testing.T) {
	table := testNegotiator(t).Table()
	for _, lang := range []string{"en", "de", "fr", "ru", "zh", "es", "pt", "nb", "no", "fil", "tl", "sr"} {
		assert.Contains(t, table, lang, "language %s has no row", lang)
	}
	for _, lang := range []string{"da", "gl", "ja", "sw"} {
		assert.NotContains(t, table, lang, "language %s should fall through to the default", lang)
	}
}

// A script map exists only where a script actually distinguishes two shipped
// siblings. Given one where it does not, a tag naming the script the language
// is merely written in would take the script branch and never reach the region
// that decides it.
func TestNegotiator_ScriptMapOnlyWhereScriptDistinguishes(t *testing.T) {
	table := testNegotiator(t).Table()
	assert.NotEmpty(t, table["zh"].ByScript, "zh ships Hans and Hant")
	assert.Empty(t, table["es"].ByScript, "es ships two regions, not two scripts")
	assert.Empty(t, table["pt"].ByScript)
	assert.Empty(t, table["de"].ByScript)
	// The default's own script is not recorded: it is what Default already says.
	assert.NotContains(t, table["zh"].ByScript, "Hans")
	assert.Equal(t, "zh-Hant", table["zh"].ByScript["Hant"])
}
