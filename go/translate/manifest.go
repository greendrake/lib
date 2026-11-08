package translate

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"

	"greendrake.nz/go/translate/format"
)

// Manifest is everything the engine knows about a host: what it ships, where
// its translatable files are and in what shape, what its translations are held
// to, and how its own consumers verify the result. Paths resolve against the
// manifest's directory.
//
// A project's manifest declares the locales it ships. A library's declares
// its master and no locales: it is run on behalf of a project whose master is
// the same, in that project's locales, with its model — so a library is
// translated into exactly the locales of the projects serving it, and the files
// it generates name no project, being the same bytes whichever of them writes
// them.
type Manifest struct {
	// Command is how a person runs the translator over a project's tree,
	// named in every file a project manifest generates and in a failed
	// check's report. A library manifest declares none and reports its
	// project's.
	Command string `yaml:"command"`
	// Default is the authored master locale; Shipped is every locale
	// produced, the master included. A tag dropped from Shipped has its files
	// removed by the next full run. A library manifest declares its master,
	// its own authored catalogue's, and takes Shipped from its project.
	Default string   `yaml:"default"`
	Shipped []string `yaml:"shipped"`
	// State is the directory of each locale's record, <State>/<tag>.yaml.
	State  string      `yaml:"state"`
	Policy PolicyFiles `yaml:"policy"`
	// Vocabulary is the pass that settles the policy's terms in each locale
	// before anything else is rendered against them.
	Vocabulary Pass `yaml:"vocabulary"`
	// LLM is a project manifest's; a library manifest runs on its project's.
	LLM     LLMSettings    `yaml:"llm"`
	Prompt  PromptSettings `yaml:"prompt"`
	Sources []Source       `yaml:"sources"`
	// Derived outputs are worked out from a project's locale set, which a
	// library manifest has none of.
	Derived []Derived `yaml:"derived"`
	Verify  []Command `yaml:"verify"`

	dir    string
	file   string
	policy Policy
	// library marks a library manifest, read on behalf of a project.
	library bool
}

// PolicyFiles says where the authored policy is, and where each locale's
// rendering of its terms goes.
type PolicyFiles struct {
	// Path is the manifest's own policy: the terms its vocabulary renders.
	Path string `yaml:"path"`
	// Brief and Register are files of their own: manifests whose strings
	// share a screen — a host and the libraries it renders, the libraries
	// among themselves — address the reader alike, and libraries made for the
	// same apps are described alike, so they name one file.
	Brief    string `yaml:"brief"`
	Register string `yaml:"register"`
	Glossary string `yaml:"glossary"`
}

// Pass is what every pass declares, the vocabulary and each source alike.
type Pass struct {
	// ID prefixes every unit id of the pass: <id>:<key>.
	ID    string `yaml:"id"`
	Batch Batch  `yaml:"batch"`
	// Tier is the capability tier the pass is rendered at: prose the product
	// is judged on gets the strong one, the bulk of short strings need not.
	Tier string `yaml:"tier"`
	// Guidance reaches every prompt of this pass and no other. {language} in
	// it is the target language's English name.
	Guidance string `yaml:"guidance"`
}

// Batch bounds one request of a pass.
type Batch struct {
	// By is what a batch never spans: `file`, a master file; `first-segment`,
	// a catalogue's top-level key. Empty groups the whole pass.
	By string `yaml:"by"`
	// Size bounds a batch in units; zero leaves it unbounded.
	Size int `yaml:"size"`
}

// LLMSettings configure the client the command builds.
type LLMSettings struct {
	// EnvPrefix is the environment block the provider is configured from.
	EnvPrefix string `yaml:"env_prefix"`
	// Timeout bounds one request unless the environment states one. A batch
	// is far larger than a classification, and a local model filling one takes
	// minutes rather than seconds.
	Timeout time.Duration `yaml:"timeout"`
	// Parallel bounds concurrent requests unless the command line states it.
	Parallel int `yaml:"parallel"`
}

// PromptSettings are the host's own rules, stated in every prompt among the
// ones nothing checks.
type PromptSettings struct {
	// KeepVerbatim are other companies', platforms' and products' names.
	KeepVerbatim []string `yaml:"keep_verbatim"`
	Rules        []string `yaml:"rules"`
}

// Source is one pass over a set of the host's files.
type Source struct {
	Pass        `yaml:",inline"`
	format.Spec `yaml:",inline"`
}

// Derived is a file generated from the shipped set rather than translated.
type Derived struct {
	Kind    string `yaml:"kind"`
	Path    string `yaml:"path"`
	Comment string `yaml:"comment"`
}

// Command is one of the host's verification commands, run after a full write.
type Command struct {
	Run []string `yaml:"run"`
	Cwd string   `yaml:"cwd"`
}

// Tiers.
const (
	TierFast   = "fast"
	TierStrong = "strong"
)

// Batch groupings.
const (
	ByFile         = "file"
	ByFirstSegment = "first-segment"
)

var passID = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// Load reads and validates a manifest and the policy it names. Nothing a
// declaration gets wrong waits for a run to find it: an unknown key, format or
// check, a shipped tag that is not canonical, a locale without a register.
//
// project is the project manifest a library manifest is read on behalf of,
// whose command, shipped locales and model it runs on; a library manifest
// without one is refused, as is a project whose master locale is not the
// library's. A project manifest is its own project, and project does not apply
// to it.
func Load(path string, project *Manifest) (*Manifest, error) {
	m := &Manifest{}
	if err := readStrict(path, m); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	m.dir, m.file = filepath.Dir(abs), filepath.Base(abs)
	if len(m.Shipped) == 0 {
		if project == nil {
			return nil, fmt.Errorf("%s declares no locales: it is a library's, run on behalf of a project — name the project's manifest", path)
		}
		if m.Command != "" || m.LLM != (LLMSettings{}) || len(m.Derived) > 0 {
			return nil, fmt.Errorf("%s: a library manifest takes its command and model from the project it runs for, and derives nothing, having no locale set to derive from", path)
		}
		if m.Default == "" {
			return nil, fmt.Errorf("%s: `default` is required: a library's master locale is its own", path)
		}
		if m.Default != project.Default {
			return nil, fmt.Errorf("%s's master is %s and %s's is %s: a library is rendered on a project's behalf from the master they share", path, m.Default, project.location(), project.Default)
		}
		m.library = true
		m.Command, m.Shipped, m.LLM = project.Command, project.Shipped, project.LLM
	}
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := readStrict(m.path(m.Policy.Path), &m.policy); err != nil {
		return nil, err
	}
	var brief struct {
		Brief string `yaml:"brief"`
	}
	if err := readStrict(m.path(m.Policy.Brief), &brief); err != nil {
		return nil, err
	}
	m.policy.Brief = brief.Brief
	if err := readStrict(m.path(m.Policy.Register), &m.policy.Register); err != nil {
		return nil, err
	}
	if err := m.policy.validate(m); err != nil {
		return nil, err
	}
	return m, nil
}

// readStrict decodes an authored YAML file, refusing a key its target does not
// have: a misspelt key read as absent is a declaration silently ignored.
func readStrict(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func (m *Manifest) validate() error {
	if m.Command == "" {
		return errors.New("`command` is required: it is what every generated file tells its reader to run")
	}
	shipped := map[string]bool{}
	for _, tag := range m.Shipped {
		parsed, err := language.Parse(tag)
		if err != nil || parsed.String() != tag {
			return fmt.Errorf("shipped tag %q is not canonical BCP-47", tag)
		}
		if shipped[tag] {
			return fmt.Errorf("shipped tag %s is listed twice", tag)
		}
		if _, err := scriptsOf(tag); err != nil {
			return fmt.Errorf("shipped tag %s: %w", tag, err)
		}
		shipped[tag] = true
	}
	if !shipped[m.Default] {
		return fmt.Errorf("the default locale %q is not among the shipped ones", m.Default)
	}
	for key, value := range map[string]string{"state": m.State, "policy.path": m.Policy.Path, "policy.brief": m.Policy.Brief, "policy.register": m.Policy.Register, "policy.glossary": m.Policy.Glossary, "llm.env_prefix": m.LLM.EnvPrefix} {
		if value == "" {
			return fmt.Errorf("`%s` is required", key)
		}
	}
	if m.LLM.Timeout <= 0 || m.LLM.Parallel < 1 {
		return errors.New("`llm.timeout` and `llm.parallel` must be positive")
	}

	ids := map[string]bool{}
	for _, pass := range m.passes() {
		if !passID.MatchString(pass.ID) {
			return fmt.Errorf("pass id %q must be a lower-case word: it prefixes every unit id", pass.ID)
		}
		if ids[pass.ID] {
			return fmt.Errorf("pass id %s is declared twice", pass.ID)
		}
		ids[pass.ID] = true
		if pass.Tier != TierFast && pass.Tier != TierStrong {
			return fmt.Errorf("%s: tier %q is neither fast nor strong", pass.ID, pass.Tier)
		}
		if pass.Batch.Size < 0 {
			return fmt.Errorf("%s: a batch size cannot be negative", pass.ID)
		}
	}
	if m.Vocabulary.Batch.By != "" {
		return errors.New("vocabulary: its terms are one list, which nothing groups")
	}
	for _, source := range m.Sources {
		switch source.Batch.By {
		case "", ByFile:
		case ByFirstSegment:
			if source.Format != format.Catalogue {
				return fmt.Errorf("%s: `by: first-segment` groups a catalogue's keys, and this is %s", source.ID, source.Format)
			}
		default:
			return fmt.Errorf("%s: batch by %q is neither file nor first-segment", source.ID, source.Batch.By)
		}
		if err := format.Validate(source.ID, source.Spec, m.Shipped); err != nil {
			return err
		}
	}

	paths := map[string]bool{}
	for _, derived := range m.Derived {
		if derivations[derived.Kind] == nil {
			return fmt.Errorf("derived output kind %q is not locale_tables", derived.Kind)
		}
		if derived.Path == "" || paths[derived.Path] {
			return fmt.Errorf("derived %s: its path must be given, and be its own", derived.Kind)
		}
		paths[derived.Path] = true
	}
	for _, command := range m.Verify {
		if len(command.Run) == 0 {
			return errors.New("a verify command must say what to run")
		}
	}
	return nil
}

// passes are the vocabulary and every source, in the order they are rendered.
func (m *Manifest) passes() []Pass {
	out := []Pass{m.Vocabulary}
	for _, source := range m.Sources {
		out = append(out, source.Pass)
	}
	return out
}

// pass is one pass by id.
func (m *Manifest) pass(id string) Pass {
	for _, pass := range m.passes() {
		if pass.ID == id {
			return pass
		}
	}
	panic("translate: no pass " + id)
}

// path resolves a manifest-relative path.
func (m *Manifest) path(rel string) string { return filepath.Join(m.dir, rel) }

// location is the manifest's own path.
func (m *Manifest) location() string { return m.path(m.file) }

// statePath is a locale's record.
func (m *Manifest) statePath(locale string) string {
	return m.path(filepath.Join(m.State, locale+".yaml"))
}

// glossaryPath is a locale's rendering of the terms.
func (m *Manifest) glossaryPath(locale string) string {
	return m.path(filepath.Join(m.Policy.Glossary, locale+".yaml"))
}

// targets are the shipped locales other than the master.
func (m *Manifest) targets() []string {
	var out []string
	for _, tag := range m.Shipped {
		if tag != m.Default {
			out = append(out, tag)
		}
	}
	return out
}

// generator is what a generated file says produced it: a project manifest's
// command, and for a library manifest — whose files any project serving it
// writes, byte for byte alike — the manifest itself.
func (m *Manifest) generator() string {
	if m.library {
		return "the translator over " + m.file
	}
	return m.Command
}

// The headers are text, one line per line: each writer frames them in its own
// file's comment syntax, and a format with none writes them nowhere.

// header tops every translated file.
func (m *Manifest) header() string {
	return fmt.Sprintf(`GENERATED by %s from the %s master. Do not add or remove keys.
An edit to a value here is kept until its source string, this locale's
rendering of the terms (%s/, beside %s) or the
translation policy changes, at which point the string is rendered again.`, m.generator(), m.Default, m.Policy.Glossary, m.file)
}

// glossaryHeader tops a locale's rendering of the terms. It says something
// the other header cannot: every string of the locale was rendered against
// these, so an edit here is not the local correction an edit elsewhere is.
func (m *Manifest) glossaryHeader() string {
	rerun := m.Command
	if m.library {
		rerun = "the translate command of every project serving it"
	}
	return fmt.Sprintf(`GENERATED by %s from the %s master.
Unlike the other generated files, correcting a value here makes EVERY string of
this locale stale — all of them were rendered against these. Run
%s afterwards and commit the re-render with the edit; until it
runs, a check of the translations fails.`, m.generator(), m.Default, rerun)
}

// stateHeader tops a locale's record.
func (m *Manifest) stateHeader() string {
	return fmt.Sprintf("GENERATED by %s: what each translation of this locale was made from.", m.generator())
}
