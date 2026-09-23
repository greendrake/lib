import type { ServerWebSocket } from 'bun'
import { identityCodec } from '@greendrake/rpc'
import type { AuthInfo, Codec, ServerFrame } from './types'

// Per-socket state, carried as Bun's websocket `data`.
export interface ConnectionData {
    // Who the socket is bound as: the query-parameter credential it opened
    // with, or whatever an in-band auth frame has since made it.
    auth: AuthInfo
    // Aborted when the socket closes, so every handler still running on it
    // learns its caller is gone.
    controller: AbortController
    clientIp: string
    // Serialises the frames that change who the connection is against the
    // calls that depend on the answer. See the WebSocket handler.
    gate: Promise<unknown>
}

export type Connection = ServerWebSocket<ConnectionData>

// What a subsystem needs in order to tell clients something happened, without
// knowing what a connection is. Push-producing code takes this, not the
// registry.
export interface EventSink {
    emitToUser(userId: unknown, event: string, data: unknown): void
    broadcastEvent(event: string, data: unknown): void
}

// Every open socket, and the index from principal to the sockets bound as it —
// a user with three tabs has three. Holding all of them as well as the index
// is what lets a broadcast reach anonymous connections too, which a per-user
// index alone cannot.
export class ConnRegistry implements EventSink {
    readonly #connections = new Set<Connection>()
    readonly #byUser = new Map<unknown, Set<Connection>>()
    // Where each connection is currently filed, so a principal that changes
    // mid-connection can be re-indexed without the caller tracking what it
    // used to be.
    readonly #filedUnder = new Map<Connection, unknown>()
    // Public because the WebSocket handler decodes inbound frames with it:
    // one codec governs the socket in both directions, and it is configured
    // here, once.
    readonly codec: Codec

    constructor(codec: Codec = identityCodec) {
        this.codec = codec
    }

    // Idempotent, and the one way a connection's principal reaches the index:
    // called when a socket opens and again whenever an auth frame changes who
    // it is.
    register(conn: Connection): void {
        this.#connections.add(conn)
        const userId = conn.data.auth.userId
        if (this.#filedUnder.get(conn) === userId) {
            return
        }
        this.#unfile(conn)
        if (userId === null) {
            return
        }
        let bound = this.#byUser.get(userId)
        if (!bound) {
            bound = new Set()
            this.#byUser.set(userId, bound)
        }
        bound.add(conn)
        this.#filedUnder.set(conn, userId)
    }

    unregister(conn: Connection): void {
        this.#connections.delete(conn)
        this.#unfile(conn)
    }

    #unfile(conn: Connection): void {
        if (!this.#filedUnder.has(conn)) {
            return
        }
        const userId = this.#filedUnder.get(conn)
        this.#filedUnder.delete(conn)
        const bound = this.#byUser.get(userId)
        if (!bound) {
            return
        }
        bound.delete(conn)
        if (bound.size === 0) {
            this.#byUser.delete(userId)
        }
    }

    // The one place a frame becomes bytes: responses and pushes leave a socket
    // the same way, so a codec is configured once rather than per call site.
    send(conn: Connection, frame: ServerFrame): void {
        conn.send(this.codec.encode(JSON.stringify(frame)))
    }

    sendToUser(userId: unknown, frame: ServerFrame): void {
        this.#byUser.get(userId)?.forEach(conn => this.send(conn, frame))
    }

    emitToUser(userId: unknown, event: string, data: unknown): void {
        this.sendToUser(userId, {
            type: 'event',
            event,
            data
        })
    }

    // To every open socket, whoever it is bound as — for state the whole
    // service shares rather than anything addressed to a user.
    broadcastEvent(event: string, data: unknown): void {
        this.#connections.forEach(conn =>
            this.send(conn, {
                type: 'event',
                event,
                data
            })
        )
    }

    // The bare `{type, data}` form, for a push whose own type names the
    // payload and needs no event envelope.
    broadcastData(type: string, data: unknown): void {
        this.#connections.forEach(conn => this.send(conn, { type, data }))
    }

    get size(): number {
        return this.#connections.size
    }
}
