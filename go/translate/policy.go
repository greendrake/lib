package translate

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"greendrake.nz/go/translate/format"
	"greendrake.nz/go/translate/internal/files"
)

// Policy is the host's authored translation policy: the brief every prompt
// opens with, the register each locale addresses its reader in, and the terms
// whose rendering must not drift between batches.
type Policy struct {
	// Brief is read from the manifest's `policy.brief` file.
	Brief string `yaml:"-"`
	// Register is one English sentence per target locale saying how it
	// addresses the reader — du or Sie, ты or вы. Product policy, judged once
	// per language rather than asked of a model on every run: every prompt for
	// the locale carries it, a shipped locale without a line is refused, and it
	// rides in every string's digest, so a change re-renders the locale. Read
	// from the manifest's `policy.register` file.
	Register map[string]string `yaml:"-"`
	// Terms map each word the product uses about itself to what it means in
	// the product.
	Terms map[string]string `yaml:"terms"`
}

func (p Policy) validate(m *Manifest) error {
	if strings.TrimSpace(p.Brief) == "" {
		return fmt.Errorf("%s: the brief is what every prompt opens with and must not be empty", m.Policy.Brief)
	}
	if _, ok := p.Register[m.Default]; ok {
		return fmt.Errorf("%s: %s is the master, which is written rather than translated, and takes no register", m.Policy.Register, m.Default)
	}
	for _, locale := range m.targets() {
		if strings.TrimSpace(p.Register[locale]) == "" {
			return fmt.Errorf("%s: no register line for %s — every shipped locale states how it addresses the reader", m.Policy.Register, locale)
		}
	}
	return nil
}

// termOrder is the terms, sorted: a map has no order, and the rendered file,
// the prompts and the digests must be stable across runs.
func (p Policy) termOrder() []string {
	out := make([]string, 0, len(p.Terms))
	for term := range p.Terms {
		out = append(out, term)
	}
	sort.Strings(out)
	return out
}

// vocabularyUnits are the terms as the vocabulary pass renders them: one line
// each, with nothing in them the renderer reads.
func (m *Manifest) vocabularyUnits() []format.Unit {
	var units []format.Unit
	for _, term := range m.policy.termOrder() {
		units = append(units, format.Unit{
			ID:      format.UnitID(m.Vocabulary.ID, term),
			Pass:    m.Vocabulary.ID,
			Key:     term,
			Text:    term,
			Context: m.policy.Terms[term],
			Checks:  format.Checks{Syntax: format.SyntaxNone, Markup: format.MarkupNone, Shape: format.ShapeLine},
		})
	}
	return units
}

// Glossary is what every prompt for a locale carries beyond the brief: the
// register it addresses the reader in, and how each term is said. The terms
// are rendered once into the locale's glossary file, which is what stops one
// noun becoming three across fifty batches. The register is authored and is no
// part of that file — policy, not output — but is part of the digest, since
// every string was produced under it.
type Glossary struct {
	Register string            `yaml:"-"`
	Terms    map[string]string `yaml:"terms"`
}

// glossary assembles a locale's glossary from the translations in hand.
func (m *Manifest) glossary(locale string, translations map[string]string) Glossary {
	g := Glossary{Register: m.policy.Register[locale], Terms: map[string]string{}}
	for _, term := range m.policy.termOrder() {
		if rendering, ok := translations[m.Vocabulary.ID+":"+term]; ok {
			g.Terms[term] = rendering
		}
	}
	return g
}

// existingGlossary is the locale's rendered terms, keyed by unit id.
func (m *Manifest) existingGlossary(locale string) (map[string]string, error) {
	out := map[string]string{}
	data, err := os.ReadFile(m.glossaryPath(locale))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read glossary %s: %w", locale, err)
	}
	var g Glossary
	if err := yaml.Unmarshal(data, &g); err != nil {
		return nil, fmt.Errorf("parse glossary %s: %w", locale, err)
	}
	for term, rendering := range g.Terms {
		if _, defined := m.policy.Terms[term]; defined {
			out[m.Vocabulary.ID+":"+term] = rendering
		}
	}
	return out, nil
}

// writeGlossary emits a locale's rendered terms into a sink, once it has any.
func (m *Manifest) writeGlossary(locale string, translations map[string]string, sink files.Sink) ([]string, error) {
	g := m.glossary(locale, translations)
	if len(g.Terms) == 0 {
		return nil, nil
	}
	return files.WriteYAML(sink, m.glossaryPath(locale), m.glossaryHeader(), g)
}

// sortedTerms are a glossary's terms and renderings, in term order.
func (g Glossary) sortedTerms() [][2]string {
	terms := make([]string, 0, len(g.Terms))
	for term := range g.Terms {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	out := make([][2]string, len(terms))
	for i, term := range terms {
		out[i] = [2]string{term, g.Terms[term]}
	}
	return out
}
