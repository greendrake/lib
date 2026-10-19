package rpcargs

import (
	"bytes"
	"encoding/json"
)

// ParsePositional unmarshals a positional JSON-array argument payload into
// the given targets: element i lands in targets[i]. Fewer elements than
// targets leave the remaining targets at their zero values (callers apply
// defaults); a JSON null element likewise leaves its target untouched, and a
// request without arguments leaves every target so. More elements than
// targets is invalid, as is any non-array payload.
func ParsePositional(raw json.RawMessage, targets ...any) error {
	if len(raw) == 0 {
		return nil
	}
	var elements []json.RawMessage
	if err := json.Unmarshal(raw, &elements); err != nil || len(elements) > len(targets) {
		return invalid()
	}
	for i, el := range elements {
		if bytes.Equal(bytes.TrimSpace(el), []byte("null")) {
			continue
		}
		if err := json.Unmarshal(el, targets[i]); err != nil {
			return invalid()
		}
	}
	return nil
}
