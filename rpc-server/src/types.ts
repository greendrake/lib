import type { StandardSchemaV1 } from '@standard-schema/spec'
import type { RpcRequest, RpcResponse } from '@greendrake/rpc'
import { ApiError } from './errors'

// Turns a credential into the principal it names, or null where it names
// nobody. `P` is the application's principal — a user id, a session, an
// operator — and nothing here reads it. What a method then requires of the
// principal is the dispatcher's business, so this never refuses: it only
// reports.
export interface AuthResolver<P> {
    resolveToken(token: string, signal: AbortSignal): Promise<P | null>
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

// The address a call came from, given its request and the address of the
// socket it arrived on. See trustProxy.
export type ClientIpResolver = (request: Request, peer: string) => string

// The calling socket's topic memberships: what a method that subscribes its
// caller to something joins it to, and leaves. See ConnRegistry's topics.
export interface Topics {
    join(topic: string): void
    leave(topic: string): void
}

// What the transport knows about a call before it dispatches.
export interface CallContext<P> {
    // Who is calling, as the resolver named them; null for a caller no
    // credential named.
    principal: P | null
    clientIp: string
    // Aborted when the caller goes away — an HTTP client disconnecting, a
    // WebSocket closing, an abort naming the call — so handlers pass it to
    // whatever I/O they do, and work nobody is waiting for is abandoned rather
    // than run to completion.
    signal: AbortSignal
    // The caller's socket, for a method that subscribes it. Null for a call
    // that came over HTTP: there is no connection to push to afterwards.
    topics: Topics | null
}

export interface HandlerContext<P> extends CallContext<P> {
    // Empty unless the dispatcher was given a LocaleResolver.
    locale: string
}

// 'required' turns away a caller no credential named.
export type AuthRequirement = 'none' | 'required'

// What a method asks of its caller. A library shipping methods takes this
// from the application rather than deciding it, since only the application
// knows its principals.
export interface MethodAccess<P> {
    auth: AuthRequirement
    // The method's own policy over the principal that passed `auth` (null on an
    // `auth: 'none'` method called anonymously): true admits, and anything else
    // is the code the call is refused with. Method syntax, like the handler's:
    // it keeps definitions over one principal type mixable with those over
    // none.
    access?(principal: P | null): true | string
}

// One method: what it requires of the caller, what shape its argument has, and
// what runs. `A` defaults to void — a method with no `args` schema takes none,
// and ClientMethods turns that into a zero-argument call. `P` defaults to
// never, a service with no principals, whose callers are all null.
export interface MethodDef<A = void, R = unknown, P = never> extends MethodAccess<P> {
    // Validates the single argument and types the handler's. Any Standard
    // Schema validator (zod, valibot, arktype) qualifies; failures answer
    // INVALID_ARGUMENT with the per-field issues in `error_details`.
    args?: StandardSchemaV1<unknown, A>
    // Declared as a method rather than a property so the map below can hold
    // definitions of every argument type: parameter bivariance is what makes a
    // MethodDef<Something> a MethodDef<unknown>.
    handler(ctx: HandlerContext<P>, args: A): R | Promise<R>
}

// A dispatch table. The erased argument type is what every concrete MethodDef
// widens to, so a table mixing schemas and bare methods is one value.
export type Methods<P = never> = Record<string, MethodDef<unknown, unknown, P>>

// The caller's socket, for a method that is nothing without one: a call that
// came over HTTP is refused with WEBSOCKET_REQUIRED rather than subscribed to
// pushes it has no connection to receive.
export const topicsOf = <P>(ctx: CallContext<P>): Topics => {
    if (ctx.topics === null) {
        throw new ApiError('WEBSOCKET_REQUIRED')
    }
    return ctx.topics
}

// Identity at runtime; inference is its whole job. A definition written inline
// in a table has nowhere to get its argument type from — the table's own type
// erases it — so the schema's output reaches the handler's parameter through
// this. A definition with no schema takes no argument, and says so.
export const method = <A = void, R = unknown, P = never>(def: MethodDef<A, R, P>): MethodDef<A, R, P> => def

type MethodArgs<A> = [A] extends [void] ? [] : [args: A]

// The client-side view of a dispatch table: the method map a frontend hands
// its typed client, derived from the definitions rather than restated beside
// them. The pair's typing meets here — a method whose argument shape or result
// changes server-side stops compiling in the frontend that calls it.
export type ClientMethods<M extends Methods> = {
    [K in keyof M]: M[K] extends MethodDef<infer A, infer R, never> ? (...args: MethodArgs<A>) => Awaited<R> : never
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

// A request as the dispatcher takes it: the shared envelope plus the
// WebSocket's id.
export interface IncomingRequest extends RpcRequest {
    id?: string
}

// What arrives on a socket: a request, an abort naming an earlier request's
// nonce, or both in one frame.
export type IncomingFrame = Partial<IncomingRequest> & { abort?: string }
