// Package locales is a product's shipped locale set: every locale it ships,
// the authored default among them, and the one answer to which of them a
// requested tag gets.
package locales

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"greendrake.nz/go/i18n/negotiate"
)

// Set is the shipped locales and their default, with the negotiator over
// them.
type Set struct {
	Default string
	Shipped []string

	negotiator *negotiate.Negotiator
}

// New builds a set. The tags must be canonical BCP-47 and include the
// default.
func New(shipped []string, def string) (*Set, error) {
	n, err := negotiate.New(shipped, def)
	if err != nil {
		return nil, err
	}
	return &Set{Default: def, Shipped: shipped, negotiator: n}, nil
}

// Load reads a set from the top-level `default` and `shipped` keys of a YAML
// file, whatever else it declares: the set is read from the file that already
// declares it.
func Load(path string) (*Set, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("locales: %w", err)
	}
	var decl struct {
		Default string   `yaml:"default"`
		Shipped []string `yaml:"shipped"`
	}
	if err := yaml.Unmarshal(data, &decl); err != nil {
		return nil, fmt.Errorf("locales: parse %s: %w", path, err)
	}
	set, err := New(decl.Shipped, decl.Default)
	if err != nil {
		return nil, fmt.Errorf("locales: %s: %w", path, err)
	}
	return set, nil
}

// Resolve maps the tag a client asked for onto a shipped locale — real
// BCP-47 matching, so `zh-TW` reaches `zh-Hant`, `es-MX` reaches `es-419` and
// `no` reaches `nb`. A tag nothing shipped speaks, an empty one and one that
// is not a tag at all resolve to the default: a caller with no usable
// preference is not a fault. It is a plain func(string) string, for a request
// layer to call.
func (s *Set) Resolve(requested string) string {
	return s.negotiator.Match(requested)
}
