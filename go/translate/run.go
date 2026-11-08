// Package translate renders a host's master — its default locale's
// translatable files — into every locale it ships, as the host's manifest
// declares them.
//
// The design in one line: every translatable thing is a *unit* with a stable
// id, a recorded digest of what it was translated from, and a translation
// living in the file its consumer already reads. A unit whose digest still
// matches is not sent anywhere, which is what makes a no-op run free and a
// one-string edit cost one request.
package translate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"greendrake.nz/go/llm"
	"greendrake.nz/go/translate/format"
	"greendrake.nz/go/translate/internal/files"
)

// Options configure one run.
type Options struct {
	// Only, when non-empty, restricts the run to these locales.
	Only []string
	// Retranslate forces every unit whose id carries one of these prefixes,
	// which is how a correction to the policy is pushed into strings already
	// rendered under it.
	Retranslate []string
	// Check reports what is stale and writes nothing — a build's gate.
	Check bool
	// Adopt records the current policy as what every locale's translations
	// were rendered under, and renders nothing: the host's statement that a
	// policy change does not touch the text on disk.
	Adopt bool
	// Settle writes every file and record as a run would from what the
	// locales hold — dropping what the master no longer defines, reordering
	// with it, writing the derived outputs — and renders nothing: what is owed
	// stays owed.
	Settle bool
	// Parallel bounds the requests in flight across the whole run — every
	// locale draws on the same slots; zero is the manifest's.
	Parallel int
	// Client renders the completions. Unused by Check, Adopt and Settle, which
	// is why a build with no provider configured still gates.
	Client llm.Client
	// Log receives the per-locale summary; nil discards it.
	Log func(string)
}

// Logger is where a run's lines go: Log, or nowhere.
func (o Options) Logger() func(string) {
	if o.Log == nil {
		return func(string) {}
	}
	return o.Log
}

// Result is what a run did, or — checking — would do.
type Result struct {
	// Stale counts the units each locale owes.
	Stale map[string]int
	// Policy names, per locale, the passes whose policy moved since its units
	// were rendered.
	Policy map[string][]string
	// Removed counts the recorded units the master no longer defines, and a
	// de-listed locale's files.
	Removed map[string]int
	// Files lists the files a check found not as a run would write them: a
	// derived output, or a locale's file whose master moved in a way no digest
	// sees — keys or paragraphs reordered.
	Files []string
	// Written lists every file the run changed.
	Written []string
}

// tidy sorts the files a run reports, each named once: a file every locale
// shares is every locale's to report.
func (r *Result) tidy() {
	for _, paths := range []*[]string{&r.Written, &r.Files} {
		slices.Sort(*paths)
		*paths = slices.Compact(*paths)
	}
}

// Clean reports whether the tree was already current.
func (r Result) Clean() bool {
	for _, counts := range []map[string]int{r.Stale, r.Removed} {
		for _, n := range counts {
			if n > 0 {
				return false
			}
		}
	}
	for _, passes := range r.Policy {
		if len(passes) > 0 {
			return false
		}
	}
	return len(r.Files) == 0 && len(r.Written) == 0
}

// ToRender counts the strings a run would ask the model for.
func (r Result) ToRender() int {
	n := 0
	for _, stale := range r.Stale {
		n += stale
	}
	return n
}

// Master is the host's translatable content: every unit of every pass, in the
// order they are rendered, and the stores that read and write them.
type Master struct {
	Units  []format.Unit
	stores []format.Store
}

// Master reads every source's master.
func (m *Manifest) Master() (*Master, error) {
	master := &Master{Units: m.vocabularyUnits()}
	for _, source := range m.Sources {
		store, err := m.open(source)
		if err != nil {
			return nil, err
		}
		master.stores = append(master.stores, store)
		master.Units = append(master.Units, store.Units()...)
	}
	return master, nil
}

// open reads one source's master.
func (m *Manifest) open(source Source) (format.Store, error) {
	return format.Open(source.ID, source.Spec, format.Env{Root: m.dir, Default: m.Default, Header: m.header()})
}

// existing is everything a locale already holds, keyed by unit id.
func (m *Manifest) existing(master *Master, locale string, state *State) (map[string]string, error) {
	out, err := m.existingGlossary(locale)
	if err != nil {
		return nil, err
	}
	for i, store := range master.stores {
		held, err := store.Existing(locale, state.Blocks[m.Sources[i].ID])
		if err != nil {
			return nil, err
		}
		for id, text := range held {
			out[id] = text
		}
	}
	return out, nil
}

// policyDigests are every pass's current policy digest.
func (m *Manifest) policyDigests() map[string]string {
	out := map[string]string{}
	for _, pass := range m.passes() {
		out[pass.ID] = m.policyDigest(pass.ID)
	}
	return out
}

// moved are the passes whose recorded policy is not the current one. A pass
// the master gives no units — a vocabulary of no terms — has rendered nothing
// under any policy, and owes nothing.
func (m *Manifest) moved(master *Master, state *State, policy map[string]string) []string {
	populated := map[string]bool{}
	for _, unit := range master.Units {
		populated[unit.Pass] = true
	}
	var out []string
	for _, pass := range m.passes() {
		if populated[pass.ID] && state.Policy[pass.ID] != policy[pass.ID] {
			out = append(out, pass.ID)
		}
	}
	return out
}

// Run produces every shipped locale — or checks them, or adopts their policy.
func Run(ctx context.Context, m *Manifest, opts Options) (Result, error) {
	result := Result{Stale: map[string]int{}, Policy: map[string][]string{}, Removed: map[string]int{}}
	modes := 0
	for _, on := range []bool{opts.Check, opts.Adopt, opts.Settle} {
		if on {
			modes++
		}
	}
	if modes > 1 {
		return result, errors.New("--check, --adopt and --settle are different questions; ask one")
	}
	if opts.Parallel == 0 {
		opts.Parallel = m.LLM.Parallel
	}
	if opts.Parallel < 0 {
		return result, fmt.Errorf("%d requests in flight: the count cannot be negative", opts.Parallel)
	}
	opts.Log = opts.Logger()
	master, err := m.Master()
	if err != nil {
		return result, err
	}
	policy := m.policyDigests()

	targets := m.targets()
	if len(opts.Only) > 0 {
		for _, locale := range opts.Only {
			// Producing the master would overwrite the authored source with a
			// translation of itself. A full run simply skips it; naming it is a
			// mistake worth saying out loud.
			if locale == m.Default {
				return result, fmt.Errorf("%s is the master, not a target", locale)
			}
			if !slices.Contains(targets, locale) {
				return result, fmt.Errorf("%s is not shipped", locale)
			}
		}
		targets = opts.Only
	}
	// Every locale renders at once, drawing on one set of request slots. A
	// locale's vocabulary goes alone and its last batches finish on their own;
	// rendered one locale after another, the model would sit on one request
	// through both, where here other locales' requests take the slots they
	// leave.
	slots := make(chan struct{}, opts.Parallel)
	var mu sync.Mutex
	var failed []error
	group, groupCtx := errgroup.WithContext(ctx)
	for _, locale := range targets {
		group.Go(func() error {
			r := &localeRun{m: m, opts: opts, master: master, policy: policy, locale: locale, slots: slots}
			var outcome localeOutcome
			var err error
			switch {
			case opts.Adopt:
				outcome, err = r.adopt()
			case opts.Settle:
				outcome, err = r.settle()
			default:
				outcome, err = r.run(groupCtx)
			}
			if err != nil {
				return fmt.Errorf("%s: %w", locale, err)
			}
			mu.Lock()
			defer mu.Unlock()
			result.Stale[locale] = outcome.stale
			result.Policy[locale] = outcome.moved
			result.Removed[locale] = outcome.removed
			result.Written = append(result.Written, outcome.written...)
			result.Files = append(result.Files, outcome.files...)
			failed = append(failed, outcome.failed...)
			if outcome.stale > 0 || outcome.removed > 0 || len(outcome.moved) > 0 || len(outcome.written) > 0 || len(outcome.files) > 0 {
				opts.Log(outcome.summary(locale, opts))
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return result, err
	}
	result.tidy()
	// What could not be rendered — a string no rendering of which passed, the
	// strings of a request that failed — cost only itself: everything else is
	// written, and the next run owes exactly what failed. The run still fails —
	// the tree is not current — and says what to look at.
	if len(failed) > 0 {
		reports := make([]string, len(failed))
		for i, err := range failed {
			reports[i] = err.Error()
		}
		sort.Strings(reports)
		return result, fmt.Errorf("%d failure(s); everything else is written, and the same command renders what failed:\n- %s",
			len(failed), strings.Join(reports, "\n- "))
	}

	// Derived outputs and de-listed locales are properties of the shipped set
	// as a whole, so they are settled once rather than per locale — and only on
	// a full run, since a --locale run has not looked at the others.
	if len(opts.Only) > 0 {
		return result, nil
	}
	sink, into := files.Sink(files.Write), &result.Written
	if opts.Check {
		sink, into = files.Compare, &result.Files
	}
	derived, err := m.writeDerived(sink)
	if err != nil {
		return result, err
	}
	*into = append(*into, derived...)
	// A library's record holds the locales of every project serving it, so
	// one outside this project's set is another project's: which of them no
	// project ships any more is Prune's to say.
	var stray []string
	if !m.library {
		if stray, err = m.recordedOutside(m.Shipped); err != nil {
			return result, err
		}
	}
	for _, locale := range stray {
		result.Removed[locale]++
		if opts.Check || opts.Adopt {
			continue
		}
		removed, err := m.removeLocale(master, locale)
		if err != nil {
			return result, err
		}
		result.Written = append(result.Written, removed...)
		opts.Log(fmt.Sprintf("%s: de-listed, %d file(s) removed", locale, len(removed)))
	}
	result.tidy()
	if !opts.Check && len(result.Written) > 0 {
		if err := m.verify(opts.Log); err != nil {
			return result, err
		}
	}
	return result, nil
}

// localeOutcome is what one locale's run did.
type localeOutcome struct {
	stale, removed int
	moved          []string
	written        []string
	// files are the locale's files a check found not as a run would write them.
	files []string
	// rendered counts the units a run produced; failed are what it could not —
	// a string, a row, the strings of a request — which stay owed.
	rendered int
	failed   []error
}

func (o localeOutcome) summary(locale string, opts Options) string {
	var moved string
	if len(o.moved) > 0 {
		moved = fmt.Sprintf(" (policy moved: %s)", strings.Join(o.moved, ", "))
	}
	switch {
	case opts.Check:
		return fmt.Sprintf("%s: %d string(s) out of date%s, %d the master no longer defines, %d file(s) not as a run would write them", locale, o.stale, moved, o.removed, len(o.files))
	case opts.Adopt:
		return fmt.Sprintf("%s: policy adopted%s, %d string(s) still out of date on their source", locale, moved, o.stale)
	case opts.Settle:
		return fmt.Sprintf("%s: settled, %d removed, %d file(s) written, %d string(s) still out of date", locale, o.removed, len(o.written), o.stale)
	}
	var failed string
	if len(o.failed) > 0 {
		failed = fmt.Sprintf(", %d failed", len(o.failed))
	}
	return fmt.Sprintf("%s: %d translated%s%s, %d removed, %d file(s) written", locale, o.rendered, moved, failed, o.removed, len(o.written))
}

// localeRun is one locale's run in progress: what it holds, and what it has
// written so far.
//
// The files and the record are written after every batch, with what it
// rendered. A locale is a hundred requests, and a failure at the seventieth
// must not discard the sixty-nine before it — nor the thirty after it, nor the
// rest of its own batch: what fails is left owed and the rest of the locale
// goes on. What is left is a partial
// locale — a legitimate state, which consumers merging the master under the
// locale render, and a check reports as stale — and the next run continues
// from the record. The completeness gate still decides whether a run
// succeeded.
type localeRun struct {
	m      *Manifest
	opts   Options
	master *Master
	policy map[string]string
	locale string
	// slots are the run's requests in flight, shared by every locale.
	slots chan struct{}
	// source and target are how the master's locale and this one write — what
	// a plural's forms are asked for against, and the letters held.
	source, target writing
	state          *State
	translations   map[string]string
	written        []string
}

// load reads what the locale holds and what it was rendered from.
func (r *localeRun) load() error {
	var err error
	if r.state, err = r.m.loadState(r.locale); err != nil {
		return err
	}
	if r.translations, err = r.m.existing(r.master, r.locale, r.state); err != nil {
		return err
	}
	if r.source, err = writingOf(r.m.Default); err != nil {
		return err
	}
	r.target, err = writingOf(r.locale)
	return err
}

// split is the master's vocabulary and everything else.
func (r *localeRun) split() (vocabulary, other []format.Unit) {
	for _, unit := range r.master.Units {
		if unit.Pass == r.m.Vocabulary.ID {
			vocabulary = append(vocabulary, unit)
			continue
		}
		other = append(other, unit)
	}
	return vocabulary, other
}

// stale is the work each half owes: the vocabulary on its own terms, the rest
// against the glossary as the locale now renders it.
func (r *localeRun) stale(vocabulary, other []format.Unit) (staleVocabulary, staleOther []format.Unit) {
	staleVocabulary = r.m.staleUnits(vocabulary, r.state, r.translations, "", r.policy, r.opts.Retranslate)
	staleOther = r.m.staleUnits(other, r.state, r.translations, glossaryDigest(r.m.glossary(r.locale, r.translations)), r.policy, r.opts.Retranslate)
	return staleVocabulary, staleOther
}

// run produces the locale, or — checking — counts what it owes.
func (r *localeRun) run(ctx context.Context) (localeOutcome, error) {
	if err := r.load(); err != nil {
		return localeOutcome{}, err
	}
	outcome := localeOutcome{
		moved:   r.m.moved(r.master, r.state, r.policy),
		removed: len(removedIDs(r.master.Units, r.state)),
	}
	vocabulary, other := r.split()

	if r.opts.Check {
		// A digest sees what a unit says, not where the master puts it: keys
		// or paragraphs reordered move no digest and still move every file. So
		// the files are compared with what a run would write from what they
		// already hold.
		var err error
		if outcome.files, err = r.write(files.Compare); err != nil {
			return localeOutcome{}, err
		}
		staleVocabulary, staleOther := r.stale(vocabulary, other)
		if len(staleVocabulary) > 0 {
			// The glossary is about to change, so every other unit will be
			// stale too whatever its recorded digest says.
			outcome.stale = len(r.master.Units)
			return outcome, nil
		}
		outcome.stale = len(staleOther)
		return outcome, nil
	}

	// A pass whose policy moved is rendered whole under the new one. The
	// policy is recorded now and the pass's unit records dropped, so a run
	// that stops partway resumes owing exactly what it had not rendered yet.
	for _, pass := range outcome.moved {
		r.state.Policy[pass] = r.policy[pass]
		for _, unit := range r.master.Units {
			if unit.Pass == pass {
				delete(r.state.Units, unit.ID)
			}
		}
	}

	// The vocabulary is settled first and alone: every other unit's digest
	// folds in the rendering it produced, so translating anything against a
	// glossary that is about to change would record a digest that is wrong the
	// moment it is written.
	staleVocabulary, _ := r.stale(vocabulary, other)
	outcome.stale += len(staleVocabulary)
	var err error
	if outcome.rendered, outcome.failed, err = r.translateBatches(ctx, Glossary{Register: r.m.policy.Register[r.locale]}, staleVocabulary); err != nil {
		return localeOutcome{}, err
	}
	// Everything else is rendered against the glossary the vocabulary
	// settles, so with any of it owed there is nothing more to ask.
	if len(outcome.failed) == 0 {
		_, staleOther := r.stale(vocabulary, other)
		outcome.stale += len(staleOther)
		rendered, failed, err := r.translateBatches(ctx, r.m.glossary(r.locale, r.translations), staleOther)
		if err != nil {
			return localeOutcome{}, err
		}
		outcome.rendered += rendered
		outcome.failed = failed
	}
	// A locale owing what failed is incomplete by construction, and the
	// failure already names what it owes.
	if len(outcome.failed) == 0 {
		if err := r.complete(); err != nil {
			return localeOutcome{}, err
		}
	}
	// The last batch wrote everything it had. This writes what no batch
	// carried — a removed id leaving a file, a copied block — and is a no-op
	// otherwise.
	if err := r.persist(); err != nil {
		return localeOutcome{}, err
	}
	outcome.written = r.written
	return outcome, nil
}

// adopt records the current policy as the one the locale's translations were
// rendered under. A locale with no record has nothing on disk to adopt. A
// unit whose own source moved is left as it was — genuinely stale, and owed a
// render.
func (r *localeRun) adopt() (localeOutcome, error) {
	if _, err := os.Stat(r.m.statePath(r.locale)); os.IsNotExist(err) {
		return localeOutcome{}, nil
	}
	if err := r.load(); err != nil {
		return localeOutcome{}, err
	}
	outcome := localeOutcome{
		moved:   r.m.moved(r.master, r.state, r.policy),
		removed: len(removedIDs(r.master.Units, r.state)),
	}
	for pass, digest := range r.policy {
		r.state.Policy[pass] = digest
	}
	staleVocabulary, staleOther := r.stale(r.split())
	outcome.stale = len(staleVocabulary) + len(staleOther)
	written, err := r.m.saveState(r.locale, r.master, r.translations, r.state)
	outcome.written = written
	return outcome, err
}

// settle writes the locale's files and record from what it holds, rendering
// nothing. The policy record is left as it is: a pass whose policy moved is
// still owed its render.
func (r *localeRun) settle() (localeOutcome, error) {
	if err := r.load(); err != nil {
		return localeOutcome{}, err
	}
	outcome := localeOutcome{
		moved:   r.m.moved(r.master, r.state, r.policy),
		removed: len(removedIDs(r.master.Units, r.state)),
	}
	staleVocabulary, staleOther := r.stale(r.split())
	outcome.stale = len(staleVocabulary) + len(staleOther)
	if err := r.persist(); err != nil {
		return localeOutcome{}, err
	}
	outcome.written = r.written
	return outcome, nil
}

// complete is the run's last word on a locale: every unit the master defines
// has a translation. It is the one place completeness is policed — consumers
// merging the master under a locale would render the master's language
// instead of saying anything.
func (r *localeRun) complete() error {
	var missing []string
	for _, unit := range r.master.Units {
		if _, ok := r.translations[unit.ID]; !ok {
			missing = append(missing, unit.ID)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	if len(missing) > 10 {
		return fmt.Errorf("%d units untranslated, first ten: %s", len(missing), strings.Join(missing[:10], ", "))
	}
	return fmt.Errorf("%d units untranslated: %s", len(missing), strings.Join(missing, ", "))
}

// write emits every file of the locale — its glossary and each store's — from
// the translations in hand into a sink.
func (r *localeRun) write(sink files.Sink) ([]string, error) {
	out, err := r.m.writeGlossary(r.locale, r.translations, sink)
	if err != nil {
		return nil, err
	}
	for _, store := range r.master.stores {
		put, err := store.Write(r.locale, r.translations, sink)
		if err != nil {
			return nil, err
		}
		out = append(out, put...)
	}
	return out, nil
}

// persist writes every file and the record from the translations in hand,
// noting what changed.
func (r *localeRun) persist() error {
	written, err := r.write(files.Write)
	if err != nil {
		return err
	}
	record, err := r.m.saveState(r.locale, r.master, r.translations, r.state)
	if err != nil {
		return err
	}
	// A catalogue changes on every one of its batches and is one file however
	// many times it was written.
	for _, path := range append(written, record...) {
		if !slices.Contains(r.written, path) {
			r.written = append(r.written, path)
		}
	}
	return nil
}

// translateBatches groups the stale units, renders each group and writes the
// locale after each one, returning how many units it produced and what it
// could not. Copies never leave the process.
//
// A string no rendering of which passes costs itself, with the rest of its
// row; a request that outran its timeout or that the server refused costs its
// own strings. What no other request would escape stops the run: the run
// cancelled, the endpoint unreachable, a file that cannot be written.
func (r *localeRun) translateBatches(ctx context.Context, glossary Glossary, stale []format.Unit) (int, []error, error) {
	digest := glossaryDigest(glossary)
	var pending []format.Unit
	for _, unit := range stale {
		if unit.Copy {
			r.translations[unit.ID] = unit.Text
			r.state.Units[unit.ID] = r.m.unitDigest(unit, digest)
			continue
		}
		pending = append(pending, unit)
	}
	rendered := len(stale) - len(pending)
	if len(pending) == 0 {
		return rendered, nil, nil
	}
	if r.opts.Client == nil {
		return 0, nil, fmt.Errorf("%d unit(s) need translating and no provider is configured — set %sPROVIDER", len(pending), r.m.LLM.EnvPrefix)
	}

	var mu sync.Mutex
	var failed []error
	group, groupCtx := errgroup.WithContext(ctx)
	for _, b := range r.m.batches(pending) {
		group.Go(func() error {
			// A batch not yet started when the run stops is not started: what
			// it would have rendered is still owed, and the record says so.
			if err := groupCtx.Err(); err != nil {
				return err
			}
			texts, failures, err := r.renderBatch(groupCtx, glossary, b)
			if err != nil {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			// What failed stays owed, with the rest of its bundle: a row is
			// written only whole.
			owed, owedBundles := map[string]bool{}, map[string]bool{}
			for _, f := range failures {
				failed = append(failed, f.err)
				for _, unit := range f.units {
					owed[unit.ID] = true
					if unit.Bundle != "" {
						owedBundles[unit.Bundle] = true
					}
				}
			}
			for _, unit := range b.units {
				if owed[unit.ID] || owedBundles[unit.Bundle] {
					continue
				}
				r.translations[unit.ID] = texts[unit.ID]
				r.state.Units[unit.ID] = r.m.unitDigest(unit, digest)
				rendered++
			}
			return r.persist()
		})
	}
	return rendered, failed, group.Wait()
}

// unreachable reports an error no other request would escape: the run
// cancelled, or the endpoint not answering at all — a connection refused or
// dropped rather than a request that ran out of time.
func unreachable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && !netErr.Timeout()
}

// batch is one request's units: one pass, one group.
type batch struct {
	pass  Pass
	group string
	units []format.Unit
}

// batches groups units into requests: by pass, then by what the pass declares
// a batch never spans, then to its size — never splitting a bundle.
func (m *Manifest) batches(units []format.Unit) []batch {
	type key struct{ pass, group string }
	var order []key
	groups := map[key][]format.Unit{}
	for _, unit := range units {
		k := key{unit.Pass, groupOf(m.pass(unit.Pass).Batch.By, unit)}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], unit)
	}

	var out []batch
	for _, k := range order {
		pass := m.pass(k.pass)
		members := groups[k]
		var current []format.Unit
		flush := func() {
			if len(current) > 0 {
				out = append(out, batch{pass: pass, group: k.group, units: current})
				current = nil
			}
		}
		for start := 0; start < len(members); {
			end := start + 1
			for members[start].Bundle != "" && end < len(members) && members[end].Bundle == members[start].Bundle {
				end++
			}
			if pass.Batch.Size > 0 && len(current) > 0 && len(current)+end-start > pass.Batch.Size {
				flush()
			}
			current = append(current, members[start:end]...)
			start = end
		}
		flush()
	}
	return out
}

// groupOf is what a unit shares a request with.
func groupOf(by string, unit format.Unit) string {
	switch by {
	case ByFile:
		return unit.File
	case ByFirstSegment:
		segment, _, _ := strings.Cut(unit.Key, ".")
		return segment
	}
	return ""
}

// retryRenderings is how many renderings a retry asks for of each rejected
// item.
const retryRenderings = 3

// failure is units left owed, and why: a unit no rendering of which passed,
// or the units of a request that failed.
type failure struct {
	units []format.Unit
	err   error
}

// renderBatch asks for one batch and validates every unit, escalating a
// rejected unit once to the strong tier with the complaint attached. A unit no
// rendering of which passes fails alone — the alternative is writing a string
// nothing checked, and the batch's other units are as good as they were — and
// so does each unit of a request that outran its timeout or that the endpoint
// refused. An answer cut off at the cap keeps every item it finished: the one
// it was writing is rejected as a string that never ended, and those it never
// reached are asked for again as a batch of their own. The error is one no
// other request would escape: the run stops.
func (r *localeRun) renderBatch(ctx context.Context, glossary Glossary, b batch) (map[string]string, []failure, error) {
	out := map[string]string{}
	rendered, system, err := r.ask(ctx, glossary, b, b.units, 1, "", tierOf(b.pass.Tier))
	if err != nil {
		if unreachable(ctx, err) {
			return nil, nil, err
		}
		return nil, []failure{{units: b.units, err: err}}, nil
	}
	var retry, unreached []format.Unit
	var complaints []string
	// complain rejects unit for reason, quoting what it was answered with.
	complain := func(unit format.Unit, reason, wrote string) {
		retry = append(retry, unit)
		complaint := fmt.Sprintf("%s: %s", unit.ID, reason)
		if wrote != "" {
			complaint += "\n  You wrote: " + wrote
		}
		complaints = append(complaints, complaint)
	}
	for _, unit := range b.units {
		items, err := wireItems(unit, r.source, r.target)
		if err != nil {
			return nil, nil, err
		}
		switch {
		case rendered.cut != nil && slices.ContainsFunc(items, func(item wireItem) bool { return item.ID == rendered.cut.id }):
			complain(unit, "your answer was cut off at the length limit while you were writing it: it never ended", rendered.cut.written)
		case rendered.truncated && !slices.ContainsFunc(items, func(item wireItem) bool { return len(rendered.texts[item.ID]) > 0 }):
			unreached = append(unreached, unit)
		default:
			text, err := r.accept(unit, rendered)
			if err != nil {
				complain(unit, err.Error(), r.answerFor(unit, rendered))
				continue
			}
			out[unit.ID] = text
		}
	}
	var failures []failure
	if len(unreached) > 0 {
		texts, more, err := r.renderBatch(ctx, glossary, batch{pass: b.pass, group: b.group, units: unreached})
		if err != nil {
			return nil, nil, err
		}
		maps.Copy(out, texts)
		failures = append(failures, more...)
	}
	if len(retry) == 0 {
		return out, failures, nil
	}

	// The complaints ride in the system prompt: the user prompt is the data,
	// and a note about a previous answer is an instruction. Each quotes what
	// was written, and each item is asked for several times over, the first
	// rendering that passes kept: shown only a complaint, a model renders the
	// item afresh, and one that cannot count characters writes the same
	// too-long phrase again however it is told — asked for renderings each
	// shorter than the one before, it writes one that fits, and the engine is
	// what counts.
	note := fmt.Sprintf("\nYour previous answer was rejected for these items:\n- %[1]s\nAnswer each of them %[2]d times, as <id>%[3]s1, <id>%[3]s2 and so on to <id>%[3]s%[2]d — a plural's form as <id>@<category>%[3]s1 and so on. The first is what you wrote with exactly what its line names fixed, and nothing else changed; each after it says the same another way, and for an item over its limit, shorter than the one before. The first of them that passes every check is kept.\n",
		strings.Join(complaints, "\n- "), retryRenderings, renderingSeparator)
	rendered, _, err = r.ask(ctx, glossary, b, retry, retryRenderings, system+note, llm.TierStrong)
	if err != nil {
		if unreachable(ctx, err) {
			return nil, nil, err
		}
		return out, append(failures, failure{units: retry, err: err}), nil
	}
retried:
	for _, unit := range retry {
		var rejections []string
		for k := 1; k <= retryRenderings; k++ {
			rendering := rendered.rendering(k)
			text, err := r.accept(unit, rendering)
			if err == nil {
				out[unit.ID] = text
				continue retried
			}
			rejections = append(rejections, fmt.Sprintf("%s: %s\n      %v", renderingSuffix(k), r.answerFor(unit, rendering), err))
		}
		failures = append(failures, failure{
			units: []format.Unit{unit},
			err:   fmt.Errorf("%s %s: none of %d renderings passed\n  source: %s\n  %s", r.locale, unit.ID, retryRenderings, unit.Text, strings.Join(rejections, "\n  ")),
		})
	}
	return out, failures, nil
}

func tierOf(tier string) llm.Tier {
	if tier == TierStrong {
		return llm.TierStrong
	}
	return llm.TierFast
}

// accept turns what came back for a unit into its translation, or says what is
// wrong with it. Both happen in the wire's terms — a plural assembled from its
// forms, references and markup attributes still placeholders — so that a
// complaint names what the model was shown. A limit alone is measured on the
// text as it will be written, the renderer's syntax restored: that is what the
// field it is bound for receives.
func (r *localeRun) accept(unit format.Unit, rendered answer) (string, error) {
	w, err := wire(unit)
	if err != nil {
		return "", err
	}
	text, err := assemble(w.unit, rendered, r.target)
	if err != nil {
		return "", err
	}
	written, err := w.restore(text)
	if err != nil {
		return "", err
	}
	if err := joined(validate(w, text, r.target), withinLimit(unit, written)); err != nil {
		return "", err
	}
	return written, nil
}

// answerFor quotes what the model wrote for a unit, for its retry and the
// message a stopped run ends with: the item carrying its id, then each form
// asked for.
func (r *localeRun) answerFor(unit format.Unit, rendered answer) string {
	parts := append([]string(nil), rendered.written[unit.ID]...)
	for _, category := range r.target.categories {
		for _, text := range rendered.written[unit.ID+formSeparator+category] {
			parts = append(parts, formSeparator+category+": "+text)
		}
	}
	return strings.Join(parts, ", ")
}

// ask renders one completion for some of a batch's units, each asked for as
// that many renderings, and unpacks it, keyed by the ids asked for. The system
// prompt is built from the units asked for, unless one is given, and returned
// for a retry to extend.
func (r *localeRun) ask(ctx context.Context, glossary Glossary, b batch, units []format.Unit, renderings int, system string, tier llm.Tier) (answer, string, error) {
	var items []wireItem
	for _, unit := range units {
		wired, err := wireItems(unit, r.source, r.target)
		if err != nil {
			return answer{}, "", err
		}
		items = append(items, numbered(wired, renderings)...)
	}
	if system == "" {
		system = r.m.systemPrompt(batchPrompt{
			target: r.locale, glossary: glossary,
			categories: r.target.categories, pass: b.pass, units: units, items: items,
		})
	}
	user, err := userPrompt(items)
	if err != nil {
		return answer{}, "", err
	}
	schema, err := responseSchema(items)
	if err != nil {
		return answer{}, "", err
	}
	// One of the run's slots for the length of the request.
	select {
	case r.slots <- struct{}{}:
	case <-ctx.Done():
		return answer{}, "", ctx.Err()
	}
	out, err := r.opts.Client.Complete(ctx, llm.Request{
		System:        system,
		User:          user,
		Schema:        schema,
		Tier:          tier,
		Label:         fmt.Sprintf("translate %s %s/%s", r.locale, b.pass.ID, b.group),
		MaxTokens:     maxTokensFor(items),
		KeepTruncated: true,
	})
	<-r.slots
	// An answer cut off at the cap — a model looping inside one string, as a
	// rule — keeps every item it finished.
	var truncated *llm.TruncatedError
	if errors.As(err, &truncated) {
		rendered, kept, err := cutAnswerOf(truncated.Completion.JSON)
		if err != nil {
			return answer{}, "", fmt.Errorf("translate %s: completion is not the agreed shape: %w", r.locale, err)
		}
		if !kept {
			return answer{}, "", fmt.Errorf("translate %s (%d %s units): %w, with no item in it whole", r.locale, len(units), b.pass.ID, truncated)
		}
		return rendered, system, nil
	}
	if err != nil {
		return answer{}, "", fmt.Errorf("translate %s (%d %s units): %w", r.locale, len(units), b.pass.ID, err)
	}
	var parsed completion
	if err := json.Unmarshal(out.JSON, &parsed); err != nil {
		return answer{}, "", fmt.Errorf("translate %s: completion is not the agreed shape: %w", r.locale, err)
	}
	rendered, err := answerOf(parsed.Items)
	if err != nil {
		return answer{}, "", fmt.Errorf("translate %s: completion is not the agreed shape: %w", r.locale, err)
	}
	return rendered, system, nil
}

// recordedOutside are the locales with a record that set does not name. The
// state directory is the register: every locale the translator has ever
// produced has a file there.
func (m *Manifest) recordedOutside(set []string) ([]string, error) {
	entries, err := filepath.Glob(m.path(filepath.Join(m.State, "*.yaml")))
	if err != nil {
		return nil, err
	}
	var stray []string
	for _, path := range entries {
		locale := strings.TrimSuffix(filepath.Base(path), ".yaml")
		if !slices.Contains(set, locale) {
			stray = append(stray, locale)
		}
	}
	sort.Strings(stray)
	return stray, nil
}

// Prune removes, for the library manifest at path, every file of each locale
// its record holds that none of projects ships — projects being every project
// serving the library, each running it in its own locales and none able to
// tell another's locale from one nobody ships any more. They share one master
// locale, the library's. Reports what it removed.
func Prune(path string, projects []*Manifest) ([]string, error) {
	if len(projects) == 0 {
		return nil, errors.New("prune keeps the locales of the projects serving the library: name them")
	}
	// Read on each project's behalf, which refuses one whose master is not the
	// library's.
	var m *Manifest
	var keep []string
	for _, project := range projects {
		loaded, err := Load(path, project)
		if err != nil {
			return nil, err
		}
		if !loaded.library {
			return nil, fmt.Errorf("%s ships its own locales, and its run removes what it no longer ships", path)
		}
		m = loaded
		keep = append(keep, project.Shipped...)
	}
	master, err := m.Master()
	if err != nil {
		return nil, err
	}
	stray, err := m.recordedOutside(keep)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, locale := range stray {
		paths, err := m.removeLocale(master, locale)
		if err != nil {
			return nil, err
		}
		removed = append(removed, paths...)
	}
	return removed, nil
}

// removeLocale removes everything a de-listed locale has. The shipped set is
// the only declaration, so dropping a tag from it has to be as complete as
// adding one — otherwise a served catalogue outlives the locale that
// justified it. The record goes last: it is what names the locale as one to
// remove, so a removal that stops partway is finished by the next run.
func (m *Manifest) removeLocale(master *Master, locale string) ([]string, error) {
	var removed []string
	for _, store := range master.stores {
		dropped, err := store.Drop(locale)
		if err != nil {
			return nil, err
		}
		removed = append(removed, dropped...)
	}
	own, err := files.Remove([]string{m.glossaryPath(locale), m.statePath(locale)})
	return append(removed, own...), err
}
