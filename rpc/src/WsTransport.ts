import { randomString } from '@greendrake/util'
import { identityCodec } from './types'
import type { Codec, RpcRequest, RpcResponse, Transport } from './types'
import { settleResponse } from './protocol'
import type { WebSocketPipe } from './WebSocketPipe'

export type WsSend = (payload: RpcRequest) => Promise<unknown>

// The over-the-wire authentication handshake (which methods to call, the
// argument shape) is application protocol, not transport, so it is injected
// rather than hardcoded here. The transport owns only the generic lifecycle:
// caching the credential and re-applying it whenever the socket (re)connects.
// Apps with no per-session auth simply omit the strategy (or use the pipe's
// queryAuth for connection-level credentials).
export interface WsAuthStrategy {
    authenticate(send: WsSend, token: string): Promise<unknown>
    deauthenticate(send: WsSend): Promise<unknown>
}

// App-declared server-push surface: name → payload type.
//    interface Pushes { 'threads.updated': ThreadsPatch }
export type PushMap = Record<string, unknown>

export type PushMapConstraint<P> = Record<keyof P, unknown>

interface WsResponseFrame extends RpcResponse {
    id: string
}

interface PushFrame {
    type: string
    event?: string
    data?: unknown
}

export interface WsTransportOptions {
    pipe: WebSocketPipe
    codec?: Codec
    auth?: WsAuthStrategy
}

// How long a liveness probe waits for an answer before calling the socket dead.
// A deadline on a network response, not a wait for something to happen: the
// probe exists precisely because a half-open socket produces no event to wait
// for.
const DEFAULT_PROBE_DEADLINE_MS = 5000

interface PendingCall {
    settle(frame: WsResponseFrame): void
    fail(error: Error): void
}

// The wire format over a WebSocketPipe: id-correlated request/response, typed
// server-push subscription, injected auth re-applied on every (re)connect.
// PEvents types `{type:'event', event, data}` frames; PData types the bare
// `{type, data}` frames some backends push. Both default to uncallable-empty —
// an app must declare its push surface to subscribe.
export class WsTransport<PEvents extends PushMapConstraint<PEvents> = Record<never, never>, PData extends PushMapConstraint<PData> = Record<never, never>> implements Transport {
    readonly pipe: WebSocketPipe
    readonly #codec: Codec
    readonly #auth: WsAuthStrategy | undefined
    // Who this transport is to be bound as — cached the moment the credential
    // is handed over, not once a bind succeeds. It states what every
    // connection of this transport should authenticate as, independently of
    // whether any single attempt got through, so a refused or interrupted bind
    // still re-applies on the next reconnect.
    #token: string | null = null
    // The bind issued for the live socket: the credential it carries and its
    // outcome. Dropped when that socket goes (or the identity is released), so
    // each connection binds exactly once — authenticate() and the reconnect
    // handler converge on this record rather than both putting an auth frame
    // on the same socket.
    #binding: { token: string; done: Promise<unknown> } | undefined
    // In-flight calls keyed by request id. A dropped socket never delivers
    // their responses, so they are failed explicitly on close.
    readonly #pending = new Map<string, PendingCall>()
    readonly #eventHandlers = new Map<string, Set<(data: never) => void>>()
    readonly #dataHandlers = new Map<string, Set<(data: never) => void>>()
    // Whether calls can currently succeed — an open socket that has finished
    // binding its identity, not merely an open one. See the 'open' handler.
    #ready = false
    // Bumped on every open and every close, so work deferred across one of
    // those events can tell whether the connection it belongs to is still the
    // live one.
    #connectionEpoch = 0
    // Bumped on every frame the socket delivers, whatever it carries: the one
    // piece of evidence that distinguishes a live connection from a half-open
    // one. Read by probe() across its deadline.
    #framesReceived = 0
    readonly #connectionListeners = new Set<(connected: boolean) => void>()

    constructor(options: WsTransportOptions) {
        this.pipe = options.pipe
        this.#codec = options.codec ?? identityCodec
        this.#auth = options.auth
        this.pipe.on('message', raw => this.#onRaw(raw))
        // Bind the identity on every (re)connect so a dropped socket comes
        // back as the same user, and withhold the readiness report until that
        // bind settles. A consumer resuming calls on a bare 'open' would race
        // the bind frame — the loser comes back UNAUTHENTICATED, which an app
        // reasonably reads as a lost session, so a blip could log the user
        // out. Settled either way is enough: a *refused* bind means the
        // session is genuinely gone, and a resumed call surfacing that is the
        // correct outcome. Fire-and-forget otherwise: no caller awaits a
        // reconnect's bind, and an authenticate() joined to this same record
        // still receives it.
        this.pipe.on('open', () => {
            const epoch = ++this.#connectionEpoch
            if (!this.#token) {
                this.#reportConnection(true)
                return
            }
            void this.#bind(this.#token)
                .catch(() => {})
                .then(() => {
                    // A bind settling after its socket has gone must not
                    // report a connection that no longer exists; whatever
                    // replaced it reports for itself.
                    if (this.#connectionEpoch === epoch) {
                        this.#reportConnection(true)
                    }
                })
        })
        // Fail anything in flight when the socket closes: its response can
        // never arrive, so the call would otherwise hang forever (and the UI
        // awaiting it with it). The close error carries the code/reason; the
        // swept calls get the in-flight attribution, while the error as
        // emitted (and as ensureConnected rejects with) stays unflagged —
        // those requests never reached the wire.
        this.pipe.on('close', error => {
            this.#connectionEpoch++
            // The bind belonged to the socket that just went; the next open
            // issues a fresh one.
            this.#binding = undefined
            const pending = [...this.#pending.values()]
            this.#pending.clear()
            this.#reportConnection(false)
            pending.forEach(call => call.fail(error.asInFlight()))
        })
    }

    // Ask the server whether this socket is still a socket. A connection torn
    // down without a FIN — a backgrounded mobile app, an idle NAT mapping
    // expiring — stays OPEN to the browser: no close event, so nothing fails,
    // nothing reconnects, and calls made on it hang. `method` is the app's
    // cheapest round trip; an answer means live, silence past the deadline
    // means the socket is abandoned and reconnected, which puts the normal
    // close/reconnect path (and whatever the app hangs off it) back in charge.
    //
    // Resolves true when the socket is alive. Nothing to probe when the
    // transport is already down: a reconnect is in flight and will report for
    // itself.
    async probe(method: string, deadlineMs = DEFAULT_PROBE_DEADLINE_MS): Promise<boolean> {
        if (!this.connected) {
            return false
        }
        const mark = this.#framesReceived
        let timer: ReturnType<typeof setTimeout> | undefined
        const expiry = new Promise<false>(resolve => {
            timer = setTimeout(() => resolve(false), deadlineMs)
        })
        try {
            const answered = await Promise.race([this.send({ method }).then(() => true), expiry])
            if (answered) {
                return true
            }
        } catch {
            // A refusal is an answer: the socket carried it.
            return true
        } finally {
            clearTimeout(timer)
        }
        // What the deadline is really on is silence, not this one answer: the
        // frames share an ordered connection, so a slow link transferring a
        // large page ahead of the probe can outlast the deadline on a socket
        // that is demonstrably delivering. Anything received since the probe
        // went out settles the question the probe was asking, and killing a
        // working socket is not a free retry — it fails every call in flight,
        // each of which then surfaces as a failure the user did not cause.
        if (this.#framesReceived !== mark) {
            return true
        }
        this.pipe.drop()
        this.pipe.reconnectNow()
        return false
    }

    // Whether calls can currently succeed. Together with onConnectionChange,
    // this is the connectivity oracle an app hands to its UX layer.
    get connected(): boolean {
        return this.#ready
    }

    // Connection transitions, one call per underlying event and deliberately
    // NOT deduplicated: consecutive `false` reports are consecutive failed
    // attempts, which is how a consumer tells a blip from a real outage.
    onConnectionChange(listener: (connected: boolean) => void): () => void {
        this.#connectionListeners.add(listener)
        return () => {
            this.#connectionListeners.delete(listener)
        }
    }

    #reportConnection(connected: boolean): void {
        this.#ready = connected
        this.#connectionListeners.forEach(listener => listener(connected))
    }

    connect(): Promise<void> {
        return this.pipe.ensureConnected()
    }

    #onRaw(raw: string): void {
        this.#framesReceived++
        const frame = JSON.parse(this.#codec.decode(raw)) as WsResponseFrame | PushFrame
        if ('id' in frame && typeof frame.id === 'string') {
            const pending = this.#pending.get(frame.id)
            if (pending) {
                this.#pending.delete(frame.id)
                pending.settle(frame as WsResponseFrame)
            }
            return
        }
        const push = frame as PushFrame
        if (push.type === 'event' && typeof push.event === 'string') {
            this.#dispatch(this.#eventHandlers, push.event, push.data)
        } else if (push.type && push.type !== 'response') {
            this.#dispatch(this.#dataHandlers, push.type, push.data)
        }
    }

    #dispatch(handlers: Map<string, Set<(data: never) => void>>, key: string, data: unknown): void {
        handlers.get(key)?.forEach(handler => (handler as (d: unknown) => void)(data))
    }

    #subscribe(handlers: Map<string, Set<(data: never) => void>>, key: string, handler: (data: never) => void): () => void {
        let set = handlers.get(key)
        if (!set) {
            set = new Set()
            handlers.set(key, set)
        }
        set.add(handler)
        return () => {
            set.delete(handler)
        }
    }

    // Subscribe to `{type:'event', event: name}` server pushes.
    onEvent<K extends keyof PEvents & string>(event: K, handler: (data: PEvents[K]) => void): () => void {
        return this.#subscribe(this.#eventHandlers, event, handler)
    }

    // Subscribe to bare `{type: name}` server pushes.
    onData<K extends keyof PData & string>(type: K, handler: (data: PData[K]) => void): () => void {
        return this.#subscribe(this.#dataHandlers, type, handler)
    }

    async send(request: RpcRequest): Promise<unknown> {
        await this.pipe.ensureConnected()
        return new Promise((resolve, reject) => {
            const id = randomString('rpc-')
            this.#pending.set(id, {
                settle: frame => {
                    try {
                        resolve(settleResponse(frame, request.method))
                    } catch (e) {
                        reject(e)
                    }
                },
                fail: reject
            })
            this.pipe.send(this.#codec.encode(JSON.stringify({ id, ...request })))
        })
    }

    // Binds the connection to `token`, resolving once the server has accepted
    // it. Cheap to repeat: naming the credential the live socket is already
    // bound with joins that bind instead of issuing a second one — which is
    // what a boot does, the session store binding on token receipt and the
    // bootstrap then awaiting the same identity.
    async authenticate(token: string): Promise<void> {
        this.#token = token
        await this.#bind(token)
    }

    async deauthenticate(): Promise<void> {
        this.#token = null
        // The socket is no longer bound to anyone, so a later authenticate()
        // with that same credential must issue a real bind, not join the one
        // this call is undoing.
        this.#binding = undefined
        await this.#auth?.deauthenticate(payload => this.send(payload))
    }

    #bind(token: string): Promise<unknown> {
        if (this.#binding?.token !== token) {
            // send() waits for the socket itself, so this is safe to issue
            // while the handshake is still in flight: the frame goes out when
            // it opens, by which point the 'open' handler finds this record
            // already standing and adds nothing.
            this.#binding = { token, done: this.#auth?.authenticate(payload => this.send(payload), token) ?? Promise.resolve() }
        }
        return this.#binding.done
    }
}
