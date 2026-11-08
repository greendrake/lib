package translate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Nothing a declaration gets wrong waits for a run to find it.
func TestLoad_Refusals(t *testing.T) {
	cases := map[string]struct {
		file, old, new, want string
	}{
		"unknown key":             {"translate.yaml", "command:", "commnad:", "field commnad not found"},
		"no command":              {"translate.yaml", "command: bin/demo translate", "command: ''", "`command` is required"},
		"default not shipped":     {"translate.yaml", "default: en-GB", "default: en-US", `the default locale "en-US" is not among the shipped ones`},
		"tag not canonical":       {"translate.yaml", "shipped: [en-GB, de]", "shipped: [en-GB, de, en_US]", `shipped tag "en_US" is not canonical`},
		"tag twice":               {"translate.yaml", "shipped: [en-GB, de]", "shipped: [en-GB, de, de]", "shipped tag de is listed twice"},
		"script without letters":  {"translate.yaml", "shipped: [en-GB, de]", "shipped: [en-GB, de, chr]", "shipped tag chr: chr is written in Cher, a script the engine has no letters for"},
		"pass id twice":           {"translate.yaml", "- id: tax", "- id: ui", "pass id ui is declared twice"},
		"pass id with a colon":    {"translate.yaml", "- id: tax", "- id: 'tax:'", "must be a lower-case word"},
		"unknown tier":            {"translate.yaml", "tier: strong\n  guidance", "tier: best\n  guidance", `gl: tier "best" is neither fast nor strong`},
		"grouped vocabulary":      {"translate.yaml", "batch: {size: 100}", "batch: {by: file, size: 100}", "vocabulary: its terms are one list"},
		"first segment of a tree": {"translate.yaml", "batch: {by: file, size: 60}", "batch: {by: first-segment, size: 60}", "tax: `by: first-segment` groups a catalogue's keys"},
		"format refusal":          {"translate.yaml", `node_id: "{key}:{code}"`, `node_id: "{key}"`, "tax: `node_id` must carry {code}"},
		"unknown derived kind":    {"translate.yaml", "kind: locale_tables", "kind: sitemap", `derived output kind "sitemap"`},
		"no llm parallel":         {"translate.yaml", "parallel: 2", "parallel: 0", "`llm.timeout` and `llm.parallel` must be positive"},
		"empty verify":            {"translate.yaml", `{run: [sh, -c, "touch verified"], cwd: .}`, "{run: [], cwd: .}", "a verify command must say what to run"},
		"no register file":        {"translate.yaml", "  register: translation/register.yaml\n", "", "`policy.register` is required"},
		"no register":             {"translation/register.yaml", "de: Address the reader informally (du).\n", "", "translation/register.yaml: no register line for de"},
		"register for the master": {"translation/register.yaml", "de:", "en-GB: Plainly.\nde:", "en-GB is the master"},
		"register in the policy":  {"translation/policy.yaml", "terms:", "register: {de: Plainly.}\nterms:", "field register not found"},
		"brief in the policy":     {"translation/policy.yaml", "terms:", "brief: A board.\nterms:", "field brief not found"},
		"unknown policy key":      {"translation/policy.yaml", "terms:", "trems:", "field trems not found"},
		"no brief file":           {"translate.yaml", "  brief: translation/brief.yaml\n", "", "`policy.brief` is required"},
		"unknown brief key":       {"translation/brief.yaml", "brief:", "breif:", "field breif not found"},
		"no brief":                {"translation/brief.yaml", "brief: |\n  A classifieds and notice board that spans the world.\n", "brief: ''\n", "translation/brief.yaml: the brief is what every prompt opens with"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "de")
			replaceInFile(t, filepath.Join(dir, tc.file), tc.old, tc.new)
			_, err := Load(filepath.Join(dir, "translate.yaml"), nil)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

// A library manifest declares its master and no locales, and is run on behalf
// of a project: without one it is refused, as is one whose master is another;
// what it would take from one it may not declare itself.
func TestLoad_LibraryRefusals(t *testing.T) {
	_, err := Load(filepath.Join(libraryTree(t), "translate.yaml"), nil)
	assert.ErrorContains(t, err, "declares no locales: it is a library's, run on behalf of a project")

	project := load(t, fixture(t, "de"))
	for name, declaration := range map[string]string{
		"a command":     "command: bin/lib translate\n",
		"a model":       "llm: {env_prefix: TRANSLATE_LLM_, timeout: 10m, parallel: 2}\n",
		"derived files": "derived: [{kind: locale_tables, path: locale_tables.json}]\n",
	} {
		t.Run(name, func(t *testing.T) {
			lib := libraryTree(t)
			replaceInFile(t, filepath.Join(lib, "translate.yaml"), "state:", declaration+"state:")
			_, err := Load(filepath.Join(lib, "translate.yaml"), project)
			assert.ErrorContains(t, err, "a library manifest takes its command and model from the project it runs for")
		})
	}

	t.Run("no master", func(t *testing.T) {
		lib := libraryTree(t)
		replaceInFile(t, filepath.Join(lib, "translate.yaml"), "default: en-GB\n", "")
		_, err := Load(filepath.Join(lib, "translate.yaml"), project)
		assert.ErrorContains(t, err, "`default` is required: a library's master locale is its own")
	})

	t.Run("another master", func(t *testing.T) {
		lib := libraryTree(t)
		replaceInFile(t, filepath.Join(lib, "translate.yaml"), "default: en-GB", "default: de")
		_, err := Load(filepath.Join(lib, "translate.yaml"), project)
		assert.ErrorContains(t, err, "translate.yaml's master is de and ")
		assert.ErrorContains(t, err, "translate.yaml's is en-GB: a library is rendered on a project's behalf from the master they share")
	})
}

// Paths resolve against the manifest, wherever the command runs from.
func TestLoad_PathsResolveAgainstTheManifest(t *testing.T) {
	dir := fixture(t, "de")
	wd, err := os.Getwd()
	require.NoError(t, err)
	rel, err := filepath.Rel(wd, filepath.Join(dir, "translate.yaml"))
	require.NoError(t, err)
	m, err := Load(rel, nil)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "translation/state/de.yaml"), m.statePath("de"))
	_, err = m.Master()
	require.NoError(t, err)
}
