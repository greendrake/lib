import type { Server, WebSocketHandler } from 'bun'
import { AUTH_LOGOUT_METHOD, AUTH_TOKEN_METHOD, TOKEN_PARAM } from '@greendrake/rpc'
import { admitsOrigin, type OriginPolicy } from '@greendrake/util'
import { unwrapArguments } from './arguments'
import { failure, type Dispatcher } from './dispatcher'
import { peerAddress } from './http'
import type { Connection, ConnectionData, ConnRegistry } from './registry'
import type { IncomingFrame, IncomingRequest, RpcResponse } from './types'

export interface WebSocketHandlerOptions<P> {
    // Run once the socket can be written to, after the registry has greeted it
    // with every document.
    onOpen?: (conn: Connection<P>) => void
    // Run when the socket has gone, before it leaves the registry.
    onClose?: (conn: Connection<P>) => void
    // The browser origins a socket is admitted from beside the endpoint's own,
    // which always is. A browser page anywhere can open a socket to any host,
    // so without this any page its user visits could drive the service as
    // them. A client sending no Origin is no browser, and is not asked.
    origins?: OriginPolicy
    // The handshake's query parameter: TOKEN_PARAM unless given.
    tokenParam?: string
    // The methods that bind the socket to a token's principal and unbind it:
    // the wire's reserved ones unless given.
    authFrames?: { token: string; logout: string }
}

export interface RpcWebSocket<P> {
    // Resolves the handshake credential and hands the socket to Bun. Undefined
    // once it has — what a `Bun.serve` fetch answers for an upgraded request —
    // and otherwise the refusal to answer with: 403 for an origin not
    // admitted, 400 for a request Bun would not upgrade.
    upgrade(request: Request, server: Server<ConnectionData<P>>): Promise<Response | undefined>
    // Pass to `Bun.serve`'s `websocket`.
    handlers: WebSocketHandler<ConnectionData<P>>
}

// Whether a browser's handshake comes from a page of the endpoint's own
// origin: the one its Host names, as the browser sent it.
const sameOrigin = (origin: string, request: Request): boolean => URL.canParse(origin) && new URL(origin).host === request.headers.get('host')?.toLowerCase()

// The WebSocket transport: many calls over one socket, correlated by id, plus
// the pushes the server sends unasked. Requests dispatch concurrently — a slow
// one must not hold up the socket — with one exception, below.
export const websocketHandler = <P>(dispatcher: Dispatcher<P>, registry: ConnRegistry<P>, options: WebSocketHandlerOptions<P> = {}): RpcWebSocket<P> => {
    const tokenParam = options.tokenParam ?? TOKEN_PARAM
    const origins = options.origins ?? []

    const reply = (conn: Connection<P>, id: string | undefined, response: RpcResponse): void =>
        registry.send(conn, {
            type: 'response',
            id,
            ...response
        })

    // Who the connection is from here on: the index follows, and the frame that
    // asked is answered once it does.
    const bindAs = (conn: Connection<P>, request: IncomingRequest, principal: P | null): void => {
        conn.data.principal = principal
        registry.register(conn)
        reply(conn, request.id, { success: true })
    }

    const authenticate = async (conn: Connection<P>, request: IncomingRequest): Promise<void> => {
        if (!dispatcher.auth) {
            reply(conn, request.id, failure('AUTH_NOT_CONFIGURED'))
            return
        }
        const token = unwrapArguments(request.arguments)
        if (typeof token !== 'string') {
            reply(conn, request.id, failure('INVALID_ARGUMENT'))
            return
        }
        const principal = await dispatcher.auth.resolveToken(token, conn.data.controller.signal)
        if (principal === null) {
            reply(conn, request.id, failure('UNAUTHENTICATED'))
            return
        }
        bindAs(conn, request, principal)
    }

    const logout = (conn: Connection<P>, request: IncomingRequest): void => bindAs(conn, request, null)

    // The frames that change who a connection is, rather than asking it to do
    // something. A Map, so a client naming an inherited property does not
    // reach a function.
    const { token: tokenMethod, logout: logoutMethod } = options.authFrames ?? { token: AUTH_TOKEN_METHOD, logout: AUTH_LOGOUT_METHOD }
    const authFrames = new Map<string, (conn: Connection<P>, request: IncomingRequest) => void | Promise<void>>([
        [tokenMethod, authenticate],
        [logoutMethod, logout]
    ])

    // One call off the socket: run behind the gate, under a signal its caller
    // can abort by nonce, and answered unless that caller has gone or aborted it.
    const call = (conn: Connection<P>, request: IncomingRequest): void => {
        const own = new AbortController()
        const { nonce } = request
        if (nonce) {
            conn.data.abortable.set(nonce, own)
        }
        const signal = AbortSignal.any([conn.data.controller.signal, own.signal])
        void conn.data.gate
            .then(() =>
                dispatcher.dispatch(request, {
                    principal: conn.data.principal,
                    clientIp: conn.data.clientIp,
                    signal,
                    topics: {
                        join: topic => registry.join(conn, topic),
                        leave: topic => registry.leave(conn, topic)
                    }
                })
            )
            .then(response => {
                if (nonce && conn.data.abortable.get(nonce) === own) {
                    conn.data.abortable.delete(nonce)
                }
                // Aborted, nobody wants the answer; and when the socket went
                // while the handler ran, Bun would throw writing it.
                if (!signal.aborted) {
                    reply(conn, request.id, response)
                }
            })
    }

    return {
        async upgrade(request, server) {
            const origin = request.headers.get('origin')
            if (origin !== null && !sameOrigin(origin, request) && !admitsOrigin(origins, origin)) {
                return new Response(null, { status: 403 })
            }
            const token = new URL(request.url).searchParams.get(tokenParam)
            const data: ConnectionData<P> = {
                principal: token && dispatcher.auth ? await dispatcher.auth.resolveToken(token, request.signal) : null,
                controller: new AbortController(),
                clientIp: dispatcher.clientIp(request, peerAddress(request, server)),
                gate: Promise.resolve(),
                abortable: new Map()
            }
            return server.upgrade(request, { data }) ? undefined : new Response(null, { status: 400 })
        },
        handlers: {
            // A socket whose client has stopped reading, once its send buffer
            // passes Bun's backpressure limit, is closed rather than having its
            // frames dropped: a frame lost would leave the client silently wrong,
            // and a closed socket reconnects to be greeted afresh.
            closeOnBackpressureLimit: true,
            open(conn) {
                registry.register(conn)
                options.onOpen?.(conn)
            },
            message(conn, raw) {
                let frame: IncomingFrame
                try {
                    frame = JSON.parse(registry.codec.decode(String(raw))) as IncomingFrame
                } catch {
                    reply(conn, undefined, failure('INVALID_JSON'))
                    return
                }
                // First, so a frame that aborts one request and makes the next
                // — a search typed on a debounce — cancels before it starts.
                if (frame.abort !== undefined) {
                    conn.data.abortable.get(frame.abort)?.abort()
                }
                const { method } = frame
                // An abort and nothing else, which is answered with nothing.
                if (method === undefined) {
                    return
                }
                const request: IncomingRequest = { ...frame, method }
                const bind = authFrames.get(method)
                if (!bind) {
                    call(conn, request)
                    return
                }
                // An auth frame changes who every later call on this socket is
                // made by, so it goes on the gate the calls wait behind: a
                // request sent right after one must not race it and come back
                // UNAUTHENTICATED. The chain is kept fulfilled — a rejected gate
                // would silently strand every frame behind it.
                conn.data.gate = conn.data.gate
                    .then(() => bind(conn, request))
                    .catch((e: unknown) => {
                        dispatcher.onFault(method, e)
                        reply(conn, request.id, failure('INTERNAL_ERROR'))
                    })
            },
            close(conn) {
                // Every handler still running on this socket learns its caller
                // is gone, and abandons whatever I/O it is waiting on.
                conn.data.controller.abort()
                options.onClose?.(conn)
                registry.unregister(conn)
            }
        }
    }
}
