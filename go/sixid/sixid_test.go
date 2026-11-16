package sixid

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewSixID(t *testing.T) {
	// Test creating a new SixID
	id1 := NewSixID()
	id2 := NewSixID()

	// Verify they're not zero (unless random generation failed)
	if id1.IsZero() {
		t.Error("NewSixID returned zero ID")
	}

	// Verify they're different (statistically should be true)
	if bytes.Equal(id1[:], id2[:]) {
		t.Error("Two generated SixIDs are identical")
	}

	// Verify length
	if len(id1) != 6 {
		t.Errorf("SixID has wrong length: got %d, want 6", len(id1))
	}
}

func TestSixIDString(t *testing.T) {
	tests := []struct {
		name     string
		id       SixID
		expected string
	}{
		{
			name:     "all zeros",
			id:       SixID{0, 0, 0, 0, 0, 0},
			expected: "0000000000",
		},
		{
			name:     "all 255s",
			id:       SixID{255, 255, 255, 255, 255, 255},
			expected: "ZZZZZZZZZ7",
		},
		{
			name:     "mixed values",
			id:       SixID{1, 2, 3, 4, 5, 6},
			expected: "1G0602M060",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.id.String()
			if result != tt.expected {
				t.Errorf("String() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseSixID(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    SixID
		shouldError bool
	}{
		{
			name:        "valid uppercase",
			input:       "0000000000",
			expected:    SixID{0, 0, 0, 0, 0, 0},
			shouldError: false,
		},
		{
			name: "valid lowercase",
			// Canonical tail: all-ones is "ZZZZZZZZZ7" (the last char carries
			// only 3 payload bits).
			input:       "zzzzzzzzz7",
			expected:    SixID{255, 255, 255, 255, 255, 255},
			shouldError: false,
		},
		{
			name:        "valid mixed case",
			input:       "1G0602M060",
			expected:    SixID{1, 2, 3, 4, 5, 6},
			shouldError: false,
		},
		{
			name:        "with hyphens",
			input:       "1G060-2M060",
			expected:    SixID{1, 2, 3, 4, 5, 6},
			shouldError: false,
		},
		{
			name:        "with spaces",
			input:       "1G060 2M060",
			expected:    SixID{1, 2, 3, 4, 5, 6},
			shouldError: false,
		},
		{
			name:        "empty string",
			input:       "",
			expected:    SixID{},
			shouldError: true,
		},
		{
			name:        "too short",
			input:       "123456789",
			expected:    SixID{},
			shouldError: true,
		},
		{
			name:        "too long",
			input:       "12345678901",
			expected:    SixID{},
			shouldError: true,
		},
		{
			name:        "invalid character",
			input:       "123456789!",
			expected:    SixID{},
			shouldError: true,
		},
		{
			name:        "confused o for 0",
			input:       "iG0602M060",
			expected:    SixID{1, 2, 3, 4, 5, 6},
			shouldError: false,
		},
		{
			name:        "confused i for 1",
			input:       "iG0602M060",
			expected:    SixID{1, 2, 3, 4, 5, 6},
			shouldError: false,
		},
		{
			name:        "confused l for 1",
			input:       "lG0602M060",
			expected:    SixID{1, 2, 3, 4, 5, 6},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseSixID(tt.input)
			if tt.shouldError {
				if err == nil {
					t.Errorf("ParseSixID(%q) should have returned an error", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("ParseSixID(%q) returned unexpected error: %v", tt.input, err)
				}
				if !bytes.Equal(result[:], tt.expected[:]) {
					t.Errorf("ParseSixID(%q) = %v, want %v", tt.input, result, tt.expected)
				}
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	// Test that we can round-trip through string representation
	ids := []SixID{
		{0, 0, 0, 0, 0, 0},
		{255, 255, 255, 255, 255, 255},
		{1, 2, 3, 4, 5, 6},
		{128, 64, 32, 16, 8, 4},
		NewSixID(),
	}

	for _, original := range ids {
		str := original.String()
		parsed, err := ParseSixID(str)
		if err != nil {
			t.Errorf("Failed to parse string %q: %v", str, err)
			continue
		}
		if !bytes.Equal(original[:], parsed[:]) {
			t.Errorf("Round-trip failed: original=%v, string=%q, parsed=%v", original, str, parsed)
		}
	}
}

func TestMarshalBinary(t *testing.T) {
	id := SixID{1, 2, 3, 4, 5, 6}

	data, err := id.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	if !bytes.Equal(data, []byte{1, 2, 3, 4, 5, 6}) {
		t.Errorf("MarshalBinary returned wrong data: %v", data)
	}
}

func TestUnmarshalBinary(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		expected    SixID
		shouldError bool
	}{
		{
			name:        "valid data",
			data:        []byte{1, 2, 3, 4, 5, 6},
			expected:    SixID{1, 2, 3, 4, 5, 6},
			shouldError: false,
		},
		{
			name:        "too short",
			data:        []byte{1, 2, 3, 4, 5},
			expected:    SixID{},
			shouldError: true,
		},
		{
			name:        "too long",
			data:        []byte{1, 2, 3, 4, 5, 6, 7},
			expected:    SixID{},
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var id SixID
			err := id.UnmarshalBinary(tt.data)
			if tt.shouldError {
				if err == nil {
					t.Error("UnmarshalBinary should have returned an error")
				}
			} else {
				if err != nil {
					t.Errorf("UnmarshalBinary returned unexpected error: %v", err)
				}
				if !bytes.Equal(id[:], tt.expected[:]) {
					t.Errorf("UnmarshalBinary result = %v, want %v", id, tt.expected)
				}
			}
		})
	}
}

func TestJSONMarshalUnmarshal(t *testing.T) {
	original := SixID{1, 2, 3, 4, 5, 6}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}

	// Verify the JSON contains the string representation
	expected := `"1G0602M060"`
	if string(data) != expected {
		t.Errorf("JSON marshal produced %s, want %s", data, expected)
	}

	// Unmarshal back
	var decoded SixID
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	if !bytes.Equal(original[:], decoded[:]) {
		t.Errorf("JSON round-trip failed: original=%v, decoded=%v", original, decoded)
	}
}

func TestJSONInStruct(t *testing.T) {
	type TestStruct struct {
		ID   SixID  `json:"id"`
		Name string `json:"name"`
	}

	original := TestStruct{
		ID:   SixID{10, 20, 30, 40, 50, 60},
		Name: "test",
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}

	// Unmarshal back
	var decoded TestStruct
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	if !bytes.Equal(original.ID[:], decoded.ID[:]) {
		t.Errorf("ID mismatch: original=%v, decoded=%v", original.ID, decoded.ID)
	}
	if original.Name != decoded.Name {
		t.Errorf("Name mismatch: original=%s, decoded=%s", original.Name, decoded.Name)
	}
}

func TestIsZero(t *testing.T) {
	tests := []struct {
		name     string
		id       SixID
		expected bool
	}{
		{
			name:     "all zeros",
			id:       SixID{0, 0, 0, 0, 0, 0},
			expected: true,
		},
		{
			name:     "one non-zero",
			id:       SixID{0, 0, 0, 1, 0, 0},
			expected: false,
		},
		{
			name:     "all non-zero",
			id:       SixID{1, 2, 3, 4, 5, 6},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.id.IsZero()
			if result != tt.expected {
				t.Errorf("IsZero() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBytes(t *testing.T) {
	id := SixID{1, 2, 3, 4, 5, 6}
	result := id.Bytes()

	expected := []byte{1, 2, 3, 4, 5, 6}
	if !bytes.Equal(result, expected) {
		t.Errorf("Bytes() = %v, want %v", result, expected)
	}
}

func BenchmarkNewSixID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewSixID()
	}
}

func BenchmarkSixIDString(b *testing.B) {
	id := NewSixID()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = id.String()
	}
}

func BenchmarkParseSixID(b *testing.B) {
	str := "041061050N"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseSixID(str)
	}
}

func TestParseSixIDConfusables(t *testing.T) {
	// Crockford confusables decode in both cases: O → 0, I/L → 1.
	canonical := SixID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}.String() // "1G0602M060"
	variants := map[string]string{
		"O for 0 upper": strings.ReplaceAll(canonical, "0", "O"),
		"o for 0 lower": strings.ReplaceAll(canonical, "0", "o"),
		"I for 1 upper": strings.ReplaceAll(canonical, "1", "I"),
		"L for 1 upper": strings.ReplaceAll(canonical, "1", "L"),
		"l for 1 lower": strings.ReplaceAll(canonical, "1", "l"),
	}
	want, err := ParseSixID(canonical)
	if err != nil {
		t.Fatalf("canonical parse: %v", err)
	}
	for name, s := range variants {
		got, err := ParseSixID(s)
		if err != nil {
			t.Errorf("%s (%q): unexpected error: %v", name, s, err)
			continue
		}
		if got != want {
			t.Errorf("%s (%q): got %v, want %v", name, s, got, want)
		}
	}
}

func TestParseSixIDRejectsNonCanonical(t *testing.T) {
	// The last character carries only 3 payload bits; a value with its high
	// bits set spells the same 6 bytes differently and must be rejected.
	canonical := NewSixID().String()
	if _, err := ParseSixID(canonical); err != nil {
		t.Fatalf("canonical parse: %v", err)
	}
	nonCanonical := canonical[:9] + "Z" // 'Z' = 31: high bits set
	if _, err := ParseSixID(nonCanonical); err == nil {
		t.Fatalf("expected non-canonical rejection for %q", nonCanonical)
	}
	// Canonical last characters ('0'-'7') always parse.
	for _, c := range "01234567" {
		if _, err := ParseSixID(canonical[:9] + string(c)); err != nil {
			t.Errorf("canonical tail %q rejected: %v", string(c), err)
		}
	}
}
