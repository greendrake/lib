package format

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"greendrake.nz/go/translate/internal/files"
)

var androidSpec = Spec{
	Format: Android,
	Master: "res/values/strings.xml",
	Path:   "res/values-{locale}/strings.xml",
	Syntax: SyntaxPrintf,
	Markup: MarkupHTML,
}

const androidRoot = `<resources xmlns:xliff="urn:oasis:names:tc:xliff:document:1.2" xmlns:tools="http://schemas.android.com/tools">`

const masterStrings = `<?xml version="1.0" encoding="utf-8"?>
` + androidRoot + `
    <!-- The app's name, never translated. -->
    <string name="app_name" translatable="false">Acme</string>
    <!-- Greets the user. -->
    <!-- Shown on the home screen. -->
    <string name="greeting">Hello, <b><xliff:g id="name" example="Ann">%1$s</xliff:g></b>!</string>
    <string name="escapes">Don\'t say \"no\" \\ here:\n\tdone \@home \u00e9
        and   on</string>
    <string name="spaces">"  kept   inside  "  and   outside  </string>
    <string name="entities">Tom &amp; Jerry &lt;3</string>
    <string name="percent" formatted="false">100% sure</string>
    <string name="alias">@string/greeting</string>
    <plurals name="songs">
        <!-- Not a resource's comment. -->
        <item quantity="one"><xliff:g id="count">%d</xliff:g> song</item>
        <item quantity="other"><xliff:g id="count">%d</xliff:g> songs\non the list</item>
    </plurals>
    <string-array name="planets">
        <item>Mercury</item>
        <item>@string/app_name</item>
    </string-array>
    <dimen name="margin">4dp</dimen>
</resources>
`

func androidStore(t *testing.T) (Store, Env) {
	t.Helper()
	env := fixture(t, map[string]string{"res/values/strings.xml": masterStrings})
	return open(t, "app", androidSpec, env), env
}

// A string is a unit keyed by its name, a plural a keyed plural, an array's
// items units of one bundle. What Android resolves a value to is the text —
// escapes, quoting and whitespace applied, markup and the protected spans as
// written — and the comments above a resource are its context.
func TestAndroid_Units(t *testing.T) {
	store, _ := androidStore(t)
	units := store.Units()
	require.Equal(t, []string{"app:greeting", "app:escapes", "app:spaces", "app:entities", "app:percent", "app:alias", "app:songs", "app:planets[0]", "app:planets[1]"}, ids(units))
	byKey := map[string]Unit{}
	for _, unit := range units {
		byKey[unit.Key] = unit
	}

	greeting := byKey["greeting"]
	assert.Equal(t, `Hello, <b><xliff:g id="name" example="Ann">%1$s</xliff:g></b>!`, greeting.Text)
	assert.Equal(t, "Greets the user.\nShown on the home screen.", greeting.Context)
	assert.Equal(t, Checks{Syntax: SyntaxPrintf, Markup: MarkupHTML, Shape: ShapeText}, greeting.Checks)
	assert.Equal(t, "res/values/strings.xml", greeting.File)

	assert.Equal(t, "Don't say \"no\" \\ here:\n\tdone @home é and on", byKey["escapes"].Text)
	assert.Equal(t, "  kept   inside   and outside", byKey["spaces"].Text, "quoted whitespace is kept, the rest collapsed and trimmed")
	assert.Equal(t, "Tom &amp; Jerry &lt;3", byKey["entities"].Text, "the text's own & and < stay apart from markup")
	assert.Equal(t, SyntaxNone, byKey["percent"].Checks.Syntax, "formatted=\"false\" is literal text")
	assert.True(t, byKey["alias"].Copy, "a reference is copied")
	assert.Equal(t, "@string/greeting", byKey["alias"].Text)

	songs := byKey["songs"]
	assert.Equal(t, map[string]string{
		"one":   `<xliff:g id="count">%d</xliff:g> song`,
		"other": "<xliff:g id=\"count\">%d</xliff:g> songs\non the list",
	}, songs.Forms)
	assert.Equal(t, `{"one":"<xliff:g id=\"count\">%d</xliff:g> song","other":"<xliff:g id=\"count\">%d</xliff:g> songs\non the list"}`, songs.Text)

	assert.Equal(t, "Mercury", byKey["planets[0]"].Text)
	assert.Equal(t, "planets", byKey["planets[0]"].Bundle)
	assert.True(t, byKey["planets[1]"].Copy)
	assert.Empty(t, byKey["greeting"].Bundle)
}

// A locale's file is the master's translatable resources in its order, each
// translated one — an array only whole — every plural in the locale's own
// quantities, under the XML declaration and the header. It reads back as
// written, and a second write changes nothing.
func TestAndroid_WriteAndReadBack(t *testing.T) {
	store, env := androidStore(t)
	songs := EncodeForms(map[string]string{
		"one":   `<xliff:g id="count">%d</xliff:g> песня`,
		"few":   `<xliff:g id="count">%d</xliff:g> песни`,
		"many":  `<xliff:g id="count">%d</xliff:g> песен`,
		"other": "<xliff:g id=\"count\">%d</xliff:g> песни\nв списке",
	})
	translations := map[string]string{
		"app:greeting":   `Привет, <b><xliff:g id="name" example="Ann">%1$s</xliff:g></b>!`,
		"app:escapes":    "It's \"нет\" \\:\n\t@ok ?",
		"app:spaces":     "  ровно  ",
		"app:entities":   "Том &amp; Джерри &lt;3",
		"app:percent":    "100% уверен",
		"app:alias":      "@string/greeting",
		"app:songs":      songs,
		"app:planets[0]": "Меркурий",
	}
	written, err := store.Write("ru", translations, files.Write)
	require.NoError(t, err)
	path := filepath.Join(env.Root, "res/values-ru/strings.xml")
	assert.Equal(t, []string{path}, written)
	assert.Equal(t, `<?xml version="1.0" encoding="utf-8"?>
<!--
    GENERATED
    by the tests
-->
`+androidRoot+`
    <string name="greeting">Привет, <b><xliff:g id="name" example="Ann">%1$s</xliff:g></b>!</string>
    <string name="escapes">It\'s \"нет\" \\:\n\t@ok ?</string>
    <string name="spaces">"  ровно  "</string>
    <string name="entities">Том &amp; Джерри &lt;3</string>
    <string name="percent" formatted="false">100% уверен</string>
    <string name="alias">@string/greeting</string>
    <plurals name="songs">
        <item quantity="one"><xliff:g id="count">%d</xliff:g> песня</item>
        <item quantity="few"><xliff:g id="count">%d</xliff:g> песни</item>
        <item quantity="many"><xliff:g id="count">%d</xliff:g> песен</item>
        <item quantity="other"><xliff:g id="count">%d</xliff:g> песни\nв списке</item>
    </plurals>
</resources>
`, read(t, env, "res/values-ru/strings.xml"))

	existing, err := store.Existing("ru", nil)
	require.NoError(t, err)
	delete(translations, "app:planets[0]")
	assert.Equal(t, translations, existing, "half an array is not written")

	again, err := store.Write("ru", existing, files.Compare)
	require.NoError(t, err)
	assert.Empty(t, again, "what was read back writes the same bytes")

	// A text with a bare `&`, a leading reference sign and whitespace runs
	// is written so that Android reads back exactly it.
	translations["app:entities"] = "@Tom & Jerry  <b>x</b>"
	_, err = store.Write("ru", translations, files.Write)
	require.NoError(t, err)
	assert.Contains(t, read(t, env, "res/values-ru/strings.xml"), `<string name="entities">"\@Tom &amp; Jerry  <b>x</b>"</string>`)
	existing, err = store.Existing("ru", nil)
	require.NoError(t, err)
	assert.Equal(t, "@Tom &amp; Jerry  <b>x</b>", existing["app:entities"])

	nothing, err := store.Write("de", map[string]string{}, files.Write)
	require.NoError(t, err)
	assert.Empty(t, nothing, "a locale with nothing translated has no file")

	assert.Equal(t, []string{path}, dropped(t, store, "ru"))
	assert.NoFileExists(t, path)
}

// A locale's directory is named for its qualifier: a language alone, a
// language with a region, and anything else in BCP 47 form.
func TestAndroid_Qualifiers(t *testing.T) {
	for locale, want := range map[string]string{
		"de":          "de",
		"en-GB":       "en-rGB",
		"zh-Hans":     "b+zh+Hans",
		"es-419":      "b+es+419",
		"fil":         "b+fil",
		"sr-Latn-RS":  "b+sr+Latn+RS",
		"ca-valencia": "b+ca+valencia",
	} {
		got, err := qualifier(locale)
		require.NoError(t, err, locale)
		assert.Equal(t, want, got, locale)
	}
	_, err := qualifier("en-u-ca-buddhist")
	assert.ErrorContains(t, err, "a tag with an extension has no qualifier")
	_, err = qualifier("not a tag")
	assert.ErrorContains(t, err, "is no BCP 47 tag")

	store, env := androidStore(t)
	_, err = store.Write("zh-Hans", map[string]string{"app:greeting": `你好，<b><xliff:g id="name" example="Ann">%1$s</xliff:g></b>！`}, files.Write)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(env.Root, "res/values-b+zh+Hans/strings.xml"))
}

// What the store cannot read is refused when it opens.
func TestAndroid_Refusals(t *testing.T) {
	cases := map[string]struct {
		master, header, want string
	}{
		"no master":         {"", header, "read the master"},
		"not resources":     {`<values/>`, header, "<values> is not <resources>"},
		"malformed":         {`<resources><string name="x">a</resources>`, header, "res/values/strings.xml"},
		"two of one name":   {`<resources><string name="x">a</string><plurals name="x"><item quantity="other">b</item></plurals></resources>`, header, "two resources named x"},
		"plural with none":  {`<resources><plurals name="x"></plurals></resources>`, header, `<plurals name="x"> has no item`},
		"plural sans other": {`<resources><plurals name="x"><item quantity="one">a</item></plurals></resources>`, header, "has no `other` form"},
		"odd quantity":      {`<resources><plurals name="x"><item quantity="several">a</item><item quantity="other">b</item></plurals></resources>`, header, `plural form "several" is no CLDR category`},
		"bad escape":        {`<resources><string name="x">\u12</string></resources>`, header, `x: \u is not followed by four hex digits`},
		"header with --":    {`<resources/>`, "run go tool translate --check", "an XML comment cannot"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{}
			if tc.master != "" {
				files["res/values/strings.xml"] = tc.master
			}
			env := fixture(t, files)
			env.Header = tc.header
			_, err := Open("app", androidSpec, env)
			assert.ErrorContains(t, err, tc.want)
		})
	}

	// A locale's file that does not parse is said so of.
	store, env := androidStore(t)
	require.NoError(t, os.MkdirAll(filepath.Join(env.Root, "res/values-de"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(env.Root, "res/values-de/strings.xml"), []byte("<resources><string>"), 0o644))
	_, err := store.Existing("de", nil)
	assert.ErrorContains(t, err, "res/values-de/strings.xml")
}
