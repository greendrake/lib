package rpcserver

// The codes the protocol answers with itself, apart from those of the methods
// it carries. `@greendrake/rpc`'s README is their specification, which every
// server of the format implements.
const (
	// CodeInternal answers a handler's fault: any error but an *APIError.
	CodeInternal = "INTERNAL_ERROR"
	// CodeUnknownMethod answers a method the dispatch table does not have.
	CodeUnknownMethod = "UNKNOWN_METHOD"
	// CodeInvalidJSON answers a body or frame that is no request envelope.
	CodeInvalidJSON = "INVALID_JSON"
	// CodeRequestTooLarge answers an HTTP body over the transport's limit.
	CodeRequestTooLarge = "REQUEST_TOO_LARGE"
	// CodeInvalidArgument answers arguments that do not parse or validate;
	// its details, where there are any, are a []rpcargs.FieldError.
	CodeInvalidArgument = "INVALID_ARGUMENT"
	// CodeAuthRequired answers a method requiring a principal called without
	// one.
	CodeAuthRequired = "AUTH_REQUIRED"
	// CodeUnauthenticated answers an auth.token frame whose token names no
	// principal.
	CodeUnauthenticated = "UNAUTHENTICATED"
	// CodeAuthNotConfigured answers an auth.token frame on a dispatcher with no
	// AuthResolver.
	CodeAuthNotConfigured = "AUTH_NOT_CONFIGURED"
	// CodeDuplicateRequest answers a nonce already used.
	CodeDuplicateRequest = "DUPLICATE_REQUEST"
)

// APIError is a failure the client is told of: Code is the response's error,
// and Details, when non-nil, its error_details — the per-field failures of
// argument validation, say. Any other error a handler returns is a fault,
// logged and answered CodeInternal.
type APIError struct {
	Code    string
	Details any
}

func (e *APIError) Error() string { return e.Code }

func NewAPIError(code string) *APIError {
	return &APIError{Code: code}
}

func NewAPIErrorWithDetails(code string, details any) *APIError {
	return &APIError{Code: code, Details: details}
}
