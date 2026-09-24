import type { StandardSchemaV1 } from '@standard-schema/spec'
import type { RpcRequest, RpcResponse } from '@greendrake/rpc'

// Who is calling, as the application's resolver reports it. `userId` is opaque
// — the application's own id type — and null for a caller no credential named,
// which is the one thing every auth rule but 'none' turns away.
export interface AuthInfo {
    userId: unknown
    isAdmin: boolean
    phantom: boolean
}

// The principal a request carries until a credential says otherwise. Not a
// stand-in for "unknown": a resolver that ran and matched nothing yields this
// too, because a token nobody recognises makes its bearer a stranger, not an
// error.
export const ANONYMOUS: AuthInfo = {
    userId: null,
    isAdmin: false,
    phantom: false
}

// Turns a credential into a principal, or null where it names nobody. What a
// method then requires of that principal is the dispatcher's business, so this
// never refuses: it only reports.
export interface AuthResolver {
    resolveToken(token: string, signal: AbortSignal): Promise<AuthInfo | null>
}

// Replay protection for the calls that carry a nonce: true the first time one
// is seen, false for a repeat.
export interface NonceStore {
    checkAndSet(nonce: string): Promise<boolean>
}

// Maps the language tag a client asked for onto one the application actually
// ships. Which locales exist and how a near miss resolves is the application's
// policy; this package only carries the value.
export type LocaleResolver = (requested: string | undefined) => string

// What the transport knows about a call before it dispatches.
export interface CallContext {
    auth: AuthInfo
    clientIp: string
    // Aborted when the caller goes away — an HTTP client disconnecting, a
    // WebSocket closing. Handlers pass it to whatever I/O they do, so work
    // nobody is waiting for is abandoned rather than run to completion.
    signal: AbortSignal
}

export interface HandlerContext extends CallContext {
    // Empty unless the dispatcher was given a LocaleResolver.
    locale: string
}

export type AuthRequirement = 'none' | 'user' | 'admin'

// One method: what it requires of the caller, what shape its argument has, and
// what runs. `A` defaults to void — a method with no `args` schema takes none,
// and ClientMethods turns that into a zero-argument call.
export interface MethodDef<A = void, R = unknown> {
    auth: AuthRequirement
    // Let a phantom — a provisional principal the application has not fully
    // registered — through. Off by default: a method reached by one says so.
    allowPhantom?: boolean
    // Validates the single argument and types the handler's. Any Standard
    // Schema validator (zod, valibot, arktype) qualifies; failures answer
    // INVALID_ARGUMENT with the per-field issues in `error_details`.
    args?: StandardSchemaV1<unknown, A>
    // Declared as a method rather than a property so the map below can hold
    // definitions of every argument type: parameter bivariance is what makes a
    // MethodDef<Something> a MethodDef<unknown>.
    handler(ctx: HandlerContext, args: A): R | Promise<R>
}

// A dispatch table. The erased argument type is what every concrete MethodDef
// widens to, so a table mixing schemas and bare methods is one value.
export type Methods = Record<string, MethodDef<unknown, unknown>>

// Identity at runtime; inference is its whole job. A definition written inline
// in a table has nowhere to get its argument type from — the table's own type
// erases it — so the schema's output reaches the handler's parameter through
// this. A definition with no schema takes no argument, and says so.
export const method = <A = void, R = unknown>(def: MethodDef<A, R>): MethodDef<A, R> => def

type MethodArgs<A> = [A] extends [void] ? [] : [args: A]

// The client-side view of a dispatch table: the method map a dashboard hands
// `ApiClient<…>`, derived from the definitions rather than restated beside
// them. The pair's typing meets here — a method whose argument shape or result
// changes server-side stops compiling in the frontend that calls it.
export type ClientMethods<M extends Methods> = {
    [K in keyof M]: M[K] extends MethodDef<infer A, infer R> ? (...args: MethodArgs<A>) => Awaited<R> : never
}

// The WebSocket transport's additions to the wire: an id correlating a
// response with its request, and the frames the server pushes unasked.
export interface WsResponse extends RpcResponse {
    type: 'response'
    // Undefined answering a frame whose id could not be read — one that did
    // not parse — and dropped from the wire by JSON, like an absent one.
    id?: string | undefined
}

export interface EventFrame {
    type: 'event'
    event: string
    data: unknown
}

export interface DataFrame {
    type: string
    data: unknown
}

// The wire types both halves share, re-exported so a server's modules have one
// place to import from.
export type { Codec, RpcRequest, RpcResponse } from '@greendrake/rpc'

// Everything that goes out on a socket, so the registry's send path is typed
// rather than `unknown`.
export type ServerFrame = WsResponse | EventFrame | DataFrame

// A request as it arrives: the shared envelope plus the WebSocket's id.
export interface IncomingRequest extends RpcRequest {
    id?: string
}
