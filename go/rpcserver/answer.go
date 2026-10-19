package rpcserver

import (
	"errors"
	"fmt"
	"slices"
)

// ErrorCode is a sentinel and the code a method answers it with.
type ErrorCode struct {
	Err  error
	Code string
}

// Answer is a handler's failure as the wire says it: an *APIError is already
// one, and an error matching (errors.Is) one of codes is that code. Anything
// else is a fault, returned annotated with method for the dispatcher to log
// and answer CodeInternal.
func Answer(method string, err error, codes []ErrorCode) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	if i := slices.IndexFunc(codes, func(c ErrorCode) bool { return errors.Is(err, c.Err) }); i >= 0 {
		return NewAPIError(codes[i].Code)
	}
	return fmt.Errorf("%s: %w", method, err)
}
