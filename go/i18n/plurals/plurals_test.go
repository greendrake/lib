package plurals

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCategories(t *testing.T) {
	cases := map[string][]string{
		"en":    {"one", "other"},
		"de":    {"one", "other"},
		"fr":    {"one", "other"},
		"ru":    {"one", "few", "many", "other"},
		"cs":    {"one", "few", "other"},
		"ja":    {"other"},
		"ar":    {"zero", "one", "two", "few", "many", "other"},
		"he":    {"one", "two", "many", "other"},
		"pl":    {"one", "few", "many", "other"},
		"en-US": {"one", "other"},
	}
	for locale, want := range cases {
		t.Run(locale, func(t *testing.T) {
			assert.Equal(t, want, Categories(locale))
		})
	}
}

// All is CLDR's six categories in its order, a fresh copy each call.
func TestAll(t *testing.T) {
	assert.Equal(t, []string{"zero", "one", "two", "few", "many", "other"}, All())
	all := All()
	all[0] = "changed"
	assert.Equal(t, "zero", All()[0], "each call is its own copy")
}

// A form is asked for with the numbers that select it, so the examples have to
// be the ones a reader of that language recognises at a glance: 1 for
// Russian's `one` (not 1, 21, 31, which reads as plural), 2, 3, 4 for `few`,
// 5, 6, 7 for `many`, and fractions for the `other` no integer reaches.
func TestExamples(t *testing.T) {
	ru := Examples("ru")
	assert.Equal(t, []string{"1"}, ru["one"])
	assert.Equal(t, []string{"2", "3", "4"}, ru["few"])
	assert.Equal(t, []string{"5", "6", "7"}, ru["many"])
	assert.Equal(t, []string{"1.5", "2.5", "3.5"}, ru["other"])

	de := Examples("de")
	assert.Equal(t, []string{"1"}, de["one"])
	assert.Equal(t, []string{"2", "3", "4"}, de["other"])

	assert.Equal(t, []string{"1", "2", "3"}, Examples("ja")["other"])
	assert.Equal(t, []string{"0"}, Examples("ar")["zero"])
	assert.Equal(t, []string{"2"}, Examples("ar")["two"])
}

// A category a single count selects renders for that number and no other,
// which is what lets a translation write it as a word — Arabic's dual — or
// leave it out. Russian's `one` takes 1, 21 and 31, so it is not one of them.
func TestExact(t *testing.T) {
	assert.Equal(t, map[string]string{"zero": "0", "one": "1", "two": "2"}, Exact("ar"))
	assert.Equal(t, map[string]string{"one": "1"}, Exact("de"))
	assert.Empty(t, Exact("ru"))
	assert.Empty(t, Exact("ja"), "one category, every number")
	assert.Empty(t, Exact("fr")["one"], "French `one` takes 0 and 1")
}
