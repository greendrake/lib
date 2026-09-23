import { unwrapArguments, validateArguments } from './arguments'
import { ApiError } from './errors'
import type { AuthResolver, CallContext, HandlerContext, IncomingRequest, LocaleResolver, Methods, NonceStore, RpcResponse } from './types'

export interface DispatcherOptions {
    // Absent means the service has no notion of identity: every method it
    // serves must be `auth: 'none'`, and an in-band auth frame answers
    // AUTH_NOT_CONFIGURED.
    auth?: AuthResolver
    // Absent means nonces are carried but not checked — fine for a
    // single-caller service, not for one exposed to retries it cannot afford
    // to apply twice.
    nonces?: NonceStore
    locale?: LocaleResolver
}

// The engine both transports share: everything between a parsed envelope and a
// response, and nothing about how either got there.
export interface Dispatcher {
    // Exposed because the transports resolve credentials of their own — a
    // bearer header, a query parameter at the WebSocket handshake, an in-band
    // auth frame — before there is a request to dispatch.
    readonly auth?: AuthResolver
    dispatch(request: IncomingRequest, context: CallContext): Promise<RpcResponse>
}

const failure = (error: string, details?: unknown): RpcResponse => ({
    success: false,
    error,
    error_details: details
})

export const createDispatcher = (methods: Methods, options: DispatcherOptions = {}): Dispatcher => ({
    auth: options.auth,
    async dispatch(request, context) {
        try {
            // A repeat of a call that already ran is refused before anything
            // else: the point of the nonce is that its effect happens once.
            if (request.nonce && options.nonces && !(await options.nonces.checkAndSet(request.nonce))) {
                return failure('DUPLICATE_REQUEST')
            }
            // Own properties only: a method named `toString` or `constructor`
            // is not a method this service serves.
            if (!Object.hasOwn(methods, request.method)) {
                return failure('UNKNOWN_METHOD')
            }
            const def = methods[request.method]
            if (def.auth !== 'none') {
                if (context.auth.userId === null) {
                    return failure('AUTH_REQUIRED')
                }
                if (def.auth === 'admin' && !context.auth.isAdmin) {
                    return failure('ADMIN_REQUIRED')
                }
                if (context.auth.phantom && !def.allowPhantom) {
                    return failure('PHANTOM_NOT_ALLOWED')
                }
            }
            const ctx: HandlerContext = { ...context, locale: options.locale?.(request.locale) ?? '' }
            const args = def.args ? await validateArguments(def.args, unwrapArguments(request.arguments)) : undefined
            return { success: true, data: await def.handler(ctx, args) }
        } catch (e) {
            if (e instanceof ApiError) {
                return failure(e.code, e.details)
            }
            // A request whose caller has already gone fails in whatever way the
            // abandoned I/O reports; that is the disconnect, not a fault of the
            // service, and its response is discarded anyway.
            if (!context.signal.aborted) {
                console.error(`[rpc-server] ${request.method}:`, e)
            }
            return failure('INTERNAL_ERROR')
        }
    }
})
