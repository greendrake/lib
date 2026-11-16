package rpckit

import (
	"errors"
	"fmt"

	"greendrake.nz/go/persist"
	"greendrake.nz/go/rpcserver"
	"greendrake.nz/go/rpcserver/rpcargs"
)

// Fail is the answer to a handler's failed read or write: an APIError is
// already one; a persist.ErrNotFound — which every store's not-found
// sentinel wraps — is notFoundCode; a persist.ErrInvalidSortField is the
// client's sort_by, refused as INVALID_ARGUMENT. Anything else is a fault,
// returned under method for the dispatcher to log and answer INTERNAL_ERROR.
func Fail(method string, err error, notFoundCode string) error {
	var apiErr *rpcserver.APIError
	switch {
	case errors.As(err, &apiErr):
		return apiErr
	case errors.Is(err, persist.ErrNotFound):
		return rpcserver.NewAPIError(notFoundCode)
	case errors.Is(err, persist.ErrInvalidSortField):
		return rpcserver.NewAPIErrorWithDetails(rpcserver.CodeInvalidArgument, []rpcargs.FieldError{{Field: "sort_by", Rule: "oneof"}})
	}
	return fmt.Errorf("%s: %w", method, err)
}
