import { randomString } from '@greendrake/util'
import { ApiError, NotFoundError } from './errors'
import type { RpcRequest, RpcResponse } from './types'

export const packRequest = (method: string, args: unknown[], nonce: boolean, locale?: string): RpcRequest => {
    const request: RpcRequest = { method }
    if (nonce) {
        request.nonce = randomString()
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
// 'notfound' / '*_NOT_FOUND' get a NotFoundError (its own toast type),
// everything else an ApiError carrying the optional error_details payload.
export const makeError = (code: string, details?: unknown): Error => {
    if (code === 'notfound' || code.endsWith('_NOT_FOUND')) {
        // The code rides along: this class is chosen for its toast, not to
        // replace what the backend said, and a caller keying on the code
        // (codeOf) must still find it.
        return new NotFoundError(code)
    }
    return new ApiError(code, details)
}

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
