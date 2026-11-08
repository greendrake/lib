package format

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"greendrake.nz/go/i18n/plurals"
)

// A plural a source keys by CLDR category — an Android <plurals>, a String
// Catalog's plural variation — is one unit whose text is its forms written as
// one JSON object, keys in CLDR's order: `{"one":"%d song","other":"%d songs"}`.
// Any form, line breaks and all, decodes back exactly, so the digests, the
// state and everything the engine does with a text stay text-based. Its
// translation is written the same way, in the target locale's categories.

// EncodeForms writes a keyed plural's forms as its text.
func EncodeForms(forms map[string]string) string {
	var out strings.Builder
	out.WriteByte('{')
	for _, category := range plurals.All() {
		form, ok := forms[category]
		if !ok {
			continue
		}
		if out.Len() > 1 {
			out.WriteByte(',')
		}
		out.WriteString(quoted(category) + ":" + quoted(form))
	}
	out.WriteByte('}')
	return out.String()
}

// quoted is a string as JSON writes it, markup as it is written.
func quoted(s string) string {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(s); err != nil {
		panic(fmt.Sprintf("format: encode %q: %v", s, err))
	}
	return strings.TrimSuffix(out.String(), "\n")
}

// DecodeForms reads a keyed plural's text back into its forms.
func DecodeForms(text string) (map[string]string, error) {
	var forms map[string]string
	if err := json.Unmarshal([]byte(text), &forms); err != nil {
		return nil, fmt.Errorf("not a plural's forms: %w", err)
	}
	return forms, categorised(forms)
}

// pluralUnit makes unit the keyed plural of forms, which must give `other`:
// the form every count beyond a locale's own distinctions takes.
func pluralUnit(unit Unit, forms map[string]string) (Unit, error) {
	if _, ok := forms["other"]; !ok {
		return Unit{}, fmt.Errorf("%s: the plural has no `other` form", unit.ID)
	}
	if err := categorised(forms); err != nil {
		return Unit{}, fmt.Errorf("%s: %w", unit.ID, err)
	}
	unit.Forms, unit.Text = forms, EncodeForms(forms)
	return unit, nil
}

// categorised refuses a form keyed by anything but a CLDR category.
func categorised(forms map[string]string) error {
	all := plurals.All()
	for category := range forms {
		if !slices.Contains(all, category) {
			return fmt.Errorf("plural form %q is no CLDR category", category)
		}
	}
	return nil
}
