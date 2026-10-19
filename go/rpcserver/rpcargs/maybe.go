package rpcargs

import (
	"bytes"
	"encoding/json"
)

// Maybe[T] is a tri-state field: "absent from payload", "present as null",
// or "present with a value". Standard `*T` cannot distinguish the first
// two cases because Go's encoding/json folds both into a nil pointer.
//
// Typical use is in API-update structs where the client sends only the
// fields they changed (diff semantics), and where a field may need to be
// cleared explicitly (sending `null`) versus merely left untouched.
//
//	type ItemUpdate struct {
//	    Title *string        `json:"title,omitempty"`
//	    Price Maybe[float64] `json:"price,omitempty"`
//	}
//
//	// in handler:
//	if u.Price.Set { … }        // present in any form
//	if u.Price.Null { … }       // present as null → clear
//	if u.Price.HasValue() { … } // present with a value
type Maybe[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// HasValue reports whether the field is set to a concrete value
// (i.e. present in the payload and not null).
func (m Maybe[T]) HasValue() bool { return m.Set && !m.Null }

// UnmarshalJSON marks the field as Set and decodes the value (or notes
// Null when the payload contains an explicit null literal).
func (m *Maybe[T]) UnmarshalJSON(b []byte) error {
	m.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		m.Null = true
		return nil
	}
	return json.Unmarshal(b, &m.Value)
}

// MarshalJSON returns "null" for cleared fields, the value for set
// fields, and "null" for unset Maybe values (caller should use
// `omitempty`-style filtering at the struct boundary if absence
// matters on the wire).
func (m Maybe[T]) MarshalJSON() ([]byte, error) {
	if !m.Set || m.Null {
		return []byte("null"), nil
	}
	return json.Marshal(m.Value)
}
