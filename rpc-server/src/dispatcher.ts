import { unwrapArguments, validateArguments } from './arguments'
import { ApiError } from './errors'
import type { AuthResolver, CallContext, ClientIpResolver, HandlerContext, IncomingRequest, LocaleResolver, Methods, NonceStore, RpcResponse } from './types'

export interface DispatcherOptions<P> {
    // Absent means the service has no notion of identity: no method it serves
    // may require a principal, and an in-band auth frame answers
    // AUTH_NOT_CONFIGURED.
    auth?: AuthResolver<P>
    // Absent means nonces are carried but not checked — fine for a
    // single-caller service, not for one exposed to retries it cannot afford
    // to apply twice.
    nonces?: NonceStore
    locale?: LocaleResolver
    // How both transports derive a call's address. The socket peer's unless
    // given: a header naming another is believed only where the application
    // says a proxy it trusts sets it (trustProxy).
    clientIp?: ClientIpResolver
    // Where faults go: anything a method fails with but an ApiError, while its
    // caller is still there. console.error unless given.
    onFault?: (method: string, error: unknown) => void
}

// The engine both transports share: everything between a parsed envelope and a
// response, and nothing about how either got there.
export interface Dispatcher<P> {
    // Exposed because the transports resolve credentials of their own — a
    // bearer header, a query parameter at the WebSocket handshake, an in-band
    // auth frame — before there is a request to dispatch.
    readonly auth: AuthResolver<P> | undefined
    readonly clientIp: ClientIpResolver
    readonly onFault: (method: string, error: unknown) => void
    dispatch(request: IncomingRequest, context: CallContext<P>): Promise<RpcResponse>
}

// The rightmost X-Forwarded-For entry — the address the proxy in front of the
// service saw connect to it — or the peer's where there is none. Only for a
// service every request reaches through a proxy that appends to the header:
// anything left of that entry is the client's to write.
export const trustProxy: ClientIpResolver = (request, peer) => request.headers.get('x-forwarded-for')?.split(',').at(-1)?.trim() || peer

// A refusal, as both transports and the dispatcher answer one.
export const failure = (error: string, details?: unknown): RpcResponse => ({
    success: false,
    error,
    error_details: details
})

export const createDispatcher = <P = never>(methods: Methods<P>, options: DispatcherOptions<P> = {}): Dispatcher<P> => {
    if (!options.auth) {
        const needy = Object.entries(methods)
            .filter(([, def]) => def.auth === 'required')
            .map(([name]) => name)
        if (needy.length) {
            throw new Error(`createDispatcher: ${needy.join(', ')} require a principal, and there is no AuthResolver to name one`)
        }
    }
    const onFault = options.onFault ?? ((method: string, error: unknown) => console.error(`[rpc-server] ${method}:`, error))
    return {
        auth: options.auth,
        clientIp: options.clientIp ?? ((_request, peer) => peer),
        onFault,
        async dispatch(request, context) {
            try {
                // Own properties only: a method named `toString` or
                // `constructor` is not a method this service serves.
                const def = Object.hasOwn(methods, request.method) ? methods[request.method] : undefined
                if (def === undefined) {
                    return failure('UNKNOWN_METHOD')
                }
                if (def.auth === 'required' && context.principal === null) {
                    return failure('AUTH_REQUIRED')
                }
                const admitted = def.access?.(context.principal) ?? true
                if (admitted !== true) {
                    return failure(admitted)
                }
                // Only now is a repeat refused, so a call turned away above
                // leaves its nonce unspent for a retry that may yet succeed.
                if (request.nonce && options.nonces && !(await options.nonces.checkAndSet(request.nonce))) {
                    return failure('DUPLICATE_REQUEST')
                }
                const ctx: HandlerContext<P> = { ...context, locale: options.locale?.(request.locale) ?? '' }
                const args = def.args ? await validateArguments(def.args, unwrapArguments(request.arguments)) : undefined
                return { success: true, data: await def.handler(ctx, args) }
            } catch (e) {
                if (e instanceof ApiError) {
                    return failure(e.code, e.details)
                }
                // A request whose caller has already gone fails in whatever way
                // the abandoned I/O reports; that is the disconnect, not a
                // fault of the service, and its response is discarded anyway.
                if (!context.signal.aborted) {
                    onFault(request.method, e)
                }
                return failure('INTERNAL_ERROR')
            }
        }
    }
}
