import type { Server, WebSocketHandler } from 'bun'
import { unwrapArguments } from './arguments'
import type { Dispatcher } from './dispatcher'
import { clientIp } from './http'
import type { Connection, ConnectionData, ConnRegistry } from './registry'
import { ANONYMOUS } from './types'
import type { AuthInfo, IncomingRequest, RpcResponse } from './types'

export interface WebSocketHandlerOptions {
    // Run once the socket can be written to. A service that greets a new
    // connection with its current state does it here.
    onOpen?: (conn: Connection) => void
    // Run when the socket has gone, before it leaves the registry.
    onClose?: (conn: Connection) => void
}

export interface RpcWebSocket {
    // Resolves the handshake credential and hands the socket to Bun. False
    // when Bun refused the upgrade, which is the fetch handler's cue to answer
    // the request itself.
    upgrade(request: Request, server: Server<ConnectionData>): Promise<boolean>
    // Pass to `Bun.serve`'s `websocket`.
    handlers: WebSocketHandler<ConnectionData>
}

// The credential a socket presents at the handshake, as a query parameter —
// the only channel a browser's WebSocket constructor leaves open, having no
// headers. Reconnects carry it again for free, which is why a dashboard with a
// static key needs no in-band handshake at all.
const TOKEN_PARAM = 'token'

// The WebSocket transport: many calls over one socket, correlated by id, plus
// the pushes the server sends unasked. Requests dispatch concurrently — a slow
// one must not hold up the socket — with one exception, below.
export const websocketHandler = (dispatcher: Dispatcher, registry: ConnRegistry, options: WebSocketHandlerOptions = {}): RpcWebSocket => {
    const reply = (conn: Connection, id: string | undefined, response: RpcResponse): void =>
        registry.send(conn, {
            type: 'response',
            id,
            ...response
        })

    // Who the connection is from here on: the index follows, and the frame that
    // asked is answered once it does.
    const bindAs = (conn: Connection, request: IncomingRequest, auth: AuthInfo): void => {
        conn.data.auth = auth
        registry.register(conn)
        reply(conn, request.id, { success: true })
    }

    const authenticate = async (conn: Connection, request: IncomingRequest): Promise<void> => {
        if (!dispatcher.auth) {
            reply(conn, request.id, { success: false, error: 'AUTH_NOT_CONFIGURED' })
            return
        }
        const token = unwrapArguments(request.arguments)
        if (typeof token !== 'string') {
            reply(conn, request.id, { success: false, error: 'INVALID_ARGUMENT' })
            return
        }
        const auth = await dispatcher.auth.resolveToken(token, conn.data.controller.signal)
        if (!auth || auth.userId === null) {
            reply(conn, request.id, { success: false, error: 'UNAUTHENTICATED' })
            return
        }
        bindAs(conn, request, auth)
    }

    const logout = (conn: Connection, request: IncomingRequest): void => bindAs(conn, request, ANONYMOUS)

    // The frames that change who a connection is, rather than asking it to do
    // something. @greendrake/rpc's WsTransport sends exactly these. A Map, so a
    // client naming an inherited property does not reach a function.
    const authFrames = new Map<string, (conn: Connection, request: IncomingRequest) => void | Promise<void>>([
        ['auth.token', authenticate],
        ['auth.logout', logout]
    ])

    return {
        async upgrade(request, server) {
            const token = new URL(request.url).searchParams.get(TOKEN_PARAM)
            const resolved = token && dispatcher.auth ? await dispatcher.auth.resolveToken(token, request.signal) : null
            const data: ConnectionData = {
                auth: resolved ?? ANONYMOUS,
                controller: new AbortController(),
                clientIp: clientIp(request, server),
                gate: Promise.resolve()
            }
            return server.upgrade(request, { data })
        },
        handlers: {
            open(conn) {
                registry.register(conn)
                options.onOpen?.(conn)
            },
            message(conn, raw) {
                let request: IncomingRequest
                try {
                    request = JSON.parse(registry.codec.decode(String(raw))) as IncomingRequest
                } catch {
                    reply(conn, undefined, { success: false, error: 'INVALID_JSON' })
                    return
                }
                const bind = authFrames.get(request.method)
                if (bind) {
                    // An auth frame changes who every later call on this socket
                    // is made by, so it goes on the gate the calls wait behind:
                    // a request sent right after one must not race it and come
                    // back UNAUTHENTICATED. The chain is kept fulfilled — a
                    // rejected gate would silently strand every frame behind it.
                    conn.data.gate = conn.data.gate
                        .then(() => bind(conn, request))
                        .catch((e: unknown) => {
                            console.error(`[rpc-server] ${request.method}:`, e)
                            reply(conn, request.id, { success: false, error: 'INTERNAL_ERROR' })
                        })
                    return
                }
                void conn.data.gate
                    .then(() =>
                        dispatcher.dispatch(request, {
                            auth: conn.data.auth,
                            clientIp: conn.data.clientIp,
                            signal: conn.data.controller.signal
                        })
                    )
                    .then(response => {
                        // The socket went while the handler ran: the response
                        // has nowhere to go, and Bun would throw writing it.
                        if (!conn.data.controller.signal.aborted) {
                            reply(conn, request.id, response)
                        }
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
