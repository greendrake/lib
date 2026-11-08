package format

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A keyed plural's text is its forms in CLDR's order, markup as written and a
// line break decoding back as one.
func TestForms_RoundTrip(t *testing.T) {
	forms := map[string]string{"other": "<b>%d</b> songs\nlisted", "one": "%d song", "few": `"%d" & co`}
	text := EncodeForms(forms)
	assert.Equal(t, `{"one":"%d song","few":"\"%d\" & co","other":"<b>%d</b> songs\nlisted"}`, text)
	back, err := DecodeForms(text)
	require.NoError(t, err)
	assert.Equal(t, forms, back)
	assert.Equal(t, "{}", EncodeForms(nil))

	_, err = DecodeForms(`{"several":"x"}`)
	assert.EqualError(t, err, `plural form "several" is no CLDR category`)
	_, err = DecodeForms(`one item`)
	assert.ErrorContains(t, err, "not a plural's forms")
}
