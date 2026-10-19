// Package rpcargs parses and validates a method's arguments into the
// handler's own types: named ({…}) or positional ([…]), checked by the
// validator the request's dispatcher was given (rpcserver.WithValidator).
// NewValidator is go-playground/validator's, with whatever rules the host
// composes into it. Arguments that do not parse or validate are
// INVALID_ARGUMENT.
package rpcargs

import (
	"encoding/json"

	"greendrake.nz/go/rpcserver"
)

// invalid is the answer to arguments that do not parse.
func invalid() error {
	return rpcserver.NewAPIError(rpcserver.CodeInvalidArgument)
}

// ParseArguments supports both positional [arg] and named {field: value} argument formats.
// A request without arguments, which a client sends for a call it gives none, leaves
// the target at its zero value, as an empty array does.
func ParseArguments(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return nil
	}
	switch raw[0] {
	case '{':
		if err := json.Unmarshal(raw, target); err != nil {
			return invalid()
		}
		return nil
	case '[':
		var elements []json.RawMessage
		if err := json.Unmarshal(raw, &elements); err != nil {
			return invalid()
		}
		if len(elements) == 0 {
			return nil
		}
		if err := json.Unmarshal(elements[0], target); err != nil {
			return invalid()
		}
		return nil
	default:
		return invalid()
	}
}
