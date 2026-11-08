package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"greendrake.nz/go/llm"
)

// A whole host in miniature: a manifest declaring one source of every format,
// a derived output and a verify command, over one of everything each
// format reads — so a run exercises every reader, check and writer without
// depending on any real product's size or wording.

const fixtureManifest = `command: bin/demo translate
default: en-GB
shipped: [en-GB, %s]
state: translation/state
policy:
  path: translation/policy.yaml
  brief: translation/brief.yaml
  register: translation/register.yaml
  glossary: translation/glossary
vocabulary:
  id: gl
  batch: {size: 100}
  tier: strong
  guidance: Each id is a term to render into {language}.
llm: {env_prefix: TRANSLATE_LLM_, timeout: 10m, parallel: 2}
prompt:
  keep_verbatim: [App Store, Google Play]
  rules: [Keep user-interface copy about as short as the source.]
sources:
  - id: ui
    format: yaml-catalogue
    path: catalogue/{locale}.yaml
    syntax: vue-i18n
    syntax_overrides:
      - {prefix: seo., syntax: icu}
      - {prefix: content., syntax: icu}
    markup: html
    batch: {by: first-segment, size: 40}
    tier: fast
    guidance: These are user-interface strings.
  - id: tax
    format: yaml-tree
    path: taxonomies/*.yaml
    sidecar: taxonomies/names/{locale}.yaml
    keys: {key: key, label: label, nodes: nodes, code: code, names: names, children: children, skip_node_names: proper_nouns}
    node_id: "{key}:{code}"
    syntax: none
    markup: none
    shape: line
    batch: {by: file, size: 60}
    tier: fast
  - id: content
    format: text-blocks
    path: content/*/{locale}.txt
    page_id: "/{name}"
    syntax: braces
    markup: markdown
    copy_blocks: ["::TOC::"]
    batch: {by: file}
    tier: strong
  - id: email
    format: yaml-rows
    master: email_templates.yaml
    path: email_templates/{locale}.yaml
    row: {id: template_id, locale: locale}
    fields:
      subject: {shape: line, describe: The subject line}
      body: {markup: html, describe: The body}
    syntax: icu
    markup: none
    context: '{describe} of an email (template "{id}").'
    batch: {size: 10}
    tier: fast
  - id: push
    format: yaml-rows
    master: push_templates.yaml
    path: push_templates/{locale}.yaml
    row: {id: template_id, locale: locale}
    fields:
      subject: {describe: The title, limit: 40}
      body: {describe: The body}
    syntax: icu
    markup: none
    shape: line
    batch: {size: 10}
    tier: fast
  - id: app
    format: android-strings
    master: android/values/strings.xml
    path: android/values-{locale}/strings.xml
    syntax: printf
    markup: html
    tier: fast
  - id: ios
    format: xcstrings
    path: ios/Localizable.xcstrings
    syntax: printf
    markup: none
    tier: fast
derived:
  - kind: locale_tables
    path: locale_tables.json
    comment: |
      Plural categories and matcher verdicts.
      Read by the browser.
verify:
  - {run: [sh, -c, "touch verified"], cwd: .}
`

const fixtureCatalogue = `# UI strings.
common:
    # The label on the primary action.
    submit: Submit
    cancel: Cancel
    greeting: 'Hello, {name}'
    # A plural, vue-i18n style.
    listing_count: '{n} listing | {n} listings'
    linked: 'Welcome to @:brand.name'
seo:
    available: Available
    hub_description: '{title}: {count, plural, one {# listing} other {# listings}}.'
content:
    about:
        title: About
        description: What this is.
`

const fixtureTaxonomy = `key: category
label: {en-GB: Category}
nodes:
  - code: vehicles
    names: {en-GB: Vehicles}
    children:
      - {code: cars, names: {en-GB: Cars}}
`

const fixtureBrandTaxonomy = `key: brand
label: {en-GB: Brand}
proper_nouns: true
nodes:
  - {code: acme, names: {en-GB: ACME}}
`

const fixtureAbout = `# About

This is a **notice board** on {site_name}.

 - Free to use
 - [See an example](/U7DGN95PR51)

::TOC::
`

const fixtureEmails = `- template_id: activate_account
  locale: en-GB
  subject: Activate your {site_name} account
  body: |-
    <p>Welcome!</p>
    <p>Your code is <strong>{code}</strong>.</p>
`

const fixturePushes = `- template_id: push_offer
  locale: en-GB
  subject: 'Offer on {listing_title}'
  body: 'Offer: {offer_amount}'
`

const fixtureStrings = `<?xml version="1.0" encoding="utf-8"?>
<resources xmlns:xliff="urn:oasis:names:tc:xliff:document:1.2">
    <!-- Greets the user. -->
    <string name="greeting">Hello, <xliff:g id="name" example="Ann">%1$s</xliff:g>!</string>
    <plurals name="photos">
        <item quantity="one">%d photo</item>
        <item quantity="other">%d photos</item>
    </plurals>
</resources>
`

// fixtureCatalog is laid out as Xcode saves it.
const fixtureCatalog = `{
  "sourceLanguage" : "en-GB",
  "strings" : {
    "%lld photos" : {
      "localizations" : {
        "en-GB" : {
          "variations" : {
            "plural" : {
              "one" : {
                "stringUnit" : {
                  "state" : "translated",
                  "value" : "%lld photo"
                }
              },
              "other" : {
                "stringUnit" : {
                  "state" : "translated",
                  "value" : "%lld photos"
                }
              }
            }
          }
        }
      }
    },
    "greeting" : {
      "comment" : "Greets the user.",
      "localizations" : {
        "en-GB" : {
          "stringUnit" : {
            "state" : "translated",
            "value" : "Hello, %@!"
          }
        }
      }
    }
  },
  "version" : "1.0"
}`

const fixtureRegister = `de: Address the reader informally (du).
fr: Address the reader as vous.
ru: Address the reader as вы.
`

const fixtureBrief = `brief: |
  A classifieds and notice board that spans the world.
`

const fixturePolicy = `terms:
  listing: An advertisement a user posts.
  lister: The person who posted a listing.
`

// fixture writes the miniature tree, shipping the master and the given
// locales, and returns its directory.
func fixture(t *testing.T, shipped ...string) string {
	t.Helper()
	return tree(t, map[string]string{
		"catalogue/en-GB.yaml":       fixtureCatalogue,
		"taxonomies/category.yaml":   fixtureTaxonomy,
		"taxonomies/brand.yaml":      fixtureBrandTaxonomy,
		"content/about/en-GB.txt":    fixtureAbout,
		"email_templates.yaml":       fixtureEmails,
		"push_templates.yaml":        fixturePushes,
		"android/values/strings.xml": fixtureStrings,
		"ios/Localizable.xcstrings":  fixtureCatalog,
		"translation/policy.yaml":    fixturePolicy,
		"translation/brief.yaml":     fixtureBrief,
		"translation/register.yaml":  fixtureRegister,
		"translate.yaml":             fmt.Sprintf(fixtureManifest, strings.Join(shipped, ", ")),
	})
}

// tree writes files, by their paths relative to a new directory, and returns
// the directory.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	return dir
}

// ship rewrites the fixture's manifest to ship other locales.
func ship(t *testing.T, dir string, shipped ...string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "translate.yaml"), []byte(fmt.Sprintf(fixtureManifest, strings.Join(shipped, ", "))), 0o644))
}

func load(t *testing.T, dir string) *Manifest {
	t.Helper()
	return loadFor(t, dir, nil)
}

// loadFor reads the manifest of dir on behalf of project.
func loadFor(t *testing.T, dir string, project *Manifest) *Manifest {
	t.Helper()
	m, err := Load(filepath.Join(dir, "translate.yaml"), project)
	require.NoError(t, err)
	return m
}

func run(t *testing.T, dir string, opts Options) (Result, error) {
	t.Helper()
	return Run(context.Background(), load(t, dir), opts)
}

func mustRun(t *testing.T, dir string, opts Options) Result {
	t.Helper()
	return mustRunManifest(t, load(t, dir), opts)
}

// mustRunFor runs the library manifest of lib on behalf of the project fixture
// at project.
func mustRunFor(t *testing.T, lib, project string, opts Options) Result {
	t.Helper()
	return mustRunManifest(t, loadFor(t, lib, load(t, project)), opts)
}

func mustRunManifest(t *testing.T, m *Manifest, opts Options) Result {
	t.Helper()
	result, err := Run(context.Background(), m, opts)
	require.NoError(t, err)
	return result
}

// scriptedClient answers every batch by echoing each item's source, which is
// the one answer that satisfies every check by construction — the point of
// these tests is the pipeline around the model, not the model. A plural is
// asked for one form at a time, so echoing restructures nothing and a locale
// that counts differently from English still comes out with its own forms.
func scriptedClient(t *testing.T) (llm.Client, *int64) {
	t.Helper()
	var calls int64
	fake := &llm.Fake{Handler: func(req llm.Request) (json.RawMessage, error) {
		atomic.AddInt64(&calls, 1)
		return echo(t, req, nil)
	}}
	return llm.WithValidation(fake, nil), &calls
}

// echo answers one request with each item's own source, passed through edit
// where one is given.
func echo(t *testing.T, req llm.Request, edit func(id, text string) string) (json.RawMessage, error) {
	t.Helper()
	var payload struct {
		Items []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(req.User), &payload), "the user prompt is the items as JSON")
	out := struct {
		Items []map[string]string `json:"items"`
	}{}
	for _, item := range payload.Items {
		text := item.Text
		if edit != nil {
			text = edit(item.ID, text)
		}
		out.Items = append(out.Items, map[string]string{"id": item.ID, "text": text})
	}
	// Written as a model is asked to write it: every character as itself,
	// markup included.
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(out)
	return body.Bytes(), err
}

func client(t *testing.T, handler func(req llm.Request) (json.RawMessage, error)) llm.Client {
	t.Helper()
	return llm.WithValidation(&llm.Fake{Handler: handler}, nil)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(body)
}

func replaceInFile(t *testing.T, path, old, new string) {
	t.Helper()
	body := readFile(t, path)
	require.Contains(t, body, old, "fixture edit target missing in %s", path)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(body, old, new, 1)), 0o644))
}

func lineContaining(t *testing.T, body, needle string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no line containing %q", needle)
	return ""
}

// countOf counts a value's occurrences in a list.
func countOf(list []string, value string) int {
	n := 0
	for _, v := range list {
		if v == value {
			n++
		}
	}
	return n
}

// units counts the fixture's master units whose id starts with prefix.
func units(t *testing.T, dir, prefix string) int {
	t.Helper()
	return unitsOf(t, load(t, dir), prefix)
}

// unitsOf counts a manifest's master units whose id starts with prefix.
func unitsOf(t *testing.T, m *Manifest, prefix string) int {
	t.Helper()
	master, err := m.Master()
	require.NoError(t, err)
	n := 0
	for _, unit := range master.Units {
		if strings.HasPrefix(unit.ID, prefix) {
			n++
		}
	}
	return n
}

func TestRun_ProducesEveryFileThenDoesNothing(t *testing.T) {
	dir := fixture(t, "de")
	c, calls := scriptedClient(t)

	first := mustRun(t, dir, Options{Client: c})
	assert.Equal(t, units(t, dir, ""), first.Stale["de"])
	assert.Positive(t, atomic.LoadInt64(calls))
	for _, rel := range []string{
		"catalogue/de.yaml",
		"taxonomies/names/de.yaml",
		"email_templates/de.yaml",
		"push_templates/de.yaml",
		"content/about/de.txt",
		"android/values-de/strings.xml",
		"translation/glossary/de.yaml",
		"translation/state/de.yaml",
		"locale_tables.json",
		"verified",
	} {
		assert.FileExists(t, filepath.Join(dir, rel))
	}
	assert.Contains(t, readFile(t, filepath.Join(dir, "ios/Localizable.xcstrings")), `"de" : {`)
	// The master is never a target.
	assert.NoFileExists(t, filepath.Join(dir, "taxonomies/names/en-GB.yaml"))

	// A reference crosses the wire as a placeholder and reaches the file in
	// the syntax the renderer reads.
	assert.Contains(t, readFile(t, filepath.Join(dir, "catalogue/de.yaml")), "@:brand.name")
	// A tree whose nodes are names contributes its label and none of its nodes.
	var names map[string]struct {
		Label string            `yaml:"label"`
		Nodes map[string]string `yaml:"nodes"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(readFile(t, filepath.Join(dir, "taxonomies/names/de.yaml"))), &names))
	assert.Equal(t, "Brand", names["brand"].Label)
	assert.Empty(t, names["brand"].Nodes)
	assert.Equal(t, "Cars", names["category"].Nodes["cars"])

	// The second run is the one that matters for a build: nothing stale, no
	// request, no write.
	require.NoError(t, os.Remove(filepath.Join(dir, "verified")))
	before := atomic.LoadInt64(calls)
	second := mustRun(t, dir, Options{Client: c})
	assert.True(t, second.Clean(), "a second run must be a no-op, got %+v", second)
	assert.Equal(t, before, atomic.LoadInt64(calls), "a clean run must not call the model")
	assert.NoFileExists(t, filepath.Join(dir, "verified"), "nothing written, nothing to verify")
}

// A derived output is the shipped set worked out for a reader that cannot work
// it out itself, under the comment the manifest gives it.
func TestRun_DerivedOutputs(t *testing.T) {
	dir := fixture(t, "de", "ru")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})

	var tables struct {
		Comment   []string                   `json:"_comment"`
		Plurals   map[string][]string        `json:"plurals"`
		Languages map[string]json.RawMessage `json:"languages"`
	}
	require.NoError(t, json.Unmarshal([]byte(readFile(t, filepath.Join(dir, "locale_tables.json"))), &tables))
	assert.Equal(t, []string{"GENERATED by bin/demo translate from translate.yaml.", "Plural categories and matcher verdicts.", "Read by the browser."}, tables.Comment)
	assert.Equal(t, []string{"one", "few", "many", "other"}, tables.Plurals["ru"])
	assert.Contains(t, tables.Languages, "de")

	// A derived output edited by hand is out of date to a check, like any
	// translation.
	replaceInFile(t, filepath.Join(dir, "locale_tables.json"), `"few"`, `"several"`)
	check := mustRun(t, dir, Options{Check: true})
	assert.Equal(t, []string{filepath.Join(dir, "locale_tables.json")}, check.Files)
	assert.False(t, check.Clean())
}

// A digest sees what a string says, not where the master puts it: a reordered
// page or catalogue moves no digest, yet every locale's file has to follow.
// A check holds the files to what a run would write, and a run writes it.
func TestRun_ReorderedMasterIsOutOfDate(t *testing.T) {
	dir := fixture(t, "de")
	c, calls := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})

	replaceInFile(t, filepath.Join(dir, "content/about/en-GB.txt"),
		"This is a **notice board** on {site_name}.\n\n - Free to use\n - [See an example](/U7DGN95PR51)",
		" - Free to use\n - [See an example](/U7DGN95PR51)\n\nThis is a **notice board** on {site_name}.")
	replaceInFile(t, filepath.Join(dir, "catalogue/en-GB.yaml"), "    submit: Submit\n    cancel: Cancel\n", "    cancel: Cancel\n    submit: Submit\n")

	check := mustRun(t, dir, Options{Check: true})
	assert.Zero(t, check.Stale["de"], "no string moved")
	assert.ElementsMatch(t, []string{filepath.Join(dir, "content/about/de.txt"), filepath.Join(dir, "catalogue/de.yaml")}, check.Files)
	assert.False(t, check.Clean())
	assert.Zero(t, check.ToRender(), "behind on files alone asks nothing of the model")
	assert.NotContains(t, readFile(t, filepath.Join(dir, "content/about/de.txt")), " - Free to use\n - [See an example](/U7DGN95PR51)\n\nThis is", "a check writes nothing")

	atomic.StoreInt64(calls, 0)
	mustRun(t, dir, Options{Client: c})
	assert.Zero(t, atomic.LoadInt64(calls), "following the master's order asks nothing of the model")
	assert.Contains(t, readFile(t, filepath.Join(dir, "content/about/de.txt")), " - Free to use\n - [See an example](/U7DGN95PR51)\n\nThis is")
	assert.Less(t, strings.Index(readFile(t, filepath.Join(dir, "catalogue/de.yaml")), "cancel"), strings.Index(readFile(t, filepath.Join(dir, "catalogue/de.yaml")), "submit"))
	assert.True(t, mustRun(t, dir, Options{Check: true}).Clean())
}

func TestRun_CheckReportsWithoutWriting(t *testing.T) {
	dir := fixture(t, "de")
	result := mustRun(t, dir, Options{Check: true})
	assert.Equal(t, units(t, dir, ""), result.Stale["de"])
	assert.Equal(t, units(t, dir, ""), result.ToRender())
	assert.ElementsMatch(t, []string{"gl", "ui", "tax", "content", "email", "push", "app", "ios"}, result.Policy["de"], "a locale never rendered owes every pass")
	assert.NotContains(t, result.Files, filepath.Join(dir, "ios/Localizable.xcstrings"), "a shared file holding no locale's part yet is as a run would write it")
	assert.False(t, result.Clean())
	assert.Empty(t, result.Written, "check writes nothing")
	assert.NoFileExists(t, filepath.Join(dir, "catalogue/de.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, "locale_tables.json"))
}

// An edit to one source string re-renders that string and nothing else.
func TestRun_SourceEditRetranslatesOnlyThatUnit(t *testing.T) {
	dir := fixture(t, "de")
	c, calls := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})

	replaceInFile(t, filepath.Join(dir, "catalogue/en-GB.yaml"), "cancel: Cancel", "cancel: Never mind")
	atomic.StoreInt64(calls, 0)

	result := mustRun(t, dir, Options{Client: c})
	assert.Equal(t, 1, result.Stale["de"])
	assert.Equal(t, int64(1), atomic.LoadInt64(calls), "one changed string is one request")
	assert.Contains(t, readFile(t, filepath.Join(dir, "catalogue/de.yaml")), "Never mind")
}

// Every prompt carries the locale's rendered glossary, so correcting one term's
// rendering re-renders every string that was produced against the old one —
// which is the whole point of having a glossary rather than hoping for
// consistency.
func TestRun_GlossaryRenderingEditRetranslatesEverything(t *testing.T) {
	dir := fixture(t, "de")
	c, _ := scriptedClient(t)
	first := mustRun(t, dir, Options{Client: c})

	replaceInFile(t, filepath.Join(dir, "translation/glossary/de.yaml"), "listing: listing", "listing: Anzeige")
	result := mustRun(t, dir, Options{Client: c})
	assert.Equal(t, first.Stale["de"]-units(t, dir, "gl:"), result.Stale["de"], "every unit but the vocabulary's own is rendered again")
	// The correction itself stands: its source did not move.
	assert.Contains(t, readFile(t, filepath.Join(dir, "translation/glossary/de.yaml")), "listing: Anzeige")
}

// A definition edited in the policy re-renders that term, and the rest only if
// the rendering it produces actually moves.
func TestRun_TermDefinitionEditRetranslatesThatTerm(t *testing.T) {
	dir := fixture(t, "de")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})

	replaceInFile(t, filepath.Join(dir, "translation/policy.yaml"),
		"listing: An advertisement a user posts.", "listing: A notice offering or seeking something.")
	assert.Equal(t, 1, mustRun(t, dir, Options{Client: c}).Stale["de"])
}

// A key removed from the master leaves both the file and the record.
func TestRun_RemovedKeyLeavesTheFile(t *testing.T) {
	dir := fixture(t, "de")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})
	require.Contains(t, readFile(t, filepath.Join(dir, "catalogue/de.yaml")), "cancel")

	replaceInFile(t, filepath.Join(dir, "catalogue/en-GB.yaml"), "    cancel: Cancel\n", "")
	assert.Equal(t, 1, mustRun(t, dir, Options{Check: true}).Removed["de"])
	result := mustRun(t, dir, Options{Client: c})
	assert.Equal(t, 1, result.Removed["de"])
	assert.NotContains(t, readFile(t, filepath.Join(dir, "catalogue/de.yaml")), "cancel")
	assert.True(t, mustRun(t, dir, Options{Check: true}).Clean())
}

// A translation deleted by hand — or lost in a merge — is work to do, even
// though the recorded digest still matches its source. Without this the run
// would call itself clean while the locale rendered the master's language.
func TestRun_HandDeletedTranslationIsStale(t *testing.T) {
	dir := fixture(t, "de")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})

	catalogue := filepath.Join(dir, "catalogue/de.yaml")
	replaceInFile(t, catalogue, "    cancel: Cancel\n", "")
	assert.Equal(t, 1, mustRun(t, dir, Options{Client: c}).Stale["de"])
	assert.Contains(t, readFile(t, catalogue), "cancel")
}

// A hand correction survives every later run, because the state digests the
// source rather than the translation.
func TestRun_HandCorrectionSurvives(t *testing.T) {
	dir := fixture(t, "de")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})

	catalogue := filepath.Join(dir, "catalogue/de.yaml")
	replaceInFile(t, catalogue, "cancel: Cancel", "cancel: Abbrechen")
	assert.True(t, mustRun(t, dir, Options{Client: c}).Clean())
	assert.Contains(t, readFile(t, catalogue), "Abbrechen")
}

// --retranslate is how a correction is pushed through strings already rendered.
func TestRun_RetranslatePrefixForcesRerender(t *testing.T) {
	dir := fixture(t, "de")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})

	catalogue := filepath.Join(dir, "catalogue/de.yaml")
	replaceInFile(t, catalogue, "cancel: Cancel", "cancel: Abbrechen")
	result := mustRun(t, dir, Options{Client: c, Retranslate: []string{"ui:common."}})
	assert.Equal(t, 5, result.Stale["de"])
	assert.Contains(t, readFile(t, catalogue), "cancel: Cancel")
}

// A block inserted into a page translates that block; the ones around it keep
// the translations they had, because a block is addressed by its own digest.
func TestRun_ContentBlockInsertion(t *testing.T) {
	dir := fixture(t, "de")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})

	target := filepath.Join(dir, "content/about/de.txt")
	replaceInFile(t, target, "This is a **notice board** on {site_name}.", "Dies ist ein **Brett** auf {site_name}.")
	replaceInFile(t, filepath.Join(dir, "content/about/en-GB.txt"),
		"This is a **notice board** on {site_name}.", "This is a **notice board** on {site_name}.\n\nEntirely new paragraph.")

	assert.Equal(t, 1, mustRun(t, dir, Options{Client: c}).Stale["de"], "only the new block")
	out := readFile(t, target)
	assert.Contains(t, out, "Dies ist ein **Brett** auf {site_name}.", "the hand-corrected block kept its translation")
	assert.Contains(t, out, "Entirely new paragraph.")
	assert.Contains(t, out, "::TOC::", "the marker block is reproduced")
}

// A page whose block count no longer matches the record cannot be aligned by
// anything, and guessing would attach translations to the wrong blocks.
func TestRun_ContentMisalignmentErrs(t *testing.T) {
	dir := fixture(t, "de")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})

	target := filepath.Join(dir, "content/about/de.txt")
	require.NoError(t, os.WriteFile(target, []byte(readFile(t, target)+"\nAn extra paragraph nobody recorded.\n"), 0o644))
	_, err := run(t, dir, Options{Client: c})
	assert.ErrorContains(t, err, "blocks but the translator recorded")
}

// Dropping a tag from the shipped set has to be as complete as adding one.
func TestRun_DelistedLocaleIsRemoved(t *testing.T) {
	dir := fixture(t, "de", "fr")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})
	require.FileExists(t, filepath.Join(dir, "catalogue/fr.yaml"))

	require.Contains(t, readFile(t, filepath.Join(dir, "ios/Localizable.xcstrings")), `"fr" : {`)

	ship(t, dir, "de")
	assert.Equal(t, 1, mustRun(t, dir, Options{Check: true}).Removed["fr"])
	assert.Equal(t, 1, mustRun(t, dir, Options{Client: c}).Removed["fr"])
	for _, rel := range []string{"catalogue/fr.yaml", "taxonomies/names/fr.yaml", "email_templates/fr.yaml", "push_templates/fr.yaml", "content/about/fr.txt", "android/values-fr/strings.xml", "translation/state/fr.yaml", "translation/glossary/fr.yaml"} {
		assert.NoFileExists(t, filepath.Join(dir, rel))
	}
	assert.FileExists(t, filepath.Join(dir, "catalogue/de.yaml"), "the shipped locale is untouched")
	catalog := readFile(t, filepath.Join(dir, "ios/Localizable.xcstrings"))
	assert.NotContains(t, catalog, `"fr" : {`, "a shared file loses the locale's part")
	assert.Contains(t, catalog, `"de" : {`, "and keeps the others'")
	assert.True(t, mustRun(t, dir, Options{Check: true}).Clean())
}

// Every locale writes the one shared file at once, each its own part, and a
// run over what they wrote writes nothing.
func TestRun_SharedFileWrittenByEveryLocale(t *testing.T) {
	dir := fixture(t, "de", "fr", "ru")
	c, _ := scriptedClient(t)
	catalogPath := filepath.Join(dir, "ios/Localizable.xcstrings")
	first := mustRun(t, dir, Options{Client: c})
	assert.Equal(t, 1, countOf(first.Written, catalogPath), "the shared file is one file written, whichever locales wrote it")

	// Laid out as no run writes it, it is one file behind, not three.
	compact := strings.NewReplacer("\n", "", "  ", "").Replace(readFile(t, catalogPath))
	require.NoError(t, os.WriteFile(catalogPath, []byte(compact), 0o644))
	assert.Equal(t, []string{catalogPath}, mustRun(t, dir, Options{Check: true}).Files)
	mustRun(t, dir, Options{Client: c})

	m := load(t, dir)
	master, err := m.Master()
	require.NoError(t, err)
	catalogStore := master.stores[len(m.Sources)-1]
	require.Equal(t, "ios", m.Sources[len(m.Sources)-1].ID)
	for _, locale := range []string{"de", "fr", "ru"} {
		held, err := catalogStore.Existing(locale, nil)
		require.NoError(t, err)
		assert.Len(t, held, 2, locale)
	}
	catalog := readFile(t, catalogPath)
	assert.Contains(t, catalog, `"few" : {`, "Russian's plural in its own categories")
	assert.Contains(t, catalog, `"value" : "%lld photos"`)
	result := mustRun(t, dir, Options{Check: true})
	assert.True(t, result.Clean(), "%+v", result)
}

// Every generated file opens with the header in its own comment syntax — a
// YAML file's byte for byte as it always has, an Android resource file's after
// its XML declaration — and a String Catalog, which has none, carries none.
func TestRun_HeaderFramedPerFormat(t *testing.T) {
	dir := fixture(t, "de")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})

	header := `# GENERATED by bin/demo translate from the en-GB master. Do not add or remove keys.
# An edit to a value here is kept until its source string, this locale's
# rendering of the terms (translation/glossary/, beside translate.yaml) or the
# translation policy changes, at which point the string is rendered again.
`
	for _, rel := range []string{"catalogue/de.yaml", "taxonomies/names/de.yaml", "email_templates/de.yaml", "push_templates/de.yaml"} {
		assert.True(t, strings.HasPrefix(readFile(t, filepath.Join(dir, rel)), header), rel)
	}
	assert.True(t, strings.HasPrefix(readFile(t, filepath.Join(dir, "catalogue/de.yaml")), header+"common:\n    submit: Submit\n"))
	assert.True(t, strings.HasPrefix(readFile(t, filepath.Join(dir, "translation/glossary/de.yaml")), `# GENERATED by bin/demo translate from the en-GB master.
# Unlike the other generated files, correcting a value here makes EVERY string of
# this locale stale — all of them were rendered against these. Run
# bin/demo translate afterwards and commit the re-render with the edit; until it
# runs, a check of the translations fails.
terms:
`))
	assert.True(t, strings.HasPrefix(readFile(t, filepath.Join(dir, "translation/state/de.yaml")),
		"# GENERATED by bin/demo translate: what each translation of this locale was made from.\npolicy:\n"))
	assert.True(t, strings.HasPrefix(readFile(t, filepath.Join(dir, "android/values-de/strings.xml")), `<?xml version="1.0" encoding="utf-8"?>
<!--
    GENERATED by bin/demo translate from the en-GB master. Do not add or remove keys.
    An edit to a value here is kept until its source string, this locale's
    rendering of the terms (translation/glossary/, beside translate.yaml) or the
    translation policy changes, at which point the string is rendered again.
-->
<resources xmlns:xliff="urn:oasis:names:tc:xliff:document:1.2">
`))
	assert.NotContains(t, readFile(t, filepath.Join(dir, "ios/Localizable.xcstrings")), "GENERATED")
	assert.NotContains(t, readFile(t, filepath.Join(dir, "content/about/de.txt")), "GENERATED")
}

// The locale a plural message is written for decides how many forms it
// carries, in both syntaxes — and the model was never asked for more than one
// form at a time to get there.
func TestRun_PluralFormsFollowTheLocale(t *testing.T) {
	dir := fixture(t, "ru")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})

	catalogue := readFile(t, filepath.Join(dir, "catalogue/ru.yaml"))
	line := lineContaining(t, catalogue, "listing_count")
	assert.Equal(t, 3, strings.Count(line, "|"), "Russian carries four forms: %s", line)
	hub := lineContaining(t, catalogue, "hub_description")
	assert.Contains(t, hub, "{count, plural, one {# listing} few {# listings} many {# listings} other {# listings}}", hub)
}

// A model that drops a placeholder is caught before its batch is written —
// once at the tier asked for, and again, told what was wrong and asked for
// several renderings, at the strong one.
// An item none of whose renderings pass fails alone: it stays owed, everything
// else — the rest of its batch, the locale's other batches and every other
// locale — is rendered and written, and the run fails naming what was
// rejected. The next run owes exactly those strings.
func TestRun_ValidationFailureFailsItsStringAlone(t *testing.T) {
	dir := fixture(t, "de", "fr")
	var retried llm.Request
	c := client(t, func(req llm.Request) (json.RawMessage, error) {
		if req.Tier == llm.TierStrong && strings.Contains(req.System, "previous answer was rejected") {
			retried = req
		}
		return echo(t, req, func(id, text string) string {
			if req.Label != "translate de ui/common" {
				return text
			}
			// Every rendering of the item alike.
			switch base, _, _ := strings.Cut(id, renderingSeparator); base {
			case "ui:common.greeting":
				return "Hallo!"
			case "ui:common.listing_count@other":
				return "listings"
			}
			return text
		})
	})
	_, err := run(t, dir, Options{Client: c})
	require.ErrorContains(t, err, "2 failure(s)")
	assert.ErrorContains(t, err, "de ui:common.greeting: none of 3 renderings passed")
	assert.ErrorContains(t, err, "de ui:common.listing_count: none of 3 renderings passed")
	assert.ErrorContains(t, err, "#1: \"Hallo!\"\n      placeholder mismatch")
	assert.ErrorContains(t, err, "#3: \"Hallo!\"\n      placeholder mismatch")
	assert.Contains(t, retried.System, "ui:common.greeting: placeholder mismatch", "the retry is told what was wrong")
	assert.Contains(t, retried.System, `You wrote: "Hallo!"`, "and shown what it wrote")
	assert.Contains(t, retried.System, `You wrote: @one: "{n} listing", @other: "listings"`, "a plural form by form")
	assert.Contains(t, retried.User, `"ui:common.greeting#3"`, "each item is asked for as several renderings")
	assert.Contains(t, retried.User, `"ui:common.listing_count@other#3"`, "a plural's forms included")

	catalogue := readFile(t, filepath.Join(dir, "catalogue/de.yaml"))
	assert.NotContains(t, catalogue, "greeting", "the failed strings are not written")
	assert.NotContains(t, catalogue, "listing_count")
	assert.Contains(t, catalogue, "cancel", "the rest of their batch is")
	assert.Contains(t, catalogue, "hub_description", "and the locale's other batches")
	assert.FileExists(t, filepath.Join(dir, "taxonomies/names/de.yaml"))

	check := mustRun(t, dir, Options{Check: true})
	assert.Equal(t, 2, check.Stale["de"])
	assert.Zero(t, check.Stale["fr"], "another locale is not held back")
	good, _ := scriptedClient(t)
	assert.Equal(t, 2, mustRun(t, dir, Options{Client: good}).Stale["de"])
	assert.True(t, mustRun(t, dir, Options{Check: true}).Clean())
}

// A text spelled in \u escapes is refused however well it decodes — a model
// writing that way has drifted, and its words with it — and the retry is told
// to write every character as itself.
func TestRun_TextSpelledInEscapesIsRetried(t *testing.T) {
	dir := fixture(t, "de")
	var retried llm.Request
	c := client(t, func(req llm.Request) (json.RawMessage, error) {
		out, err := echo(t, req, nil)
		if req.Label != "translate de ui/common" {
			return out, err
		}
		if req.Tier == llm.TierStrong {
			retried = req
			return out, err
		}
		return json.RawMessage(strings.Replace(string(out), `"Cancel"`, `"`+spelled("Cancel")+`"`, 1)), err
	})
	mustRun(t, dir, Options{Client: c})
	assert.Contains(t, retried.System, `ui:common.cancel: the text of ui:common.cancel is spelled in \u escapes: write every character as itself`)
	assert.Contains(t, readFile(t, filepath.Join(dir, "catalogue/de.yaml")), "cancel: Cancel")

	// Spelled that way at both tiers, in every rendering, the batch fails, and
	// its message quotes the text as the model spelled it — the evidence of
	// what was wrong.
	dir = fixture(t, "de")
	stuck := client(t, func(req llm.Request) (json.RawMessage, error) {
		out, err := echo(t, req, nil)
		if req.Label != "translate de ui/common" {
			return out, err
		}
		return json.RawMessage(strings.ReplaceAll(string(out), `"Cancel"`, `"`+spelled("Cancel")+`"`)), err
	})
	_, err := run(t, dir, Options{Client: stuck})
	require.ErrorContains(t, err, "1 failure(s)")
	assert.ErrorContains(t, err, "#1: \""+spelled("Cancel")+"\"")
}

// A limit is stated to the model, and a translation over it is refused like
// any other defect. A model cannot count characters: shown what it wrote and
// told how long that was and may be, it writes the same phrase again. So the
// retry asks for renderings each shorter than the one before, and the first
// that fits is kept.
func TestRun_OverLimitIsRetried(t *testing.T) {
	dir := fixture(t, "de")
	var first, retried llm.Request
	c := client(t, func(req llm.Request) (json.RawMessage, error) {
		if !strings.HasPrefix(req.Label, "translate de push/") {
			return echo(t, req, nil)
		}
		if req.Tier == llm.TierStrong {
			retried = req
			return echo(t, req, func(id, text string) string {
				switch id {
				case "push:push_offer.subject#1":
					return "Ein Angebot für {listing_title} ist eingegangen"
				case "push:push_offer.subject#2":
					return "Angebot für {listing_title}"
				}
				return text
			})
		}
		first = req
		return echo(t, req, func(id, text string) string {
			if id == "push:push_offer.subject" {
				return "Ein Angebot für {listing_title} ist soeben eingegangen"
			}
			return text
		})
	})
	mustRun(t, dir, Options{Client: c})
	assert.Contains(t, first.User, `At most 40 characters.`)
	assert.Contains(t, first.System, `An item whose context says "At most N characters." is no longer than that`)
	assert.Contains(t, retried.System, "push:push_offer.subject: this item is 54 characters long and may be at most 40: say it more briefly\n  You wrote: \"Ein Angebot für {listing_title} ist soeben eingegangen\"")
	assert.Contains(t, retried.System, "for an item over its limit, shorter than the one before")
	assert.Contains(t, readFile(t, filepath.Join(dir, "push_templates/de.yaml")), "subject: Angebot für {listing_title}", "the first rendering that fits")
}

// A failed field fails its row: a row is written only whole, so the fields that
// passed stay owed with it.
func TestRun_FailedFieldHoldsItsRowBack(t *testing.T) {
	dir := fixture(t, "de")
	c := client(t, func(req llm.Request) (json.RawMessage, error) {
		return echo(t, req, func(id, text string) string {
			if base, _, _ := strings.Cut(id, renderingSeparator); base == "push:push_offer.subject" {
				return "Ein Angebot für {listing_title} ist soeben eingegangen"
			}
			return text
		})
	})
	_, err := run(t, dir, Options{Client: c})
	require.ErrorContains(t, err, "1 failure(s)")
	assert.ErrorContains(t, err, "de push:push_offer.subject: none of 3 renderings passed")
	assert.NoFileExists(t, filepath.Join(dir, "push_templates/de.yaml"), "neither field of the only row is written")
	assert.Equal(t, units(t, dir, "push:"), mustRun(t, dir, Options{Check: true}).Stale["de"])
}

// cutting answers as a backend does whose completion its handler says ran into
// the cap.
type cutting struct {
	handler func(req llm.Request) (json.RawMessage, bool, error)
}

func (c cutting) Complete(_ context.Context, req llm.Request) (llm.Completion, error) {
	out, truncated, err := c.handler(req)
	return llm.Completion{JSON: out, Truncated: truncated}, err
}

// cutWhileWriting is an echoed answer cut off as a model looping inside the
// text of the item id leaves it: every item before it whole, its text a run of
// the same escape, nothing after it.
func cutWhileWriting(t *testing.T, req llm.Request, id string) json.RawMessage {
	t.Helper()
	full, err := echo(t, req, nil)
	require.NoError(t, err)
	at := strings.Index(string(full), `{"id":"`+id+`"`)
	require.Positive(t, at, "%s is asked for, after another item", id)
	return json.RawMessage(string(full[:at]) + `{"id":"` + id + `","text":"Hallo` + strings.Repeat(`\b`, 300))
}

// An answer cut off at the cap — a model looping inside one string — keeps
// every item it finished. The string it was cut off writing is asked for again
// at the strong tier, told it never ended and shown how it began, and the items
// it never reached are asked for again as a batch of their own: one looping
// string costs one string's retry.
func TestRun_CutOffAnswerKeepsWhatItFinished(t *testing.T) {
	dir := fixture(t, "de")
	var mu sync.Mutex
	var asked []llm.Request
	c := llm.WithValidation(cutting{handler: func(req llm.Request) (json.RawMessage, bool, error) {
		if req.Label != "translate de ui/common" {
			out, err := echo(t, req, nil)
			return out, false, err
		}
		mu.Lock()
		asked = append(asked, req)
		first := len(asked) == 1
		mu.Unlock()
		if first {
			return cutWhileWriting(t, req, "ui:common.greeting"), true, nil
		}
		out, err := echo(t, req, nil)
		return out, false, err
	}}, nil)
	mustRun(t, dir, Options{Client: c})

	require.Len(t, asked, 3, "the batch, the string it was cut off writing, and what it never reached")
	var retried, rest llm.Request
	for _, req := range asked[1:] {
		if req.Tier == llm.TierStrong {
			retried = req
		} else {
			rest = req
		}
	}
	assert.Contains(t, retried.System, "ui:common.greeting: your answer was cut off at the length limit while you were writing it: it never ended")
	assert.Contains(t, retried.System, `You wrote: "Hallo\b\b\b`)
	assert.Contains(t, retried.User, `"ui:common.greeting#1"`)
	assert.NotContains(t, retried.User, "ui:common.submit", "what was finished is not asked for again")
	assert.Contains(t, rest.User, `"ui:common.linked"`, "what it never reached is asked for again")
	assert.NotContains(t, rest.User, "ui:common.greeting")
	assert.NotContains(t, rest.User, "ui:common.submit")
	assert.True(t, mustRun(t, dir, Options{Check: true}).Clean())
}

// An answer cut off before any item in it is whole leaves nothing to keep and
// no one string to blame: its batch fails, as a request that went wrong.
func TestRun_CutOffWithNothingWholeFailsItsBatch(t *testing.T) {
	dir := fixture(t, "de")
	c := llm.WithValidation(cutting{handler: func(req llm.Request) (json.RawMessage, bool, error) {
		if req.Label == "translate de ui/seo" {
			return json.RawMessage(`{"items":[{"id":"ui:se`), true, nil
		}
		out, err := echo(t, req, nil)
		return out, false, err
	}}, nil)
	_, err := run(t, dir, Options{Client: c})
	require.ErrorContains(t, err, "1 failure(s)")
	assert.ErrorContains(t, err, "cut off at the length limit before it was complete, with no item in it whole")
	assert.Equal(t, units(t, dir, "ui:seo."), mustRun(t, dir, Options{Check: true}).Stale["de"])
}

// A limit reaches the prompt, so it is policy: changing one owes its pass a
// render.
func TestRun_LimitChangeIsStalenessForItsPass(t *testing.T) {
	dir := fixture(t, "de")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})

	replaceInFile(t, filepath.Join(dir, "translate.yaml"), "limit: 40", "limit: 30")
	check := mustRun(t, dir, Options{Check: true})
	assert.Equal(t, units(t, dir, "push:"), check.Stale["de"])
	assert.Equal(t, []string{"push"}, check.Policy["de"])
}

// Every other string of a locale is rendered against the glossary its
// vocabulary settles, so a failed vocabulary holds that locale back — and no
// other.
func TestRun_VocabularyFailureHoldsItsLocaleBack(t *testing.T) {
	dir := fixture(t, "de", "fr")
	var asked []string
	var logged []string
	c := client(t, func(req llm.Request) (json.RawMessage, error) {
		if strings.HasPrefix(req.Label, "translate de ") {
			asked = append(asked, req.Label)
		}
		if req.Label == "translate de gl/" {
			return nil, errors.New("openai_compat: status 500: the runtime fell over")
		}
		return echo(t, req, nil)
	})
	_, err := run(t, dir, Options{Client: c, Log: func(line string) { logged = append(logged, line) }})
	require.ErrorContains(t, err, "1 failure(s)")
	assert.Equal(t, []string{"translate de gl/"}, asked, "nothing of the locale is asked for past its vocabulary")
	assert.Contains(t, lineContaining(t, strings.Join(logged, "\n"), "de: "), ", 1 failed")
	assert.True(t, strings.HasPrefix(lineContaining(t, strings.Join(logged, "\n"), "de: "), "de: 0 translated"))
	assert.Zero(t, mustRun(t, dir, Options{Check: true}).Stale["fr"])
}

// A count of requests in flight is a count.
func TestRun_NegativeParallelErrs(t *testing.T) {
	_, err := run(t, fixture(t, "de"), Options{Parallel: -1})
	assert.ErrorContains(t, err, "cannot be negative")
}

// What goes wrong with one request is that request's batch's: a request that
// outran its timeout — a model looping inside its answer — or one the server
// refused. The run carries on and reports it.
func TestRun_RequestFailuresFailTheirBatchAlone(t *testing.T) {
	for name, fault := range map[string]error{
		"timed out": &url.Error{Op: "Post", URL: "http://127.0.0.1:1/v1/chat/completions", Err: context.DeadlineExceeded},
		"refused":   errors.New("openai_compat: status 400: this request exceeds the model length"),
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "de")
			c := client(t, func(req llm.Request) (json.RawMessage, error) {
				if req.Label == "translate de ui/seo" {
					return nil, fault
				}
				return echo(t, req, nil)
			})
			_, err := run(t, dir, Options{Client: c})
			require.ErrorContains(t, err, "1 failure(s)")
			assert.ErrorContains(t, err, fault.Error())
			assert.Equal(t, units(t, dir, "ui:seo."), mustRun(t, dir, Options{Check: true}).Stale["de"])
		})
	}
}

// An endpoint that does not answer at all stops the run: every other request
// would meet it too, and a person — or the GPU launcher, reopening its tunnel —
// has to act first. What was rendered before is kept, and the next run owes
// exactly the rest.
func TestRun_UnreachableEndpointStopsTheRunAndResumes(t *testing.T) {
	dir := fixture(t, "de")
	refused := &url.Error{Op: "Post", URL: "http://127.0.0.1:1/v1/chat/completions", Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}}
	stopped := client(t, func(req llm.Request) (json.RawMessage, error) {
		if req.Label == "translate de ui/seo" {
			return nil, refused
		}
		return echo(t, req, nil)
	})
	_, err := run(t, dir, Options{Client: stopped, Parallel: 1})
	require.ErrorContains(t, err, "connection refused")
	assert.NotContains(t, err.Error(), "failure(s)", "not one batch's failure but the run's")
	assert.FileExists(t, filepath.Join(dir, "translation/glossary/de.yaml"), "the vocabulary went first and was written")

	// A check calls the locale stale rather than broken — owing at least the
	// batch that met the endpoint, and not the vocabulary delivered before it;
	// which other batches got a slot first is the scheduler's business — and
	// the next run owes exactly what the stopped one did not deliver.
	owed := mustRun(t, dir, Options{Check: true}).Stale["de"]
	assert.GreaterOrEqual(t, owed, units(t, dir, "ui:seo."))
	assert.LessOrEqual(t, owed, units(t, dir, "")-units(t, dir, "gl:"))
	c, _ := scriptedClient(t)
	assert.Equal(t, owed, mustRun(t, dir, Options{Client: c}).Stale["de"])
	assert.True(t, mustRun(t, dir, Options{Client: c}).Clean())
}

// Locales render at once, on one set of request slots: while one locale's
// vocabulary is in flight another's is asked for too, and the run never has
// more requests in flight than it was given.
func TestRun_LocalesShareTheRequestSlots(t *testing.T) {
	dir := fixture(t, "de", "fr", "ru")
	var inFlight, most, vocabularies int64
	second := make(chan struct{})
	c := client(t, func(req llm.Request) (json.RawMessage, error) {
		n := atomic.AddInt64(&inFlight, 1)
		defer atomic.AddInt64(&inFlight, -1)
		for m := atomic.LoadInt64(&most); n > m && !atomic.CompareAndSwapInt64(&most, m, n); m = atomic.LoadInt64(&most) {
		}
		if strings.HasSuffix(req.Label, " gl/") {
			switch atomic.AddInt64(&vocabularies, 1) {
			case 1:
				// Rendered one locale after another, nothing else would ever
				// be asked for while this is in flight; the bound only turns
				// that into a failure rather than a hang.
				select {
				case <-second:
				case <-time.After(10 * time.Second):
					return nil, errors.New("no other locale was asked for while this vocabulary was in flight")
				}
			case 2:
				close(second)
			}
		}
		return echo(t, req, nil)
	})
	mustRun(t, dir, Options{Client: c, Parallel: 2})
	assert.Equal(t, int64(2), atomic.LoadInt64(&most))
	assert.True(t, mustRun(t, dir, Options{Check: true}).Clean())
}

// Naming the master as a target would overwrite the authored source.
func TestRun_RefusesTheMasterAsATarget(t *testing.T) {
	dir := fixture(t, "de")
	_, err := run(t, dir, Options{Only: []string{"en-GB"}})
	assert.ErrorContains(t, err, "is the master, not a target")
	_, err = run(t, dir, Options{Only: []string{"fr"}})
	assert.ErrorContains(t, err, "fr is not shipped")
}

// Work to do and no provider is a stated failure, not a silent fallback.
func TestRun_NoProviderErrsWhenThereIsWork(t *testing.T) {
	_, err := run(t, fixture(t, "de"), Options{})
	assert.ErrorContains(t, err, "set TRANSLATE_LLM_PROVIDER")
}

// Deleting a locale's state file is how a person says "render this again": the
// prose on disk cannot be addressed without the order that was recorded with
// it, so the page is simply produced afresh rather than refused.
func TestRun_DeletedStateRerendersRatherThanErrs(t *testing.T) {
	dir := fixture(t, "de")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})
	require.NoError(t, os.Remove(filepath.Join(dir, "translation/state/de.yaml")))

	assert.Positive(t, mustRun(t, dir, Options{Client: c}).Stale["de"])
	assert.Contains(t, readFile(t, filepath.Join(dir, "content/about/de.txt")), "::TOC::")
	assert.True(t, mustRun(t, dir, Options{Client: c}).Clean(), "and the run after it is a no-op again")
}

// Every generated file has to be readable back, whatever the strings in it
// look like. A body whose first line opens with a space is the case that
// broke: the emitter states such a block's indentation explicitly and then
// cannot parse what it wrote.
func TestRun_GeneratedFilesRoundTripWhateverTheStringsLookLike(t *testing.T) {
	dir := fixture(t, "de")
	awkward := client(t, func(req llm.Request) (json.RawMessage, error) {
		return echo(t, req, func(_, text string) string {
			if strings.Contains(text, "\n") {
				// What a model does now and then: a stray space before the
				// first line of a multi-line body.
				return " " + text
			}
			return text
		})
	})
	mustRun(t, dir, Options{Client: awkward})

	var rows []map[string]string
	require.NoError(t, yaml.Unmarshal([]byte(readFile(t, filepath.Join(dir, "email_templates/de.yaml"))), &rows))
	assert.True(t, strings.HasPrefix(rows[0]["body"], " <p>Welcome!</p>\n"))
	c, _ := scriptedClient(t)
	assert.True(t, mustRun(t, dir, Options{Client: c}).Clean(), "and the run after it still sees nothing to do")
}

// The register a locale addresses its reader in is product policy, authored
// once and never asked of the model: every prompt for the locale carries it,
// the vocabulary's included; it is no part of the rendered glossary, which is
// output; and a shipped locale without a line is refused when the manifest
// loads — by a check too, since a build must not pass on a locale nobody
// decided it for.
func TestRun_RegisterIsAuthoredAndRequired(t *testing.T) {
	dir := fixture(t, "de")
	seen := int64(0)
	c := client(t, func(req llm.Request) (json.RawMessage, error) {
		atomic.AddInt64(&seen, 1)
		assert.Contains(t, req.System, "How to address the reader in German: Address the reader informally (du).")
		return echo(t, req, nil)
	})
	mustRun(t, dir, Options{Client: c})
	assert.Positive(t, seen)
	assert.NotContains(t, readFile(t, filepath.Join(dir, "translation/glossary/de.yaml")), "Address")

	replaceInFile(t, filepath.Join(dir, "translation/register.yaml"), "de: Address the reader informally (du).\n", "")
	_, err := Load(filepath.Join(dir, "translate.yaml"), nil)
	assert.ErrorContains(t, err, "no register line for de")
}

// A register change is a change to what every string of the locale was
// rendered under.
func TestRun_RegisterChangeRetranslatesEverything(t *testing.T) {
	dir := fixture(t, "de")
	c, _ := scriptedClient(t)
	first := mustRun(t, dir, Options{Client: c})

	replaceInFile(t, filepath.Join(dir, "translation/register.yaml"), "de: Address the reader informally (du).", "de: Address the reader as Sie.")
	assert.Equal(t, first.Stale["de"]-units(t, dir, "gl:"), mustRun(t, dir, Options{Client: c}).Stale["de"],
		"every unit but the vocabulary's own is rendered again")
}

// --- policy ---

// A pass's guidance is policy that pass's units were rendered under, and
// only that pass's.
func TestRun_GuidanceChangeIsStalenessForItsPassAlone(t *testing.T) {
	dir := fixture(t, "de")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})

	replaceInFile(t, filepath.Join(dir, "translate.yaml"), "guidance: These are user-interface strings.", "guidance: These are the app's strings.")
	check := mustRun(t, dir, Options{Check: true})
	assert.Equal(t, units(t, dir, "ui:"), check.Stale["de"])
	assert.Equal(t, []string{"ui"}, check.Policy["de"])
}

// The vocabulary's guidance is the vocabulary's policy: its terms are
// rendered again, and the rest only where a term then renders differently.
func TestRun_VocabularyGuidanceChangeRerendersTheTerms(t *testing.T) {
	dir := fixture(t, "de")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})

	replaceInFile(t, filepath.Join(dir, "translate.yaml"), "guidance: Each id is a term to render into {language}.", "guidance: Each id is a word of the product's, to render into {language}.")
	assert.Equal(t, []string{"gl"}, mustRun(t, dir, Options{Check: true}).Policy["de"])
	assert.Equal(t, units(t, dir, "gl:"), mustRun(t, dir, Options{Client: c}).Stale["de"], "the echo renders every term as it was")

	replaceInFile(t, filepath.Join(dir, "translate.yaml"), "Each id is a word of the product's", "Each id is a term")
	renamed := client(t, func(req llm.Request) (json.RawMessage, error) {
		return echo(t, req, func(id, text string) string {
			if id == "gl:listing" {
				return "Anzeige"
			}
			return text
		})
	})
	assert.Equal(t, units(t, dir, ""), mustRun(t, dir, Options{Client: renamed}).Stale["de"], "a term that moved takes every string rendered against it")
}

// What does not reach a prompt is not policy: tuning a batch or moving a
// source to another tier costs no render.
func TestRun_ThroughputKnobsAreNotStaleness(t *testing.T) {
	dir := fixture(t, "de")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})

	replaceInFile(t, filepath.Join(dir, "translate.yaml"), "batch: {by: first-segment, size: 40}\n    tier: fast", "batch: {by: first-segment, size: 3}\n    tier: strong")
	replaceInFile(t, filepath.Join(dir, "translate.yaml"), "parallel: 2", "parallel: 7")
	assert.True(t, mustRun(t, dir, Options{Check: true}).Clean())
}

// --adopt is the host saying the text on disk still stands under a policy
// that moved: it records the policy and touches no translation, and a unit
// whose own source moved is still owed its render.
func TestRun_AdoptRecordsThePolicyAndNothingElse(t *testing.T) {
	dir := fixture(t, "de", "fr")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})
	catalogue := readFile(t, filepath.Join(dir, "catalogue/de.yaml"))

	replaceInFile(t, filepath.Join(dir, "translate.yaml"), "rules: [Keep user-interface copy about as short as the source.]", "rules: [Keep copy short.]")
	replaceInFile(t, filepath.Join(dir, "catalogue/en-GB.yaml"), "cancel: Cancel", "cancel: Never mind")
	check := mustRun(t, dir, Options{Check: true})
	assert.Equal(t, units(t, dir, ""), check.Stale["de"], "a rule every prompt states is every pass's policy")
	assert.Len(t, check.Policy["de"], 8, "the vocabulary and every source")

	adopted := mustRun(t, dir, Options{Adopt: true})
	assert.ElementsMatch(t, []string{
		filepath.Join(dir, "translation/state/de.yaml"),
		filepath.Join(dir, "translation/state/fr.yaml"),
	}, adopted.Written, "the records and nothing else")
	assert.Equal(t, catalogue, readFile(t, filepath.Join(dir, "catalogue/de.yaml")))

	after := mustRun(t, dir, Options{Check: true})
	assert.Equal(t, 1, after.Stale["de"], "the edited string is still owed")
	assert.Empty(t, after.Policy["de"])
	assert.Equal(t, 1, mustRun(t, dir, Options{Client: c}).Stale["de"])
	assert.True(t, mustRun(t, dir, Options{Check: true}).Clean())
}

// A locale nothing was ever rendered for has nothing to adopt.
func TestRun_AdoptLeavesAnUnrenderedLocaleAlone(t *testing.T) {
	dir := fixture(t, "de")
	mustRun(t, dir, Options{Adopt: true})
	assert.NoFileExists(t, filepath.Join(dir, "translation/state/de.yaml"))
	assert.FileExists(t, filepath.Join(dir, "locale_tables.json"), "the derived outputs are written")
}

// A pass whose policy moved is rendered whole, and a run that falls short of
// it resumes owing exactly what it had not rendered yet.
func TestRun_PolicyRenderResumes(t *testing.T) {
	dir := fixture(t, "de")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})

	replaceInFile(t, filepath.Join(dir, "translate.yaml"), "guidance: These are user-interface strings.", "guidance: These are the app's strings.")
	failing := client(t, func(req llm.Request) (json.RawMessage, error) {
		if req.Label == "translate de ui/seo" {
			return nil, errors.New("openai_compat: status 500: the runtime fell over")
		}
		return echo(t, req, nil)
	})
	_, err := run(t, dir, Options{Client: failing})
	require.ErrorContains(t, err, "1 failure(s)")
	check := mustRun(t, dir, Options{Check: true})
	assert.Equal(t, units(t, dir, "ui:seo."), check.Stale["de"])
	assert.Empty(t, check.Policy["de"])
	mustRun(t, dir, Options{Client: c})
	assert.True(t, mustRun(t, dir, Options{Check: true}).Clean())
}

// --- verify ---

// The host's own consumers have the last word on a full write.
func TestRun_VerifyRefusalFailsTheRun(t *testing.T) {
	dir := fixture(t, "de")
	replaceInFile(t, filepath.Join(dir, "translate.yaml"), `touch verified`, `exit 3`)
	c, _ := scriptedClient(t)
	_, err := run(t, dir, Options{Client: c})
	assert.ErrorContains(t, err, "verify `sh -c exit 3` refused what was just written")
}

// A run restricted to a locale has left the others alone, and settles nothing
// that belongs to the shipped set as a whole.
func TestRun_OneLocaleVerifiesNothing(t *testing.T) {
	dir := fixture(t, "de", "fr")
	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c, Only: []string{"de"}})
	assert.FileExists(t, filepath.Join(dir, "catalogue/de.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, "catalogue/fr.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, "locale_tables.json"))
	assert.NoFileExists(t, filepath.Join(dir, "verified"))
}

// Checking and adopting are different questions.
func TestRun_CheckAndAdoptExcludeEachOther(t *testing.T) {
	_, err := run(t, fixture(t, "de"), Options{Check: true, Adopt: true})
	assert.ErrorContains(t, err, "ask one")
}
