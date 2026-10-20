import type { ServerWebSocket } from 'bun'
import { documentOf, identityCodec, mergePatchBetween, type JsonObject } from '@greendrake/rpc'
import type { Codec, EventFrame, ServerFrame } from './types'

// Per-socket state, carried as Bun's websocket `data`.
export interface ConnectionData<P = never> {
    // Who the socket is bound as: the query-parameter credential it opened
    // with, or whatever an in-band auth frame has since made it.
    principal: P | null
    // Aborted when the socket closes, so every handler still running on it
    // learns its caller is gone.
    controller: AbortController
    clientIp: string
    // Serialises the frames that change who the connection is against the
    // calls that depend on the answer. See the WebSocket handler.
    gate: Promise<unknown>
    // The calls running on the socket that an abort can name, by nonce.
    abortable: Map<string, AbortController>
}

export type Connection<P = never> = ServerWebSocket<ConnectionData<P>>

// What a subsystem needs in order to tell clients something happened, without
// knowing what a connection is. Push-producing code takes this, not the
// registry. A user is named by the key the registry files principals under.
export interface EventSink {
    emitToUser(userKey: unknown, event: string, data: unknown): void
    broadcastEvent(event: string, data: unknown): void
}

// What the sockets of one principal are filed under, for emitToUser to reach
// them all. A primitive principal (a user id) is its own key. An object one is
// a fresh object for each socket its credential is resolved on, so it names the
// key its sockets share, and the type insists.
type UserKeyOption<P> = [P] extends [string | number | bigint | boolean | symbol] ? { userKey?: (principal: P) => unknown } : { userKey: (principal: P) => unknown }

export type ConnRegistryOptions<P> = UserKeyOption<P> & {
    // Governs every frame on the socket, in both directions.
    codec?: Codec
}

type ConnRegistryArgs<P> = object extends ConnRegistryOptions<P> ? [options?: ConnRegistryOptions<P>] : [options: ConnRegistryOptions<P>]

// What a producer holds of its document: publishing it is the producer's whole part.
export interface Document {
    // Publishes the state as `produce` now has it: what changed since it was last
    // published goes to every socket keeping the document as one patch, and
    // nothing goes when nothing changed. So a producer calls it on any change it
    // knows of; a call too many costs a diff.
    refresh(): void
}

// Where a document is kept: by the members of the topic, rather than by every
// socket.
export interface DocumentOptions {
    topic: string
}

// What a subsystem needs in order to keep a document on sockets, without
// knowing what a connection is: on every socket, or on the members of a topic,
// whose audience the handle is then too. Document-producing code takes this,
// not the registry. See ConnRegistry's document().
export interface DocumentPublisher {
    document(type: string, produce: () => object): Document
    document(type: string, produce: () => object, options: DocumentOptions): Document & Audience
}

// Whether anybody is there to hear a broadcast, for a producer whose pushes
// cost something to produce — a reading taken from somewhere slow — and are
// worth producing only while somebody is there to be told: the open sockets,
// for the registry; a topic's members, for a document kept by a topic.
export interface Audience {
    // How many there are.
    readonly size: number
    // Called with true as the first arrives — a socket opening, or joining the
    // topic — and false as the last goes — closing, or leaving. The function
    // returned stops the calls.
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

// A document the registry publishes: what it is called on the wire, the state as
// last published, which is what a socket starting to keep it is sent, and the
// topic whose members keep it — every socket, where null.
interface Published {
    type: string
    doc: JsonObject
    topic: string | null
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
// And the documents sockets keep, every socket or a topic's members (see
// document()), and who is listening for a topic's first member and last.
export class ConnRegistry<P = never> implements EventSink, Audience, DocumentPublisher {
    readonly #connections = new Set<Connection<P>>()
    readonly #documents: Published[] = []
    readonly #presence = new Set<(present: boolean) => void>()
    readonly #topicPresence = new Map<string, Set<(present: boolean) => void>>()
    readonly #byUser = new Map<unknown, Set<Connection<P>>>()
    // Where each connection is currently filed, so a principal that changes
    // mid-connection can be re-indexed without the caller tracking what it
    // used to be.
    readonly #filedUnder = new Map<Connection<P>, unknown>()
    // A topic's sockets, and a socket's topics: the second so a closing socket
    // leaves every topic it is in without a search through all of them.
    readonly #topicMembers = new Map<string, Set<Connection<P>>>()
    readonly #joined = new Map<Connection<P>, Set<string>>()
    readonly #userKey: (principal: P) => unknown
    // Public because the WebSocket handler decodes inbound frames with it:
    // one codec governs the socket in both directions, and it is configured
    // here, once.
    readonly codec: Codec

    constructor(...[options]: ConnRegistryArgs<P>) {
        this.codec = options?.codec ?? identityCodec
        this.#userKey = options?.userKey ?? (principal => principal)
    }

    // Idempotent, and the one way a connection's principal reaches the index:
    // called when a socket opens and again whenever an auth frame changes who
    // it is.
    register(conn: Connection<P>): void {
        const arriving = !this.#connections.has(conn)
        this.#connections.add(conn)
        if (arriving) {
            this.#greetWithKept(conn, null)
        }
        if (arriving && this.#connections.size === 1) {
            this.#presence.forEach(listener => listener(true))
        }
        const { principal } = conn.data
        const key = principal === null ? null : this.#userKey(principal)
        if (this.#filedUnder.get(conn) === key) {
            return
        }
        this.#unfile(conn)
        if (key === null) {
            return
        }
        addTo(this.#byUser, key, conn)
        this.#filedUnder.set(conn, key)
    }

    unregister(conn: Connection<P>): void {
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

    // Puts the socket in the topic, for publish to reach, and greets it with
    // every document the topic's members keep — ahead of the joining call's
    // response, sends being synchronous. A socket already in the topic is not
    // greeted again. A socket that has already closed joins nothing: a handler
    // can outlive its caller, and a membership taken after the close would be a
    // dead socket published to for as long as the process runs.
    join(conn: Connection<P>, topic: string): void {
        if (!this.#connections.has(conn) || this.#topicMembers.get(topic)?.has(conn)) {
            return
        }
        addTo(this.#topicMembers, topic, conn)
        addTo(this.#joined, conn, topic)
        this.#greetWithKept(conn, topic)
        if (this.#topicMembers.get(topic)?.size === 1) {
            this.#topicPresence.get(topic)?.forEach(listener => listener(true))
        }
    }

    leave(conn: Connection<P>, topic: string): void {
        if (!this.#topicMembers.get(topic)?.has(conn)) {
            return
        }
        removeFrom(this.#topicMembers, topic, conn)
        removeFrom(this.#joined, conn, topic)
        if (!this.#topicMembers.has(topic)) {
            this.#topicPresence.get(topic)?.forEach(listener => listener(false))
        }
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

    #unfile(conn: Connection<P>): void {
        if (!this.#filedUnder.has(conn)) {
            return
        }
        const key = this.#filedUnder.get(conn)
        this.#filedUnder.delete(conn)
        removeFrom(this.#byUser, key, conn)
    }

    // The one place a frame becomes bytes: responses and pushes leave a socket
    // the same way, so a codec is configured once rather than per call site.
    send(conn: Connection<P>, frame: ServerFrame): void {
        conn.send(this.codec.encode(JSON.stringify(frame)))
    }

    sendToUser(userKey: unknown, frame: ServerFrame): void {
        this.#byUser.get(userKey)?.forEach(conn => this.send(conn, frame))
    }

    emitToUser(userKey: unknown, event: string, data: unknown): void {
        this.sendToUser(userKey, eventFrame(event, data))
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
    // socket, or with `topic` by the topic's members alone. A socket is sent the
    // document as last published when it starts keeping it — as it opens, or as
    // it joins the topic — `{type, data: {doc}}`, and each change after that as
    // an RFC 7386 merge patch over it, `{type, data: {patch}}` — objects merge,
    // null removes a member, anything else replaces — so what it holds is always
    // the document. A socket that leaves the topic, or closes, is sent nothing
    // further. A member produced as null is dropped, the same as one left out, a
    // patch being unable to tell the two apart (`@greendrake/rpc`'s documentOf,
    // which the client end shares). `produce` runs now, so every greeting has a
    // document, and again on each refresh. A state that is no JSON object
    // throws. Sends are synchronous, so no patch can precede or overtake a
    // socket's document. A topic's document is also that topic's Audience: its
    // members, and the first to join and the last to leave or close.
    document(type: string, produce: () => object): Document
    document(type: string, produce: () => object, options: DocumentOptions): Document & Audience
    document(type: string, produce: () => object, options?: DocumentOptions): Document | (Document & Audience) {
        const topic = options?.topic ?? null
        const published: Published = {
            type,
            doc: documentOf(type, produce()),
            topic
        }
        this.#documents.push(published)
        // The sockets keeping it, as they are now.
        const keepers = (): ReadonlySet<Connection<P>> => (topic === null ? this.#connections : (this.#topicMembers.get(topic) ?? new Set()))
        for (const conn of keepers()) {
            this.#greet(conn, published)
        }
        const refresh = (): void => {
            const next = documentOf(type, produce())
            const patch = mergePatchBetween(published.doc, next)
            if (!patch) {
                return
            }
            published.doc = next
            for (const conn of keepers()) {
                this.send(conn, { type, data: { patch } })
            }
        }
        if (topic === null) {
            return { refresh }
        }
        return {
            refresh,
            get size() {
                return keepers().size
            },
            onPresence: listener => {
                addTo(this.#topicPresence, topic, listener)
                return () => removeFrom(this.#topicPresence, topic, listener)
            }
        }
    }

    #greet(conn: Connection<P>, published: Published): void {
        this.send(conn, { type: published.type, data: { doc: published.doc } })
    }

    // Greets a socket starting to keep the documents of a topic — or, with
    // null, those every socket keeps — with each of them.
    #greetWithKept(conn: Connection<P>, topic: string | null): void {
        this.#documents.filter(published => published.topic === topic).forEach(published => this.#greet(conn, published))
    }

    get size(): number {
        return this.#connections.size
    }
}
