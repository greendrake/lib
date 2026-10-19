import { NetworkError } from '@greendrake/util'

// A failure the server answered with: `code` is the response's `error` (and
// the message), `details` its `error_details` payload (the structured data a
// handler attaches alongside a code, e.g. the conflicting user's id on
// OAUTH_ALREADY_LINKED).
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

// Every `*_NOT_FOUND` code the server answers with. An app raising a not-found
// of its own — a route that resolved to nothing — gives none, and is
// NOT_FOUND.
export class NotFoundError extends ApiError {
    constructor(code = 'NOT_FOUND', details?: unknown) {
        super(code, details)
        this.name = 'NotFoundError'
    }
}

// Rejection value for a WebSocket that closes before it opens, or that closes
// while calls are in flight. Carries the CloseEvent fields so a drop is
// diagnosable (which close code, was it clean) instead of surfacing as a bare,
// contentless DOM Event. Extends NetworkError: a transport condition,
// classified alongside HTTP fetch failures by one instanceof check.
export class WebSocketClosedError extends NetworkError {
    readonly code: number
    readonly reason: string
    readonly wasClean: boolean
    // Whether the request this error failed was already on the wire when the
    // socket went. A request that never left cannot have had an effect and is
    // always safe to re-issue; one that was in flight may have executed
    // server-side with only its response lost, so re-issuing it is a decision
    // for the caller.
    readonly inFlight: boolean
    readonly #event: CloseEvent

    constructor(event: CloseEvent, inFlight = false) {
        super(`WebSocket closed (code ${event.code}${event.reason ? `, ${event.reason}` : ''}, clean=${event.wasClean})`)
        this.name = 'WebSocketClosedError'
        this.code = event.code
        this.reason = event.reason
        this.wasClean = event.wasClean
        this.inFlight = inFlight
        this.#event = event
    }

    // The same close, attributed to a request that was already on the wire.
    // A separate instance rather than a mutation: the emitted error is shared
    // by every close listener, and only the swept calls carry the flag.
    asInFlight(): WebSocketClosedError {
        return new WebSocketClosedError(this.#event, true)
    }

    // An unexpected drop worth reporting, vs the clean close a normal
    // navigation or logout produces (mere operational noise). 1000 = normal,
    // 1001 = going away; anything else — 1006 (abnormal, no close frame),
    // 1011/1012/1013 (server error / restart / overload) — is unexpected.
    get isAbnormal(): boolean {
        return !this.wasClean || ![1000, 1001].includes(this.code)
    }
}
