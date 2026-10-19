import { ApiError, NotFoundError } from './errors'
import type { RpcRequest, RpcResponse } from './types'
import type { WsAuthStrategy } from './WsTransport'

// The codes the protocol answers with itself, apart from those of the methods
// it carries. The README's wire section is their specification.
export const PROTOCOL_CODES = ['INTERNAL_ERROR', 'UNKNOWN_METHOD', 'INVALID_JSON', 'REQUEST_TOO_LARGE', 'INVALID_ARGUMENT', 'AUTH_REQUIRED', 'UNAUTHENTICATED', 'AUTH_NOT_CONFIGURED', 'DUPLICATE_REQUEST', 'WEBSOCKET_REQUIRED'] as const

export type ProtocolCode = (typeof PROTOCOL_CODES)[number]

// The WebSocket methods a server answers itself, binding the socket to the
// principal a token names and unbinding it, and the query parameter a socket
// can be opened with a token in.
export const AUTH_TOKEN_METHOD = 'auth.token'
export const AUTH_LOGOUT_METHOD = 'auth.logout'
export const TOKEN_PARAM = 'token'

// The in-band handshake over the reserved methods: what a WsTransport binding
// its socket to a session token is given.
export const tokenAuth: WsAuthStrategy = {
    authenticate: (send, token) => send({ method: AUTH_TOKEN_METHOD, arguments: [token] }),
    deauthenticate: send => send({ method: AUTH_LOGOUT_METHOD })
}

export const packRequest = (method: string, args: unknown[], nonce: boolean, locale?: string): RpcRequest => {
    const request: RpcRequest = { method }
    if (nonce) {
        // 128 random bits. Not crypto.randomUUID: that exists only in secure
        // contexts, and a page served over plain HTTP on a LAN is none.
        request.nonce = Array.from(crypto.getRandomValues(new Uint8Array(16)), byte => byte.toString(16).padStart(2, '0')).join('')
    }
    if (args.length) {
        request.arguments = args
    }
    if (locale) {
        request.locale = locale
    }
    return request
}

// Single owner of the error-code → Error mapping shared by both transports:
// '*_NOT_FOUND' gets a NotFoundError, everything else an ApiError, each
// carrying the code and the optional error_details payload.
export const makeError = (code: string, details?: unknown): ApiError => (code.endsWith('_NOT_FOUND') ? new NotFoundError(code, details) : new ApiError(code, details))

// Dev-only diagnostic for non-success responses. These are server-returned
// business outcomes (NOT_FOUND, INVALID_CREDENTIALS, …) the caller goes on to
// handle — not program faults — so they log at `warn`, never `error`: a
// handled outcome must not trip a test's console-error guard or read as a
// malfunction in the devtools console. Guarded optional access keeps this
// Node-safe (no import.meta.env outside Vite).
const logIfDev = (method: string, response: RpcResponse): void => {
    if ((import.meta as ImportMeta & { env?: { DEV?: boolean } }).env?.DEV) {
        console.warn('[rpc]', method, response)
    }
}

// Resolve a parsed response to its data, or throw the mapped error.
export const settleResponse = (response: RpcResponse, method: string): unknown => {
    if (response.success) {
        return response.data
    }
    logIfDev(method, response)
    throw makeError(response.error!, response.error_details)
}
