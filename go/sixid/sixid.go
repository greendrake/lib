// Package sixid is entity ids of six random bytes, written as ten Crockford Base32
// characters.
package sixid

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
)

// SixID is a 6-byte ID
type SixID [6]byte

// NewSixID creates a new 6-byte SixID using random data, so a test cannot
// predict the ids a store assigns by calling it. When a test needs to decide
// them, the store takes its id generator — a func() SixID — as a dependency
// instead: NewSixID in production, a function returning known ids in the test.
func NewSixID() SixID {
	// crypto/rand.Read never returns an error: a failing source crashes the
	// program.
	var id SixID
	_, _ = rand.Read(id[:])
	return id
}

// ParseSixID parses a string into a SixID from its Crockford Base32 string representation.
func ParseSixID(s string) (SixID, error) {
	return ParseCrockfordSixID(s)
}

// Crockford Base32 encoding alphabet (uppercase)
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Mapping from Crockford Base32 chars to their values
var crockfordDecodeMap map[byte]byte

func init() {
	// Initialize the decoding map
	crockfordDecodeMap = make(map[byte]byte, 32)
	for i := range crockfordAlphabet {
		crockfordDecodeMap[crockfordAlphabet[i]] = byte(i)
	}

	// Add lowercase variants
	lower := strings.ToLower(crockfordAlphabet)
	for i := range lower {
		if i >= 10 { // Skip numbers
			crockfordDecodeMap[lower[i]] = byte(i)
		}
	}

	// Commonly confused characters, per Crockford: O reads as 0, I and L as 1
	// — in both cases (the alphabet itself excludes O, I and L).
	for _, c := range []byte{'O', 'o'} {
		crockfordDecodeMap[c] = 0
	}
	for _, c := range []byte{'I', 'i', 'L', 'l'} {
		crockfordDecodeMap[c] = 1
	}
}

// String returns the Crockford Base32 (uppercase) representation of the 6-byte SixID
func (u SixID) String() string {
	if len(u) != 6 {
		return ""
	}

	var bytes = u[:]

	// 6 bytes = 48 bits, requires ceil(48/5) = 10 characters in Base32
	result := make([]byte, 10)
	var bits, offset uint
	resultIndex := 0

	for i := range 6 {
		bits |= uint(bytes[i]) << offset
		offset += 8

		for offset >= 5 {
			result[resultIndex] = crockfordAlphabet[bits&0x1F]
			resultIndex++
			bits >>= 5
			offset -= 5
		}
	}

	if offset > 0 {
		result[resultIndex] = crockfordAlphabet[bits&0x1F]
		resultIndex++
	}

	return string(result[:resultIndex])
}

// ParseCrockfordSixID converts a Crockford Base32 string back to 6-byte SixID.
// An empty string is no id.
func ParseCrockfordSixID(s string) (SixID, error) {
	// Remove hyphens and spaces for leniency
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")

	// Must be exactly 10 characters for 6 bytes (48 bits)
	if len(s) != 10 {
		return SixID{}, errors.New("invalid Crockford Base32 SixID: string length must be 10")
	}

	var bits uint64
	var offset uint
	bytes := make([]byte, 6)
	byteIndex := 0

	for i := range 10 {
		val, ok := crockfordDecodeMap[s[i]]
		if !ok {
			return SixID{}, errors.New("invalid character in Crockford Base32 SixID")
		}

		bits |= uint64(val) << offset
		offset += 5

		for offset >= 8 && byteIndex < 6 {
			bytes[byteIndex] = byte(bits & 0xFF)
			byteIndex++
			bits >>= 8
			offset -= 8
		}
	}

	if byteIndex != 6 {
		return SixID{}, errors.New("invalid Crockford Base32 SixID: couldn't decode 6 bytes")
	}

	// 10 chars carry 50 bits; 6 bytes consume 48. The 2 leftover high bits of
	// the last character must be zero, or the string is non-canonical — a
	// distinct spelling that would silently alias another id. Reject: every
	// canonical string (as produced by String()) round-trips; nothing else may.
	if bits != 0 {
		return SixID{}, errors.New("invalid Crockford Base32 SixID: non-canonical encoding")
	}

	var id SixID
	copy(id[:], bytes)
	return id, nil
}

// MarshalBinary implements the encoding.BinaryMarshaler interface.
func (u SixID) MarshalBinary() ([]byte, error) {
	return u[:], nil
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface.
func (u *SixID) UnmarshalBinary(data []byte) error {
	if len(data) != 6 {
		return errors.New("invalid SixID length")
	}
	copy((*u)[:], data)
	return nil
}

// MarshalJSON marshals the SixID as a JSON string in Crockford Base32 format.
func (u SixID) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String())
}

// UnmarshalJSON unmarshals a SixID from a JSON string in Crockford Base32 format.
func (u *SixID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseSixID(s)
	if err != nil {
		return err
	}
	*u = parsed
	return nil
}

// IsZero returns true if the SixID is all zeros
func (u SixID) IsZero() bool {
	for i := range 6 {
		if u[i] != 0 {
			return false
		}
	}
	return true
}

// Bytes returns the raw byte representation of the SixID
func (u SixID) Bytes() []byte {
	return u[:]
}
