package translate

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"greendrake.nz/go/translate/format"
	"greendrake.nz/go/translate/internal/files"
)

// What each translation was made from, and therefore what makes it stale.
//
// The state is machine-owned and hashes the *source* side only — never the
// translation — which is what lets a person correct a rendered string and keep
// the correction: nothing re-renders until the thing it was rendered from
// moves. Two things are recorded, because two different things move: per
// unit, the text it was rendered from and the locale's glossary; per pass, the
// policy every unit of the pass was rendered under. A policy change is then a
// line per pass rather than a line per unit — and a change a host judges not
// to matter can be adopted without a render.

// promptVersion is bumped when the engine's own instructions change enough
// that existing translations should be produced again. It rides in every
// pass's policy digest, so a bump makes every locale stale — or, adopted,
// none.
const promptVersion = "3"

// State is one locale's record.
type State struct {
	// Policy maps each pass to the digest of the policy its units were
	// rendered under.
	Policy map[string]string `yaml:"policy"`
	// Units maps unit id to the digest of what its translation was made from.
	Units map[string]string `yaml:"units"`
	// Blocks records, per pass and per positional file, the unit ids of the
	// translated blocks in the order the file holds them — the only thing that
	// says which block is which.
	Blocks map[string]map[string][]string `yaml:"blocks,omitempty"`
}

// loadState reads a locale's record; a locale with none is simply all stale.
func (m *Manifest) loadState(locale string) (*State, error) {
	state := &State{Policy: map[string]string{}, Units: map[string]string{}, Blocks: map[string]map[string][]string{}}
	data, err := os.ReadFile(m.statePath(locale))
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read translation state %s: %w", locale, err)
	}
	if err := yaml.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("parse translation state %s: %w", locale, err)
	}
	// A section the file does not have reads back as nil.
	if state.Policy == nil {
		state.Policy = map[string]string{}
	}
	if state.Units == nil {
		state.Units = map[string]string{}
	}
	if state.Blocks == nil {
		state.Blocks = map[string]map[string][]string{}
	}
	return state, nil
}

// saveState writes the record as it stands: every unit the master defines that
// has both a translation and a digest. A record without its translation is
// dropped, so a translation deleted by hand reads as work; a translation
// without a record is one nothing has yet rendered against, and reads as work
// too.
//
// The record is written after every batch, so it holds stale entries
// truthfully: a unit whose recorded digest no longer matches its source keeps
// that record until it is rendered again, which is what lets a run that
// stopped partway be resumed rather than started over.
func (m *Manifest) saveState(locale string, master *Master, translations map[string]string, state *State) ([]string, error) {
	out := &State{Policy: map[string]string{}, Units: map[string]string{}, Blocks: map[string]map[string][]string{}}
	for _, pass := range m.passes() {
		if digest, ok := state.Policy[pass.ID]; ok {
			out.Policy[pass.ID] = digest
		}
	}
	type sequence struct{ pass, id string }
	incomplete := map[sequence]bool{}
	for _, unit := range master.Units {
		_, translated := translations[unit.ID]
		digest, recorded := state.Units[unit.ID]
		seq := sequence{unit.Pass, unit.Sequence}
		if !translated || !recorded {
			if unit.Sequence != "" {
				incomplete[seq] = true
			}
			continue
		}
		out.Units[unit.ID] = digest
		if unit.Sequence != "" {
			if out.Blocks[unit.Pass] == nil {
				out.Blocks[unit.Pass] = map[string][]string{}
			}
			out.Blocks[unit.Pass][unit.Sequence] = append(out.Blocks[unit.Pass][unit.Sequence], unit.ID)
		}
	}
	// A file's recorded order describes the file on disk. It is recomputed for
	// a file wholly translated — the only kind a store writes — and kept as it
	// was for one that is not, since the file it describes is still the old one.
	for seq := range incomplete {
		delete(out.Blocks[seq.pass], seq.id)
		if ids := state.Blocks[seq.pass][seq.id]; len(ids) > 0 {
			if out.Blocks[seq.pass] == nil {
				out.Blocks[seq.pass] = map[string][]string{}
			}
			out.Blocks[seq.pass][seq.id] = ids
		}
	}
	for pass, sequences := range out.Blocks {
		if len(sequences) == 0 {
			delete(out.Blocks, pass)
		}
	}
	return files.WriteYAML(files.Write, m.statePath(locale), m.stateHeader(), out)
}

// unitDigest is what a unit's translation was made from: its own text and the
// locale's glossary, which every prompt carries, so a change to it changes
// what any string would be rendered as. A vocabulary term is excluded from the
// glossary's own digest for the obvious reason, and carries its definition
// instead — the definition is its source.
func (m *Manifest) unitDigest(unit format.Unit, glossaryDigest string) string {
	if unit.Pass == m.Vocabulary.ID {
		return files.Digest(unit.Text, unit.Context)
	}
	return files.Digest(unit.Text, glossaryDigest)
}

// glossaryDigest digests one locale's glossary — the input every other unit's
// digest folds in.
func glossaryDigest(g Glossary) string {
	parts := []string{g.Register}
	for _, pair := range g.sortedTerms() {
		parts = append(parts, pair[0], pair[1])
	}
	return files.Digest(parts...)
}

// policyDigest is what every unit of a pass was rendered under beyond its own
// text and the glossary: the engine's instructions, the master's language, the
// brief, the host's rules, and the pass's own declarations that reach a prompt.
// What does not reach one — paths, batch sizes, tiers — stays out: moving a
// source to a cheaper model or tuning a batch must not cost a render.
func (m *Manifest) policyDigest(pass string) string {
	declarations, err := json.Marshal(m.promptDeclarations(pass))
	if err != nil {
		panic(fmt.Sprintf("translate: marshal declarations of %s: %v", pass, err))
	}
	return files.Digest(promptVersion, m.Default, m.policy.Brief,
		strings.Join(m.Prompt.KeepVerbatim, "\x00"), strings.Join(m.Prompt.Rules, "\x00"),
		string(declarations))
}

// promptDeclarations are a pass's declarations that reach a prompt: the
// guidance, and for a source, how its text is read and checked and what its
// units' contexts are expanded from.
func (m *Manifest) promptDeclarations(pass string) any {
	if pass == m.Vocabulary.ID {
		return struct{ Guidance string }{m.Vocabulary.Guidance}
	}
	for _, source := range m.Sources {
		if source.ID != pass {
			continue
		}
		spec := source.Spec
		return struct {
			Guidance        string
			Format          string
			Syntax          string
			SyntaxOverrides []format.SyntaxOverride
			Markup          string
			Shape           string
			Fields          format.Fields
			CopyBlocks      []string
			Context         string
			LabelContext    string
		}{source.Guidance, spec.Format, spec.Syntax, spec.SyntaxOverrides, spec.Markup, spec.Shape, spec.Fields, spec.CopyBlocks, spec.Context, spec.LabelContext}
	}
	panic("translate: no pass " + pass)
}

// staleUnits is the work a set of units owes.
//
// Four things make a unit stale, and the third is the one a state file alone
// would miss: no record, a record of a different source, a record with no
// translation behind it — a key deleted from the file by hand or lost in a
// merge — or a pass whose policy moved since. Without the third the run would
// report itself clean while the locale silently rendered the master's
// language.
func (m *Manifest) staleUnits(units []format.Unit, state *State, translations map[string]string, glossaryDigest string, policy map[string]string, retranslate []string) []format.Unit {
	var stale []format.Unit
	for _, unit := range units {
		_, translated := translations[unit.ID]
		if !translated || state.Policy[unit.Pass] != policy[unit.Pass] ||
			state.Units[unit.ID] != m.unitDigest(unit, glossaryDigest) || forced(unit.ID, retranslate) {
			stale = append(stale, unit)
		}
	}
	return stale
}

// removedIDs are the recorded ids the master no longer defines — a retired
// string, a deleted node, a block that was edited away.
func removedIDs(all []format.Unit, state *State) []string {
	defined := make(map[string]bool, len(all))
	for _, unit := range all {
		defined[unit.ID] = true
	}
	var removed []string
	for id := range state.Units {
		if !defined[id] {
			removed = append(removed, id)
		}
	}
	sort.Strings(removed)
	return removed
}

// forced reports whether an explicit --retranslate prefix names this unit.
func forced(id string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if prefix != "" && strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}
