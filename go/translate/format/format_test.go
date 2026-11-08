package format

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"greendrake.nz/go/translate/internal/files"
)

// header is what every store is opened with, and yamlHeader how a YAML
// writer frames it.
const (
	header     = "GENERATED\nby the tests"
	yamlHeader = "# GENERATED\n# by the tests\n"
)

// shipped are the locales the declarations are validated for.
var shipped = []string{"en-GB", "de", "ru", "zh-Hans", "es-419"}

// fixture writes a miniature tree of files and returns the env over it.
func fixture(t *testing.T, files map[string]string) Env {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	return Env{Root: dir, Default: "en-GB", Header: header}
}

func read(t *testing.T, env Env, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(env.Root, rel))
	require.NoError(t, err)
	return string(body)
}

func open(t *testing.T, pass string, spec Spec, env Env) Store {
	t.Helper()
	require.NoError(t, Validate(pass, spec, shipped))
	store, err := Open(pass, spec, env)
	require.NoError(t, err)
	return store
}

// dropped drops a locale from a store and says what it changed.
func dropped(t *testing.T, store Store, locale string) []string {
	t.Helper()
	changed, err := store.Drop(locale)
	require.NoError(t, err)
	return changed
}

func ids(units []Unit) []string {
	out := make([]string, len(units))
	for i, unit := range units {
		out[i] = unit.ID
	}
	return out
}

// --- yaml-catalogue ---

var catalogueSpec = Spec{
	Format:          "yaml-catalogue",
	Path:            "catalogue/{locale}.yaml",
	Syntax:          SyntaxVueI18n,
	SyntaxOverrides: []SyntaxOverride{{Prefix: "seo.", Syntax: SyntaxICU}},
	Markup:          MarkupHTML,
}

const masterCatalogue = `# The whole catalogue.
common:
    # The primary action.
    submit: Submit
    cancel: Cancel
seo:
    title: '{count, plural, one {# listing} other {# listings}}'
`

// A key's context is every note above it, its section's included, and a key
// under an override's prefix is read in the override's syntax.
func TestCatalogue_Units(t *testing.T) {
	store := open(t, "ui", catalogueSpec, fixture(t, map[string]string{"catalogue/en-GB.yaml": masterCatalogue}))
	units := store.Units()
	assert.Equal(t, []string{"ui:common.submit", "ui:common.cancel", "ui:seo.title"}, ids(units))
	assert.Equal(t, "common.submit", units[0].Key)
	assert.Equal(t, "The whole catalogue.\nThe primary action.", units[0].Context)
	assert.Equal(t, Checks{Syntax: SyntaxVueI18n, Markup: MarkupHTML, Shape: ShapeText}, units[0].Checks)
	assert.Equal(t, SyntaxICU, units[2].Checks.Syntax)
}

// A locale's catalogue is the master's structure with its strings: the same
// key order, a section written once something in it is translated, nothing
// written at all before that.
func TestCatalogue_WritesInTheMastersShape(t *testing.T) {
	env := fixture(t, map[string]string{"catalogue/en-GB.yaml": masterCatalogue})
	store := open(t, "ui", catalogueSpec, env)

	written, err := store.Write("de", map[string]string{}, files.Write)
	require.NoError(t, err)
	assert.Empty(t, written)
	assert.NoFileExists(t, filepath.Join(env.Root, "catalogue/de.yaml"))

	translations := map[string]string{"ui:common.cancel": "Abbrechen", "ui:common.submit": "Senden"}
	compared, err := store.Write("de", translations, files.Compare)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(env.Root, "catalogue/de.yaml")}, compared, "a comparison reports the file a write would change")
	assert.NoFileExists(t, filepath.Join(env.Root, "catalogue/de.yaml"), "and changes nothing")

	written, err = store.Write("de", translations, files.Write)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(env.Root, "catalogue/de.yaml")}, written)
	assert.Equal(t, yamlHeader+"common:\n    submit: Senden\n    cancel: Abbrechen\n", read(t, env, "catalogue/de.yaml"))
	compared, err = store.Write("de", translations, files.Compare)
	require.NoError(t, err)
	assert.Empty(t, compared, "nor one that already holds its bytes")

	existing, err := store.Existing("de", nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"ui:common.submit": "Senden", "ui:common.cancel": "Abbrechen"}, existing)
	assert.Equal(t, []string{filepath.Join(env.Root, "catalogue/de.yaml")}, dropped(t, store, "de"))
	assert.NoFileExists(t, filepath.Join(env.Root, "catalogue/de.yaml"))
	assert.Empty(t, dropped(t, store, "de"), "a locale with nothing left has nothing to drop")
}

// --- yaml-tree ---

var treeSpec = Spec{
	Format:       "yaml-tree",
	Path:         "trees/*.yaml",
	Sidecar:      "trees/names/{locale}.yaml",
	Keys:         TreeKeys{Key: "key", Label: "label", Nodes: "nodes", Code: "code", Names: "names", Children: "children", SkipNodeNames: "proper_nouns"},
	NodeID:       "{key}:{code}",
	Syntax:       SyntaxNone,
	Markup:       MarkupNone,
	Shape:        ShapeLine,
	LabelContext: `The "{key}" attribute.`,
	Context:      `A "{key}" ({label}).`,
}

var treeFiles = map[string]string{
	"trees/category.yaml": `key: category
label: {en-GB: Category, fr: Catégorie}
nodes:
  - code: vehicles
    names: {en-GB: Vehicles}
    children:
      - {code: cars, names: {en-GB: Cars, de: Wagen}}
  - {code: "2024", names: {en-GB: 2024}}
`,
	"trees/brand.yaml": `key: brand
label: {en-GB: Brand}
proper_nouns: true
nodes:
  - {code: acme, names: {en-GB: ACME}}
`,
}

// A tree contributes its label and its nodes in authored order, ids composed
// as declared, each node told what it is nested inside; a tree whose nodes are
// names contributes its label alone. A name is read as written, even one that
// looks like a number.
func TestTree_Units(t *testing.T) {
	store := open(t, "tax", treeSpec, fixture(t, treeFiles))
	units := store.Units()
	assert.Equal(t, []string{"tax:brand", "tax:category", "tax:category:vehicles", "tax:category:cars", "tax:category:2024"}, ids(units))
	assert.Equal(t, `The "brand" attribute.`, units[0].Context)
	assert.Equal(t, `A "category" (Category).`, units[2].Context)
	assert.Equal(t, `A "category" (Category). Inside: Vehicles.`, units[3].Context)
	assert.Equal(t, "2024", units[4].Text)
	assert.Equal(t, "category", units[3].File)
	assert.Equal(t, ShapeLine, units[3].Checks.Shape)
}

// A locale's names come from the master inline and from its sidecar, the
// sidecar's winning, and an entry for what the master no longer defines names
// nothing. The sidecar is written from the translations, keyed by code.
func TestTree_ExistingAndWrite(t *testing.T) {
	tree := map[string]string{"trees/names/de.yaml": "category:\n    label: Kategorie\n    nodes:\n        cars: Autos\n        gone: Weg\nretired:\n    label: Alt\n"}
	for rel, body := range treeFiles {
		tree[rel] = body
	}
	env := fixture(t, tree)
	store := open(t, "tax", treeSpec, env)

	existing, err := store.Existing("de", nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"tax:category": "Kategorie", "tax:category:cars": "Autos"}, existing)
	fr, err := store.Existing("fr", nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"tax:category": "Catégorie"}, fr)

	_, err = store.Write("de", map[string]string{"tax:brand": "Marke", "tax:brand:acme": "ignored", "tax:category:vehicles": "Fahrzeuge"}, files.Write)
	require.NoError(t, err)
	var sidecar map[string]sidecarEntry
	require.NoError(t, yaml.Unmarshal([]byte(read(t, env, "trees/names/de.yaml")), &sidecar))
	assert.Equal(t, map[string]sidecarEntry{
		"brand":    {Label: "Marke"},
		"category": {Nodes: map[string]string{"vehicles": "Fahrzeuge"}},
	}, sidecar)
	assert.Equal(t, []string{filepath.Join(env.Root, "trees/names/de.yaml")}, dropped(t, store, "de"))
}

// --- text-blocks ---

var blocksSpec = Spec{
	Format:     "text-blocks",
	Path:       "pages/*/{locale}.txt",
	PageID:     "/{name}",
	Syntax:     SyntaxBraces,
	Markup:     MarkupMarkdown,
	CopyBlocks: []string{"::TOC::"},
}

// A page is its blocks, each addressed by its own digest under the page's id
// — a block repeated verbatim by occurrence — and a marker block is copied
// rather than translated.
func TestBlocks_Units(t *testing.T) {
	store := open(t, "content", blocksSpec, fixture(t, map[string]string{
		"pages/about/en-GB.txt": "# About\n\nSame.\n\nSame.\n\n::TOC::\n",
		"pages/about/de.txt":    "ignored: not the master\n",
	}))
	units := store.Units()
	require.Len(t, units, 4)
	assert.Regexp(t, `^content:/about#[0-9a-f]{16}$`, units[0].ID)
	assert.Equal(t, units[1].ID+"~2", units[2].ID)
	assert.Equal(t, "One block of the /about page.", units[0].Context)
	assert.Equal(t, "/about", units[0].Sequence)
	assert.True(t, units[3].Copy)
	assert.False(t, units[1].Copy)
}

// A locale's page is written once every block of it is translated, and read
// back by the order recorded when it was; a file whose count no longer
// matches that order cannot be aligned by anything.
func TestBlocks_ExistingAndWrite(t *testing.T) {
	env := fixture(t, map[string]string{"pages/about/en-GB.txt": "# About\n\nBody.\n"})
	store := open(t, "content", blocksSpec, env)
	units := store.Units()

	written, err := store.Write("de", map[string]string{units[0].ID: "# Über"}, files.Write)
	require.NoError(t, err)
	assert.Empty(t, written, "half a page is not written")

	_, err = store.Write("de", map[string]string{units[0].ID: "# Über", units[1].ID: "Text."}, files.Write)
	require.NoError(t, err)
	assert.Equal(t, "# Über\n\nText.\n", read(t, env, "pages/about/de.txt"))

	order := map[string][]string{"/about": {units[0].ID, units[1].ID}}
	existing, err := store.Existing("de", order)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{units[0].ID: "# Über", units[1].ID: "Text."}, existing)

	unrecorded, err := store.Existing("de", nil)
	require.NoError(t, err)
	assert.Empty(t, unrecorded, "without the order that addresses it, the file says nothing")

	require.NoError(t, os.WriteFile(filepath.Join(env.Root, "pages/about/de.txt"), []byte("# Über\n\nText.\n\nMore.\n"), 0o644))
	_, err = store.Existing("de", order)
	assert.ErrorContains(t, err, "has 3 blocks but the translator recorded 2")
	assert.Equal(t, []string{filepath.Join(env.Root, "pages/about/de.txt")}, dropped(t, store, "de"))
}

func TestSplitBlocks(t *testing.T) {
	blocks := SplitBlocks("# Title\n\nOne paragraph.\n\n\n - a\n - b\n\n::TOC::\n")
	assert.Equal(t, []string{"# Title", "One paragraph.", " - a\n - b", "::TOC::"}, blocks)
}

// --- yaml-rows ---

var rowsSpec = Spec{
	Format: "yaml-rows",
	Master: "templates.yaml",
	Path:   "templates/{locale}.yaml",
	Row:    RowKeys{ID: "template_id", Locale: "locale"},
	Fields: Fields{
		{Name: "subject", Describe: "The subject line", Shape: ShapeLine, Limit: 78},
		{Name: "body", Describe: "The body", Markup: MarkupHTML},
	},
	Syntax:  SyntaxICU,
	Markup:  MarkupNone,
	Context: `{describe} of "{id}".`,
}

const masterRows = `- template_id: welcome
  locale: en-GB
  subject: Welcome
  body: <p>Hello</p>
- template_id: welcome
  locale: fr
  subject: Bienvenue
  body: <p>Bonjour</p>
- template_id: bye
  locale: en-GB
  subject: Goodbye
  body: <p>Bye</p>
`

// A row's fields are units that share a bundle, each held to its own field's
// checks over the source's, its limit among them.
func TestRows_Units(t *testing.T) {
	store := open(t, "email", rowsSpec, fixture(t, map[string]string{"templates.yaml": masterRows}))
	units := store.Units()
	assert.Equal(t, []string{"email:welcome.subject", "email:welcome.body", "email:bye.subject", "email:bye.body"}, ids(units))
	assert.Equal(t, `The subject line of "welcome".`, units[0].Context)
	assert.Equal(t, Checks{Syntax: SyntaxICU, Markup: MarkupNone, Shape: ShapeLine, Limit: 78}, units[0].Checks)
	assert.Equal(t, Checks{Syntax: SyntaxICU, Markup: MarkupHTML, Shape: ShapeText}, units[1].Checks)
	assert.Equal(t, "welcome", units[1].Bundle)
}

// A locale's rows come from the master inline and from its own file; a row
// is written only whole, in master order, fields in declared order.
func TestRows_ExistingAndWrite(t *testing.T) {
	env := fixture(t, map[string]string{"templates.yaml": masterRows})
	store := open(t, "email", rowsSpec, env)

	fr, err := store.Existing("fr", nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"email:welcome.subject": "Bienvenue", "email:welcome.body": "<p>Bonjour</p>"}, fr)

	_, err = store.Write("de", map[string]string{
		"email:bye.subject": "Tschüss", "email:bye.body": "<p>Tschüss</p>\n<p>Bis bald</p>",
		"email:welcome.subject": "Willkommen",
	}, files.Write)
	require.NoError(t, err)
	assert.Equal(t, yamlHeader+"- template_id: bye\n  locale: de\n  subject: Tschüss\n  body: \"<p>Tschüss</p>\\n<p>Bis bald</p>\"\n", read(t, env, "templates/de.yaml"))

	de, err := store.Existing("de", nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"email:bye.subject": "Tschüss", "email:bye.body": "<p>Tschüss</p>\n<p>Bis bald</p>"}, de)

	require.NoError(t, os.WriteFile(filepath.Join(env.Root, "templates/de.yaml"), []byte("- {template_id: bye, locale: fr, subject: x, body: y}\n"), 0o644))
	_, err = store.Existing("de", nil)
	assert.ErrorContains(t, err, `declares locale "fr", which is not the file's`)
	assert.Equal(t, []string{filepath.Join(env.Root, "templates/de.yaml")}, dropped(t, store, "de"))
}

// --- declarations ---

// What a format cannot honour is refused before anything is read.
func TestValidate_Refusals(t *testing.T) {
	with := func(spec Spec, edit func(*Spec)) Spec {
		edit(&spec)
		return spec
	}
	cases := map[string]struct {
		spec Spec
		want string
	}{
		"unknown format":               {with(catalogueSpec, func(s *Spec) { s.Format = "xliff" }), `unknown format "xliff"`},
		"key the format does not take": {with(catalogueSpec, func(s *Spec) { s.PageID = "/{name}" }), "yaml-catalogue takes no `page_id`"},
		"unknown syntax":               {with(catalogueSpec, func(s *Spec) { s.Syntax = "fluent" }), `syntax "fluent" is none of icu, vue-i18n, braces, printf, none`},
		"unknown override syntax":      {with(catalogueSpec, func(s *Spec) { s.SyntaxOverrides = []SyntaxOverride{{Prefix: "a.", Syntax: "fluent"}} }), "a syntax override needs a prefix and one of icu"},
		"markup unstated":              {with(catalogueSpec, func(s *Spec) { s.Markup = "" }), `markup "" is none of`},
		"unknown shape":                {with(catalogueSpec, func(s *Spec) { s.Shape = "label" }), `shape "label" is none of`},
		"per-locale path without tag":  {with(catalogueSpec, func(s *Spec) { s.Path = "catalogue/en-GB.yaml" }), "`path` must carry {locale}"},
		"tree glob with a tag":         {with(treeSpec, func(s *Spec) { s.Path = "trees/{locale}.yaml" }), "without {locale}"},
		"sidecar without a tag":        {with(treeSpec, func(s *Spec) { s.Sidecar = "trees/names.yaml" }), "`sidecar` must carry {locale}"},
		"node id without a code":       {with(treeSpec, func(s *Spec) { s.NodeID = "{key}" }), "must carry {code}"},
		"node id naming the unknown":   {with(treeSpec, func(s *Spec) { s.NodeID = "{key}:{code}:{name}" }), "names {name}"},
		"tree key unnamed":             {with(treeSpec, func(s *Spec) { s.Keys.Code = "" }), "`keys.code` is required"},
		"page glob without a star":     {with(blocksSpec, func(s *Spec) { s.Path = "pages/about/{locale}.txt" }), "exactly one `*`"},
		"page id naming the unknown":   {with(blocksSpec, func(s *Spec) { s.PageID = "/{page}" }), "names {page}"},
		"rows master with a tag":       {with(rowsSpec, func(s *Spec) { s.Master = "templates/{locale}.yaml" }), "without {locale}"},
		"rows without fields":          {with(rowsSpec, func(s *Spec) { s.Fields = nil }), "must name at least one"},
		"rows context naming unknown":  {with(rowsSpec, func(s *Spec) { s.Context = "{label}" }), "names {label}"},
		"field markup unknown":         {with(rowsSpec, func(s *Spec) { s.Fields = Fields{{Name: "body", Markup: "rtf"}} }), `field body: markup "rtf"`},
		"field limit negative":         {with(rowsSpec, func(s *Spec) { s.Fields = Fields{{Name: "title", Limit: -1}} }), "field title: limit -1 is not a number of characters"},
		"android master with a tag":    {with(androidSpec, func(s *Spec) { s.Master = "res/values-{locale}/strings.xml" }), "`master` is the authored strings.xml"},
		"android path without a tag":   {with(androidSpec, func(s *Spec) { s.Path = "res/values-de/strings.xml" }), "`path` must carry {locale}"},
		"android not printf":           {with(androidSpec, func(s *Spec) { s.Syntax = SyntaxICU }), "`syntax: printf`, `markup: html`"},
		"android without markup":       {with(androidSpec, func(s *Spec) { s.Markup = MarkupNone }), "`syntax: printf`, `markup: html`"},
		"android takes no sidecar":     {with(androidSpec, func(s *Spec) { s.Sidecar = "x/{locale}.yaml" }), "android-strings takes no `sidecar`"},
		"catalog path with a tag":      {with(catalogSpec, func(s *Spec) { s.Path = "{locale}.xcstrings" }), "the one catalog every locale shares"},
		"catalog not printf":           {with(catalogSpec, func(s *Spec) { s.Syntax = SyntaxBraces }), "`syntax: printf`"},
		"catalog takes no master":      {with(catalogSpec, func(s *Spec) { s.Master = "x.xcstrings" }), "xcstrings takes no `master`"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.ErrorContains(t, Validate("p", tc.spec, shipped), tc.want)
		})
	}
	// A locale Android names no resource directory for is refused before
	// anything is read.
	assert.ErrorContains(t, Validate("p", androidSpec, []string{"en-GB", "en-u-ca-buddhist"}), "Android names no resource directory for en-u-ca-buddhist")
}

// Fields keep the order they are declared in, which is the order a row is
// written in.
func TestFields_KeepDeclaredOrder(t *testing.T) {
	var spec Spec
	require.NoError(t, yaml.Unmarshal([]byte("fields:\n  subject: {shape: line, limit: 78}\n  body: {markup: html}\n  footer: {}\n"), &spec))
	names := make([]string, len(spec.Fields))
	for i, field := range spec.Fields {
		names[i] = field.Name
	}
	assert.Equal(t, []string{"subject", "body", "footer"}, names)
	assert.Equal(t, 78, spec.Fields[0].Limit)
}
