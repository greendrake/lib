package wire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ParseHex reads a number as the device writes it in hex: "0x" and its digits, which
// one firmware pads to eight ("0x00000100") and another does not ("0x100"); digits is
// how many it has.
func ParseHex(s string) (n uint32, digits int, ok bool) {
	hex, found := strings.CutPrefix(s, "0x")
	if !found || hex == "" {
		return 0, 0, false
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, 0, false
	}
	return uint32(v), len(hex), true
}

// FormatHex writes n as the device does, in at least digits digits.
func FormatHex(n uint32, digits int) string {
	return fmt.Sprintf("0x%0*X", digits, n)
}

// DecodeExact decodes JSON keeping numbers as written, so that a value passing through
// is written back unchanged.
func DecodeExact(data json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var v any
	err := decoder.Decode(&v)
	return v, err
}

// FlagForm finds the form v, as DecodeExact reads it, writes a switch in (true or
// false, 1 or 0, or a hex 1 or 0), and whether it is on; write gives a switch in that
// form, a hex one in as many digits as v has.
func FlagForm(v any) (write func(on bool) any, on, known bool) {
	bit := map[bool]uint32{false: 0, true: 1}
	switch v := v.(type) {
	case bool:
		return func(on bool) any { return on }, v, true
	case json.Number:
		if v == "0" || v == "1" {
			return func(on bool) any { return json.Number(strconv.Itoa(int(bit[on]))) }, v == "1", true
		}
	case string:
		if n, digits, ok := ParseHex(v); ok && n <= 1 {
			return func(on bool) any { return FormatHex(bit[on], digits) }, n == 1, true
		}
	}
	return nil, false, false
}

// Overlay lays top over base, as a write onto a configuration the device has must:
// objects merge key by key, arrays of the same length element by element, a bool over
// a switch takes the form the device writes the switch in, a hex value over a hex
// value as many digits as the device's, and anything else top replaces.
func Overlay(base, top json.RawMessage) (json.RawMessage, error) {
	b, err := DecodeExact(base)
	if err != nil {
		return nil, err
	}
	t, err := DecodeExact(top)
	if err != nil {
		return nil, err
	}
	return json.Marshal(merge(b, t))
}

func merge(base, top any) any {
	switch t := top.(type) {
	case map[string]any:
		if b, ok := base.(map[string]any); ok {
			for k, v := range t {
				b[k] = merge(b[k], v)
			}
			return b
		}
	case []any:
		if b, ok := base.([]any); ok && len(b) == len(t) {
			for i, v := range t {
				b[i] = merge(b[i], v)
			}
			return b
		}
	case bool:
		if write, _, known := FlagForm(base); known {
			return write(t)
		}
	case string:
		if b, ok := base.(string); ok {
			if _, digits, ok := ParseHex(b); ok {
				if n, _, ok := ParseHex(t); ok {
					return FormatHex(n, digits)
				}
			}
		}
	}
	return top
}
