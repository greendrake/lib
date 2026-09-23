// A business outcome the caller is meant to see: `code` becomes the response's
// `error`, `details` its `error_details` (the structured payload a handler
// attaches alongside a code — the per-field list argument validation produces,
// the conflicting record's id, whatever the method's contract says).
//
// Anything else a handler throws is a fault, not an outcome: it is logged and
// answered INTERNAL_ERROR, so an unforeseen failure never leaks its internals
// to a client.
export class ApiError extends Error {
    readonly code: string
    readonly details?: unknown

    constructor(code: string, details?: unknown) {
        super(code)
        this.name = 'ApiError'
        this.code = code
        this.details = details
    }
}
