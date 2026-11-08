package catalogue

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"greendrake.nz/go/i18n/locales"
)

// files is a well-formed catalogue: the default, a translation overriding part
// of it, and one carrying none of the rendered subtrees. `app.*` and
// `pages.about.*` are the frontend's, so the `|` in one of them is no fault.
func files() map[string]string {
	return map[string]string{
		"en-GB": `
app:
  greeting: Hi {name} | Hello {name}
seo:
  available: Available
  count: '{count, plural, one {# listing} other {# listings}}'
  root_title: '{site_name} — the noticeboard'
  nested:
    up: Up
pages:
  home:
    title: Home
  about:
    title: About
`,
		"de": `
seo:
  count: '{count, plural, one {# Anzeige} other {# Anzeigen}}'
`,
		"fr": "app:\n  greeting: Salut\n",
	}
}

// shipped is a locale set whose default is its first locale.
func shipped(t *testing.T, tags ...string) *locales.Set {
	t.Helper()
	set, err := locales.New(tags, tags[0])
	require.NoError(t, err)
	return set
}

// options writes a catalogue into a fresh directory and returns what loads it.
func options(t *testing.T, catalogue map[string]string) Options {
	t.Helper()
	dir := t.TempDir()
	for locale, body := range catalogue {
		require.NoError(t, os.WriteFile(filepath.Join(dir, locale+".yaml"), []byte(body), 0o644))
	}
	return Options{
		Dir:       dir,
		Locales:   shipped(t, "en-GB", "de", "fr"),
		Prefixes:  []string{"seo", "pages.home"},
		Constants: map[string]string{"site_name": "Acme"},
	}
}

func load(t *testing.T, opts Options) *Catalogue {
	t.Helper()
	c, err := Load(opts)
	require.NoError(t, err)
	return c
}

func message(t *testing.T, c *Catalogue, locale, key string, args map[string]any) string {
	t.Helper()
	text, err := c.Message(locale, key, args)
	require.NoError(t, err)
	return text
}

// Each locale is the default overlaid with its own messages, rendered under
// its own tag; a locale nothing ships renders nothing.
func TestMessage_MergesEachLocaleOverTheDefault(t *testing.T) {
	c := load(t, options(t, files()))

	assert.Equal(t, "Available", message(t, c, "en-GB", "seo.available", nil))
	// German grouping: the edition renders under its own locale.
	assert.Equal(t, "1.200 Anzeigen", message(t, c, "de", "seo.count", map[string]any{"count": 1200}))
	// A key the translation lacks reads in the default language.
	assert.Equal(t, "Available", message(t, c, "de", "seo.available", nil))
	// So does a whole subtree it lacks.
	assert.Equal(t, "Up", message(t, c, "fr", "seo.nested.up", nil))
	_, err := c.Message("xx-XX", "seo.count", map[string]any{"count": 1})
	assert.ErrorContains(t, err, "not shipped")
	// A dotted prefix renders its subtree and nothing beside it.
	assert.Equal(t, "Home", message(t, c, "en-GB", "pages.home.title", nil))

	// Keys outside the prefixes are the frontend's and are not loaded.
	for _, key := range []string{"pages.about.title", "app.greeting"} {
		_, err := c.Message("en-GB", key, nil)
		assert.Error(t, err, key)
	}
}

// The constants reach every render under the call's own arguments.
func TestMessage_ConstantsUnderArgs(t *testing.T) {
	opts := options(t, files())
	c := load(t, opts)
	assert.Equal(t, "Acme — the noticeboard", message(t, c, "de", "seo.root_title", nil))
	assert.Equal(t, "Other — the noticeboard", message(t, c, "de", "seo.root_title", map[string]any{"site_name": "Other"}))

	opts.Constants = nil
	_, err := load(t, opts).Message("de", "seo.root_title", nil)
	assert.Error(t, err, "without the constant the message lacks its argument")
}

// Both failure modes are returned, never degraded into the output: an unknown
// key and an argument the message cannot render against (a count that is not
// an int is the one a call site can reach by accident).
func TestMessage_ReturnsFailures(t *testing.T) {
	c := load(t, options(t, files()))
	_, err := c.Message("en-GB", "seo.no_such_key", nil)
	assert.Error(t, err)
	_, err = c.Message("en-GB", "seo.count", map[string]any{"count": int64(2)})
	assert.Error(t, err)
}

func TestLoad_Rejections(t *testing.T) {
	cases := map[string]func(catalogue map[string]string){
		"vue-i18n plural in the default": func(c map[string]string) {
			c["en-GB"] = "seo:\n  available: one | many\npages:\n  home:\n    title: Home\n"
		},
		"linked message in a translation": func(c map[string]string) {
			c["de"] = "seo:\n  count: '@:seo.available'\n"
		},
		"malformed ICU": func(c map[string]string) {
			c["de"] = "seo:\n  count: '{count, plural, one {# Anzeige}'\n"
		},
		"a value that is not a message": func(c map[string]string) {
			c["fr"] = "seo:\n  nested:\n    up: 5\n"
		},
		"a prefix the default defines nothing under": func(c map[string]string) {
			c["en-GB"] = "seo:\n  available: Available\n"
		},
		"a shipped locale without a file": func(c map[string]string) {
			delete(c, "fr")
		},
		"not YAML": func(c map[string]string) {
			c["de"] = "seo: [\n"
		},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			catalogue := files()
			edit(catalogue)
			_, err := Load(options(t, catalogue))
			assert.Error(t, err)
		})
	}
}

// The fingerprint moves with everything a render reads, and with nothing else.
func TestFingerprint(t *testing.T) {
	base := load(t, options(t, files())).Fingerprint()
	assert.NotEmpty(t, base)
	assert.Equal(t, base, load(t, options(t, files())).Fingerprint())

	edited := func(locale, body string) map[string]string {
		catalogue := files()
		catalogue[locale] = body
		return catalogue
	}
	// A string the backend renders.
	assert.NotEqual(t, base, load(t, options(t, edited("de", "seo:\n  count: '{count, plural, other {# Anzeigen}}'\n"))).Fingerprint())
	// A string it does not.
	assert.Equal(t, base, load(t, options(t, edited("fr", "app:\n  greeting: Bonjour\n"))).Fingerprint())

	opts := options(t, files())
	opts.Constants = map[string]string{"site_name": "Other"}
	assert.NotEqual(t, base, load(t, opts).Fingerprint(), "a constant")

	opts = options(t, files())
	opts.Locales = shipped(t, "en-GB", "de")
	assert.NotEqual(t, base, load(t, opts).Fingerprint(), "the shipped set")
}
