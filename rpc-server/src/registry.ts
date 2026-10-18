import type { ServerWebSocket } from 'bun'
import { documentOf, identityCodec, mergePatchBetween, type JsonObject } from '@greendrake/rpc'
import type { AuthInfo, Codec, EventFrame, ServerFrame } from './types'

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

// What a producer holds of its document: publishing it is the producer's whole part.
export interface Document {
    // Publishes the state as `produce` now has it: what changed since it was last
    // published goes to every socket as one patch, and nothing goes when nothing
    // changed. So a producer calls it on any change it knows of; a call too many
    // costs a diff.
    refresh(): void
}

// Whether anybody is connected to hear a broadcast, for a producer whose
// pushes cost something to produce — a reading taken from somewhere slow —
// and are worth producing only while somebody is there to be told.
export interface Audience {
    // How many sockets are open.
    readonly size: number
    // Called with true as the first socket opens and false as the last one
    // closes. The function returned stops the calls.
    onPresence(listener: (present: boolean) => void): () => void
}

// One entry of a one-to-many index. A key is present exactly while something is
// under it, so an index never holds a set for a key with nothing left in it.
const addTo = <K, V>(index: Map<K, Set<V>>, key: K, value: V): void => {
    const under = index.get(key)
    if (under) {
        under.add(value)
    } else {
        index.set(key, new Set([value]))
    }
}

const removeFrom = <K, V>(index: Map<K, Set<V>>, key: K, value: V): void => {
    const under = index.get(key)
    if (!under) {
        return
    }
    under.delete(value)
    if (under.size === 0) {
        index.delete(key)
    }
}

const eventFrame = (event: string, data: unknown): EventFrame => ({
    type: 'event',
    event,
    data
})

// A document the registry publishes: what it is called on the wire, and the state as
// last published, which is what an opening socket is sent.
interface Published {
    type: string
    doc: JsonObject
}

// Every open socket, and the index from principal to the sockets bound as it —
// a user with three tabs has three. Holding all of them as well as the index
// is what lets a broadcast reach anonymous connections too, which a per-user
// index alone cannot.
//
// And the topics sockets have joined: subscriptions a method takes out for its
// caller (see Topics), published to by the application and dropped with the
// socket.
//
// And the documents every socket keeps (see document()).
export class ConnRegistry implements EventSink, Audience {
    readonly #connections = new Set<Connection>()
    readonly #documents: Published[] = []
    readonly #presence = new Set<(present: boolean) => void>()
    readonly #byUser = new Map<unknown, Set<Connection>>()
    // Where each connection is currently filed, so a principal that changes
    // mid-connection can be re-indexed without the caller tracking what it
    // used to be.
    readonly #filedUnder = new Map<Connection, unknown>()
    // A topic's sockets, and a socket's topics: the second so a closing socket
    // leaves every topic it is in without a search through all of them.
    readonly #topicMembers = new Map<string, Set<Connection>>()
    readonly #joined = new Map<Connection, Set<string>>()
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
        const arriving = !this.#connections.has(conn)
        this.#connections.add(conn)
        if (arriving) {
            this.#documents.forEach(published => this.#greet(conn, published))
        }
        if (arriving && this.#connections.size === 1) {
            this.#presence.forEach(listener => listener(true))
        }
        const userId = conn.data.auth.userId
        if (this.#filedUnder.get(conn) === userId) {
            return
        }
        this.#unfile(conn)
        if (userId === null) {
            return
        }
        addTo(this.#byUser, userId, conn)
        this.#filedUnder.set(conn, userId)
    }

    unregister(conn: Connection): void {
        const leaving = this.#connections.delete(conn)
        this.#unfile(conn)
        // A copy: leaving a topic takes it out of the set being walked.
        for (const topic of [...(this.#joined.get(conn) ?? [])]) {
            this.leave(conn, topic)
        }
        if (leaving && this.#connections.size === 0) {
            this.#presence.forEach(listener => listener(false))
        }
    }

    // Puts the socket in the topic, for publish to reach. A socket that has
    // already closed joins nothing: a handler can outlive its caller, and a
    // membership taken after the close would be a dead socket published to for
    // as long as the process runs.
    join(conn: Connection, topic: string): void {
        if (!this.#connections.has(conn)) {
            return
        }
        addTo(this.#topicMembers, topic, conn)
        addTo(this.#joined, conn, topic)
    }

    leave(conn: Connection, topic: string): void {
        removeFrom(this.#topicMembers, topic, conn)
        removeFrom(this.#joined, conn, topic)
    }

    // To every socket in the topic, whoever it is bound as — for state only the
    // sockets that asked for it want.
    publish(topic: string, event: string, data: unknown): void {
        this.#topicMembers.get(topic)?.forEach(conn => this.send(conn, eventFrame(event, data)))
    }

    // The topics with at least one socket in them, as they are now.
    topics(): ReadonlySet<string> {
        return new Set(this.#topicMembers.keys())
    }

    onPresence(listener: (present: boolean) => void): () => void {
        this.#presence.add(listener)
        return () => {
            this.#presence.delete(listener)
        }
    }

    #unfile(conn: Connection): void {
        if (!this.#filedUnder.has(conn)) {
            return
        }
        const userId = this.#filedUnder.get(conn)
        this.#filedUnder.delete(conn)
        removeFrom(this.#byUser, userId, conn)
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
        this.sendToUser(userId, eventFrame(event, data))
    }

    // To every open socket, whoever it is bound as — for state the whole
    // service shares rather than anything addressed to a user.
    broadcastEvent(event: string, data: unknown): void {
        this.#connections.forEach(conn => this.send(conn, eventFrame(event, data)))
    }

    // The bare `{type, data}` form, for a push whose own type names the
    // payload and needs no event envelope.
    broadcastData(type: string, data: unknown): void {
        this.#connections.forEach(conn => this.send(conn, { type, data }))
    }

    // A state stream: the whole of some state as one JSON object, kept by every
    // socket. A socket is sent the document as last published when it opens,
    // `{type, data: {doc}}`, and each change after that as an RFC 7386 merge
    // patch over it, `{type, data: {patch}}` — objects merge, null removes a
    // member, anything else replaces — so what it holds is always the document.
    // A member produced as null is dropped, the same as one left out, a patch
    // being unable to tell the two apart (`@greendrake/rpc`'s documentOf, which
    // the client end shares). `produce` runs now, so every greeting has a
    // document, and again on each refresh. A state that is no JSON object
    // throws. Sends are synchronous, so no patch can precede or overtake a
    // socket's document.
    document(type: string, produce: () => object): Document {
        const published: Published = { type, doc: documentOf(type, produce()) }
        this.#documents.push(published)
        this.#connections.forEach(conn => this.#greet(conn, published))
        return {
            refresh: () => {
                const next = documentOf(type, produce())
                const patch = mergePatchBetween(published.doc, next)
                if (!patch) {
                    return
                }
                published.doc = next
                this.#connections.forEach(conn => this.send(conn, { type, data: { patch } }))
            }
        }
    }

    #greet(conn: Connection, published: Published): void {
        this.send(conn, { type: published.type, data: { doc: published.doc } })
    }

    get size(): number {
        return this.#connections.size
    }
}
