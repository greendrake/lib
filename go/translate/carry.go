package translate

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"greendrake.nz/go/translate/format"
	"greendrake.nz/go/translate/internal/files"
)

// Carriage names the strings a carry hands over: every key of the target's
// master under one of Prefixes, from the same key in the source, and each key
// of Keys from the source key it names.
type Carriage struct {
	Prefixes []string
	// Keys maps a target key to the source key its translations come from.
	Keys map[string]string
}

// Carry hands translations from one catalogue source's keys to another's, so
// that rearranging where strings are kept costs no render: a library taking
// over what its host used to say itself, a key renamed, a new key saying
// exactly what an existing one already says. The masters are authored and
// change by hand first; Carry moves what only the engine writes. It carries
// each of the target's locales — a library manifest's being those of the
// project it was read for — and with every key of a pass named (the empty
// prefix), moves a pass whole: from one manifest's state to another's.
//
// A translation is carried only where it still holds: the source records it
// as rendered from the very text the target's master now holds, under the
// source's current policy and glossary. It arrives recorded as current in the
// target — unless the target owes a render of the whole pass anyway (a policy
// moved, a vocabulary not yet rendered), in which case it arrives as words
// kept until that render. A translation that does not hold is not carried:
// the target owes a render of it, which a check reports.
//
// The source is not touched: a key it still defines keeps its own, and one it
// no longer defines leaves its files and records on its next run or settle.
// Carried within one source — a key renamed — that source's files are written
// as a run writes them, which settles them: carry anything else out of it
// first.
func Carry(from *Manifest, source string, to *Manifest, target string, carriage Carriage) ([]string, error) {
	fromStore, err := from.catalogue(source)
	if err != nil {
		return nil, err
	}
	toStore, err := to.catalogue(target)
	if err != nil {
		return nil, err
	}
	pairs, err := carriage.pairs(target, toStore.Units())
	if err != nil {
		return nil, err
	}
	toMaster, err := to.Master()
	if err != nil {
		return nil, err
	}

	var written []string
	for _, locale := range to.targets() {
		holding, err := from.holding(fromStore, source, locale)
		if err != nil {
			return nil, err
		}
		state, err := to.loadState(locale)
		if err != nil {
			return nil, err
		}
		translations, err := to.existing(toMaster, locale, state)
		if err != nil {
			return nil, err
		}
		// What arrives can be recorded only against a pass the target renders
		// as it stands: one whose policy moved, or whose vocabulary is owed,
		// renders every unit again whatever it carries.
		policy := to.policyDigest(target)
		if _, recorded := state.Policy[target]; !recorded {
			state.Policy[target] = policy
		}
		vocabulary := to.vocabularyUnits()
		record := state.Policy[target] == policy && len(to.staleUnits(vocabulary, state, translations, "", to.policyDigests(), nil)) == 0
		glossary := glossaryDigest(to.glossary(locale, translations))

		carried := 0
		for _, unit := range pairs {
			text, holds := holding(unit.from, unit.Text)
			if !holds {
				continue
			}
			translations[unit.ID] = text
			if record {
				state.Units[unit.ID] = to.unitDigest(unit.Unit, glossary)
			}
			carried++
		}
		if carried == 0 {
			continue
		}
		put, err := toStore.Write(locale, translations, files.Write)
		if err != nil {
			return nil, err
		}
		recorded, err := to.saveState(locale, toMaster, translations, state)
		if err != nil {
			return nil, err
		}
		written = append(written, append(put, recorded...)...)
	}
	return written, nil
}

// carried is a target unit and the source key it is carried from.
type carried struct {
	format.Unit
	from string
}

// pairs resolves a carriage against the target's master.
func (c Carriage) pairs(target string, units []format.Unit) ([]carried, error) {
	byKey := make(map[string]format.Unit, len(units))
	for _, unit := range units {
		byKey[unit.Key] = unit
	}
	var out []carried
	for _, prefix := range c.Prefixes {
		n := 0
		for _, unit := range units {
			if strings.HasPrefix(unit.Key, prefix) {
				out = append(out, carried{Unit: unit, from: unit.Key})
				n++
			}
		}
		if n == 0 {
			return nil, fmt.Errorf("%s: the master holds nothing under %q — change the master first", target, prefix)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(c.Keys)) {
		unit, ok := byKey[key]
		if !ok {
			return nil, fmt.Errorf("%s: the master holds no %s — change the master first", target, key)
		}
		out = append(out, carried{Unit: unit, from: c.Keys[key]})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: nothing named to carry", target)
	}
	return out, nil
}

// holding answers, for one locale of a catalogue source, its translation of
// a key — and whether that translation holds for a given master text: the
// locale records it as rendered from that text, under the source's current
// policy and the glossary it now has. A locale whose vocabulary is owed holds
// nothing, since every string of it is about to be rendered again.
func (m *Manifest) holding(store format.Store, source, locale string) (func(key, text string) (string, bool), error) {
	state, err := m.loadState(locale)
	if err != nil {
		return nil, err
	}
	held, err := store.Existing(locale, nil)
	if err != nil {
		return nil, err
	}
	terms, err := m.existingGlossary(locale)
	if err != nil {
		return nil, err
	}
	policy := m.policyDigests()
	if state.Policy[source] != policy[source] || len(m.staleUnits(m.vocabularyUnits(), state, terms, "", policy, nil)) > 0 {
		return func(string, string) (string, bool) { return "", false }, nil
	}
	glossary := glossaryDigest(m.glossary(locale, terms))
	return func(key, text string) (string, bool) {
		id := format.UnitID(source, key)
		translation, ok := held[id]
		if !ok || state.Units[id] != m.unitDigest(format.Unit{Pass: source, Text: text}, glossary) {
			return "", false
		}
		return translation, true
	}, nil
}

// catalogue opens a yaml-catalogue source by id.
func (m *Manifest) catalogue(id string) (format.Store, error) {
	for _, source := range m.Sources {
		if source.ID != id {
			continue
		}
		if source.Format != format.Catalogue {
			return nil, fmt.Errorf("%s is %s, and only a catalogue's strings are carried", id, source.Format)
		}
		return m.open(source)
	}
	return nil, fmt.Errorf("%s declares no source %s", m.file, id)
}
