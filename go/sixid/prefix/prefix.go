// Package prefix renders SixIDs in their external form. The mechanism lives
// here; the policy — which entities a host exposes and whether they carry a
// letter — is the host's, the host being the program that exposes the ids,
// which hands its Codec to every library that renders them. A library names
// the entities whose ids it renders and documents those names; the host's
// Table must cover them.
package prefix

import (
	"fmt"
	"slices"

	"greendrake.nz/go/sixid"
)

// Entity names one kind of id.
type Entity string

// Codec encodes and decodes the external form of an entity's ids.
type Codec interface {
	Encode(e Entity, id sixid.SixID) string
	Decode(s string, e Entity) (sixid.SixID, error)
}

// Plain is the Codec of a host without a prefix convention: every id
// travels as the bare sixid string, whatever its entity.
var Plain Codec = plain{}

type plain struct{}

func (plain) Encode(_ Entity, id sixid.SixID) string { return id.String() }

func (plain) Decode(s string, _ Entity) (sixid.SixID, error) { return sixid.ParseSixID(s) }

// Table maps each entity a host exposes to its one-letter prefix.
type Table map[Entity]byte

// Prefixed is the Codec over a Table: an id's external form is its entity's
// letter followed by the bare sixid string — 11 characters.
type Prefixed struct {
	letters  Table
	entities map[byte]Entity
}

var _ Codec = (*Prefixed)(nil)

// New returns the codec of t. Two entities sharing a letter is a
// programming error in the host's table, so New panics on it.
func New(t Table) *Prefixed {
	entities := make(map[byte]Entity, len(t))
	for e, letter := range t {
		if other, taken := entities[letter]; taken {
			panic(fmt.Sprintf("prefix: %q and %q share the letter %q", e, other, string(letter)))
		}
		entities[letter] = e
	}
	return &Prefixed{letters: t, entities: entities}
}

// Letter returns e's prefix. An entity outside the table is a programming
// error, so it panics.
func (p *Prefixed) Letter(e Entity) byte {
	letter, ok := p.letters[e]
	if !ok {
		panic(fmt.Sprintf("prefix: entity %q is not in the table", e))
	}
	return letter
}

// Encode returns the prefixed form of id.
func (p *Prefixed) Encode(e Entity, id sixid.SixID) string {
	return string(p.Letter(e)) + id.String()
}

// Decode parses a prefixed id that must belong to e.
func (p *Prefixed) Decode(s string, e Entity) (sixid.SixID, error) {
	got, id, err := p.DecodeAny(s)
	if err != nil {
		return sixid.SixID{}, err
	}
	if got != e {
		return sixid.SixID{}, fmt.Errorf("prefix: %q is a %s id, not a %s id", s, got, e)
	}
	return id, nil
}

// DecodeAny parses a prefixed id of any entity in the table.
func (p *Prefixed) DecodeAny(s string) (Entity, sixid.SixID, error) {
	if s == "" {
		return "", sixid.SixID{}, fmt.Errorf("prefix: empty id")
	}
	e, ok := p.entities[s[0]]
	if !ok {
		return "", sixid.SixID{}, fmt.Errorf("prefix: unknown prefix %q", s[:1])
	}
	id, err := sixid.ParseSixID(s[1:])
	if err != nil {
		return "", sixid.SixID{}, err
	}
	return e, id, nil
}

// Entities returns every entity in the table, sorted.
func (p *Prefixed) Entities() []Entity {
	out := make([]Entity, 0, len(p.letters))
	for e := range p.letters {
		out = append(out, e)
	}
	slices.Sort(out)
	return out
}
