package format

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"greendrake.nz/go/translate/internal/files"
)

var catalogSpec = Spec{
	Format: XCStrings,
	Path:   "Localizable.xcstrings",
	Syntax: SyntaxPrintf,
	Markup: MarkupNone,
}

// xcodeCatalog is testdata/xcode.xcstrings: a catalog as Xcode's own
// xcstringstool saved it, with a plural, device variations, substitutions, a
// key that is English text holding `@`, an entry with no localization, and
// entries the translator leaves alone.
func xcodeCatalog(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("testdata/xcode.xcstrings")
	require.NoError(t, err)
	return string(data)
}

func catalogStore(t *testing.T, catalog string) (Store, Env) {
	t.Helper()
	env := fixture(t, map[string]string{"Localizable.xcstrings": catalog})
	env.Default = "en"
	return open(t, "ios", catalogSpec, env), env
}

// digested is the unit key of a catalog key that is not an identifier.
func digested(key string) string { return "k" + files.Digest(key)[:12] }

// Every string of the catalog the translator owns is a unit — a key that is
// not an identifier addressed by its digest — its comment the context, its
// master the source language's localization or, without one, the key.
func TestXCStrings_Units(t *testing.T) {
	store, _ := catalogStore(t, xcodeCatalog(t))
	items, signIn := "ios:"+digested("%lld items"), "ios:"+digested("Sign in as @%@")
	units := store.Units()
	require.Equal(t, []string{
		items, signIn, "ios:Zebra",
		"ios:files.summary", "ios:files.summary%files", "ios:files.summary%folders",
		"ios:greeting",
		"ios:tap.hint#iphone", "ios:tap.hint#mac", "ios:tap.hint#other",
	}, ids(units))
	byID := map[string]Unit{}
	for _, unit := range units {
		byID[unit.ID] = unit
		assert.NotContains(t, unit.ID, "@", "no unit id holds the form separator")
	}

	assert.Equal(t, map[string]string{"one": "%lld item", "other": "%lld items\nin your cart"}, byID[items].Forms)
	assert.Equal(t, "How many items the cart holds.", byID[items].Context)
	assert.Equal(t, digested("%lld items"), byID[items].Bundle)
	assert.Equal(t, "Sign in as @%@", byID[signIn].Text, "the words reach the model as the text, not the id")
	assert.Equal(t, "Zebra", byID["ios:Zebra"].Text, "a key with no localization is its own text")
	assert.Equal(t, "%#@files@ in %#@folders@", byID["ios:files.summary"].Text)
	assert.Equal(t, map[string]string{"one": "%arg folder", "other": "%arg folders"}, byID["ios:files.summary%folders"].Forms)
	assert.Equal(t, "Hello, %@!", byID["ios:greeting"].Text)
	assert.Equal(t, "Greets the signed-in user by name.", byID["ios:greeting"].Context)
	assert.Equal(t, Checks{Syntax: SyntaxPrintf, Markup: MarkupNone, Shape: ShapeText}, byID["ios:greeting"].Checks)
	assert.Equal(t, "Click to open", byID["ios:tap.hint#mac"].Text)
	assert.Equal(t, map[string]string{"one": "Open %lld file", "other": "Open %lld files"}, byID["ios:tap.hint#other"].Forms)
	assert.Equal(t, "tap.hint", byID["ios:tap.hint#mac"].Bundle)
}

// What a locale already holds, written back, is the catalog Xcode saved, byte
// for byte.
func TestXCStrings_RewriteKeepsXcodesLayout(t *testing.T) {
	store, _ := catalogStore(t, xcodeCatalog(t))
	de, err := store.Existing("de", nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"ios:" + digested("%lld items"): EncodeForms(map[string]string{"one": "%lld Artikel", "other": "%lld Artikel\nim Warenkorb"}),
		"ios:greeting":                  "Hallo, %@!",
	}, de)
	compared, err := store.Write("de", de, files.Compare)
	require.NoError(t, err)
	assert.Empty(t, compared)
}

// A locale's write replaces its localization of every entry whose units are
// all translated, each value marked translated and in its master's shape —
// a substitution's argument and format carried over — and leaves the rest of
// the catalog as it was. A de-listed locale's part is dropped from it.
func TestXCStrings_WriteAndDrop(t *testing.T) {
	store, env := catalogStore(t, xcodeCatalog(t))
	fr := map[string]string{
		"ios:" + digested("%lld items"):     EncodeForms(map[string]string{"one": "%lld article", "other": "%lld articles\ndans le panier"}),
		"ios:" + digested("Sign in as @%@"): "Se connecter en tant que @%@",
		"ios:Zebra":                         "Zèbre",
		"ios:files.summary":                 "%#@files@ dans %#@folders@",
		"ios:files.summary%files":           EncodeForms(map[string]string{"one": "%arg fichier", "other": "%arg fichiers"}),
		"ios:files.summary%folders":         EncodeForms(map[string]string{"one": "%arg dossier", "other": "%arg dossiers"}),
		"ios:greeting":                      "Bonjour, %@ !",
		"ios:tap.hint#iphone":               "Touchez pour ouvrir",
		"ios:tap.hint#other":                EncodeForms(map[string]string{"one": "Ouvrir %lld fichier", "other": "Ouvrir %lld fichiers"}),
	}
	written, err := store.Write("fr", fr, files.Write)
	require.NoError(t, err)
	path := filepath.Join(env.Root, "Localizable.xcstrings")
	assert.Equal(t, []string{path}, written)
	catalog := read(t, env, "Localizable.xcstrings")
	assert.Contains(t, catalog, `    "Zebra" : {
      "localizations" : {
        "fr" : {
          "stringUnit" : {
            "state" : "translated",
            "value" : "Zèbre"
          }
        }
      }
    },`)
	assert.Contains(t, catalog, `        "fr" : {
          "stringUnit" : {
            "state" : "translated",
            "value" : "%#@files@ dans %#@folders@"
          },
          "substitutions" : {
            "files" : {
              "argNum" : 1,
              "formatSpecifier" : "lld",
              "variations" : {
                "plural" : {
                  "one" : {
                    "stringUnit" : {
                      "state" : "translated",
                      "value" : "%arg fichier"
                    }
                  },`)
	assert.NotContains(t, catalog, "Touchez", "an entry is written only whole")
	assert.False(t, strings.HasSuffix(catalog, "\n"), "as Xcode writes it, with no newline at the end")

	existing, err := store.Existing("fr", nil)
	require.NoError(t, err)
	delete(fr, "ios:tap.hint#iphone")
	delete(fr, "ios:tap.hint#other")
	assert.Equal(t, fr, existing)
	de, err := store.Existing("de", nil)
	require.NoError(t, err)
	assert.Len(t, de, 2, "another locale's part is untouched")

	assert.Equal(t, []string{path}, dropped(t, store, "fr"))
	assert.Equal(t, xcodeCatalog(t), read(t, env, "Localizable.xcstrings"), "dropping the locale leaves the catalog as Xcode saved it")
	assert.Empty(t, dropped(t, store, "fr"))
}

// Locales written at once each land their own part.
func TestXCStrings_SharedWritesDoNotLoseEachOther(t *testing.T) {
	store, _ := catalogStore(t, xcodeCatalog(t))
	locales := []string{"fr", "es", "it", "ja", "ko", "pt-BR"}
	var group sync.WaitGroup
	for _, locale := range locales {
		group.Go(func() {
			_, err := store.Write(locale, map[string]string{"ios:Zebra": "Zebra " + locale}, files.Write)
			assert.NoError(t, err)
		})
	}
	group.Wait()
	for _, locale := range locales {
		existing, err := store.Existing(locale, nil)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"ios:Zebra": "Zebra " + locale}, existing, locale)
	}
}

// A catalog the store does not know how to read is refused when it opens.
func TestXCStrings_Refusals(t *testing.T) {
	for name, tc := range map[string]struct{ catalog, want string }{
		"another source language": {`{"sourceLanguage":"de","strings":{},"version":"1.0"}`, `is written in "de", and the manifest's master is en`},
		"an unknown version":      {`{"sourceLanguage":"en","strings":{},"version":"2.0"}`, `String Catalog version "2.0"; this reader knows 1.0`},
		"no strings":              {`{"sourceLanguage":"en","version":"1.0"}`, "has no strings"},
		"not JSON":                {`sourceLanguage = en`, "parse"},
		"two documents":           {`{} {}`, "more than one JSON value"},
		"not an object":           {`[]`, "is not a String Catalog"},
		"an entry not an object":  {`{"sourceLanguage":"en","strings":{"a":1},"version":"1.0"}`, `"a" is not an object`},
		"an unknown shape":        {`{"sourceLanguage":"en","strings":{"a":{"localizations":{"en":{"variations":{"width":{}}}}}},"version":"1.0"}`, "localizations.en holds neither a string nor a plural variation"},
		"a device of no shape":    {`{"sourceLanguage":"en","strings":{"a":{"localizations":{"en":{"variations":{"device":{"mac":{}}}}}}},"version":"1.0"}`, "localizations.en.variations.device.mac holds neither"},
		"a device not an object":  {`{"sourceLanguage":"en","strings":{"a":{"localizations":{"en":{"variations":{"device":{"mac":1}}}}}},"version":"1.0"}`, `device variation "mac" is not an object`},
		"a substitution unvaried": {`{"sourceLanguage":"en","strings":{"a":{"localizations":{"en":{"stringUnit":{"state":"translated","value":"%#@n@"},"substitutions":{"n":{"argNum":1}}}}}},"version":"1.0"}`, `substitution "n" has no plural variation`},
		"a plural without other":  {`{"sourceLanguage":"en","strings":{"a":{"localizations":{"en":{"variations":{"plural":{"one":{"stringUnit":{"state":"translated","value":"x"}}}}}}}},"version":"1.0"}`, "has no `other` form"},
	} {
		t.Run(name, func(t *testing.T) {
			env := fixture(t, map[string]string{"Localizable.xcstrings": tc.catalog})
			env.Default = "en"
			_, err := Open("ios", catalogSpec, env)
			assert.ErrorContains(t, err, tc.want)
		})
	}
	env := fixture(t, nil)
	_, err := Open("ios", catalogSpec, env)
	assert.ErrorContains(t, err, "read")
}
