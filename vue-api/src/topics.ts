import { nextTick, shallowReactive } from 'vue'
import { NetworkError } from '@greendrake/util'
import { liveBinding } from '@greendrake/rpc'
import type { PushMapConstraint, StateFrame, WsTransport } from '@greendrake/rpc'
import { reactiveDocument, type ReactiveDocument } from './live'
import { getErrorSink } from './uxConfig'

// What holding a key takes on the server: the calls joining and leaving its
// topic. They are plain calls over the transport, settling as the wire answers
// — an RpcClient's, never an ApiClient's. An ApiClient call parks across an
// outage and is re-sent on the return, and one lost in flight waits on the
// user's retry: either would re-join a key nobody holds any more, or hold a
// join out indefinitely. The holder owns retrying and reporting instead.
export interface TopicCalls<Key> {
    join(key: Key): Promise<unknown>
    leave(key: Key): Promise<unknown>
}

// holdTopics' options: the calls, and who is told as a key's membership begins
// and ends — as its first join is about to go out, and as its leave is, or
// would be. What a key's topic carries is kept between the two (see
// keepTopicDocuments).
export interface HoldTopicsOptions<Key> extends TopicCalls<Key> {
    opens?(key: Key): void
    closes?(key: Key): void
}

export interface TopicHolder<Key> {
    // Holds the key's membership; returns the release.
    hold(key: Key): () => void
    // Why the key's last join was refused, reactively: while it is held, until
    // a join of it succeeds.
    failure(key: Key): Error | undefined
}

// A key's membership of its topic, and the calls made for it.
interface Membership {
    holders: number
    // From the first hold until the last release is torn down.
    current: boolean
    // A join queued or out, which a rejoin would only repeat.
    joining: boolean
    // Whether the key's last join was granted: only then is there anything on
    // the server to leave.
    joined: boolean
    // The key's calls, one after another: each runs once the one before it has
    // settled, so the server never runs a join and a leave out of order.
    tail: Promise<void>
}

// Holds topic memberships by key over transport, for any topic subscription.
// The first hold of a key joins and the last release of it leaves, the release
// torn down only after the current render flush: a hold arriving before then —
// a control swapped for another of the same key — keeps the membership, and
// nothing is sent. A dropped socket takes its memberships with it, so after
// each return from a drop it witnessed (liveBinding's rule) every held key
// joins again, unless a join of it is still out.
//
// A join failing with a NetworkError is retried at the next return: a failed
// connection attempt reports a drop, so that return always comes. Any other
// failure is the key's `failure`. A leave is decided when its turn comes, and
// goes out only where there is a membership to leave: not where the key's last
// join was refused or lost, and not while the transport is down — a socket that
// has gone took its memberships with it, and sending the leave would only open
// a new socket to leave a topic it was never in. A leave failing with a
// NetworkError is dropped for the same reason, and any other failure goes to
// the error sink, nobody being left to show it.
export const holdTopics = <Key, PEvents extends PushMapConstraint<PEvents>, PData extends PushMapConstraint<PData>>(transport: WsTransport<PEvents, PData>, options: HoldTopicsOptions<Key>): TopicHolder<Key> => {
    const memberships = new Map<Key, Membership>()
    const failures = shallowReactive(new Map<Key, Error>())

    // Runs once every call of the key asked before it has settled. A key left
    // with nothing current and nothing queued is forgotten, so a page holding
    // many keys over its life keeps none it has let go of; a later hold starts
    // it afresh, every call of the old one having settled.
    const after = (key: Key, membership: Membership, step: () => Promise<void>): void => {
        const queued = membership.tail.then(step)
        membership.tail = queued
        void queued.then(() => {
            if (!membership.current && membership.tail === queued && memberships.get(key) === membership) {
                memberships.delete(key)
            }
        })
    }

    // Tells a hook of a membership beginning or ending. The hooks are the
    // caller's, and one that throws is reported rather than allowed to stop the
    // key's calls: the join or leave it came with still goes, and so does every
    // call after it.
    const tell = (hook: ((key: Key) => void) | undefined, key: Key): void => {
        try {
            hook?.(key)
        } catch (e) {
            getErrorSink()?.(e as Error, { key })
        }
    }

    const join = (key: Key, membership: Membership, opening: boolean): void => {
        membership.joining = true
        after(key, membership, async () => {
            if (opening) {
                tell(options.opens, key)
            }
            try {
                await options.join(key)
                membership.joined = true
                failures.delete(key)
            } catch (e) {
                membership.joined = false
                if (!(e instanceof NetworkError)) {
                    failures.set(key, e as Error)
                }
            } finally {
                membership.joining = false
            }
        })
    }

    const leave = (key: Key, membership: Membership): void => {
        after(key, membership, async () => {
            tell(options.closes, key)
            failures.delete(key)
            const member = membership.joined
            membership.joined = false
            if (!member || !transport.connected) {
                return
            }
            try {
                await options.leave(key)
            } catch (e) {
                if (!(e instanceof NetworkError)) {
                    getErrorSink()?.(e as Error, { leave: key })
                }
            }
        })
    }

    liveBinding(transport, {
        resync: () => {
            for (const [key, membership] of memberships) {
                if (membership.current && !membership.joining) {
                    join(key, membership, false)
                }
            }
        }
    })

    return {
        hold: key => {
            let membership = memberships.get(key)
            if (!membership) {
                membership = {
                    holders: 0,
                    current: false,
                    joining: false,
                    joined: false,
                    tail: Promise.resolve()
                }
                memberships.set(key, membership)
            }
            const held = membership
            held.holders += 1
            if (!held.current) {
                held.current = true
                join(key, held, true)
            }
            let released = false
            return () => {
                if (released) {
                    return
                }
                released = true
                held.holders -= 1
                if (held.holders > 0) {
                    return
                }
                void nextTick(() => {
                    if (held.holders === 0 && held.current) {
                        held.current = false
                        leave(key, held)
                    }
                })
            }
        },
        failure: key => failures.get(key)
    }
}

// keepTopicDocuments' options: the calls, the document type of a key's topic,
// and what a held key's document is until it is greeted.
export interface TopicDocumentsOptions<Key, T extends string, D> extends TopicCalls<Key> {
    typeOf(key: Key): T
    empty(): D
}

export interface TopicDocuments<Key, D> extends TopicHolder<Key> {
    // The held key's document, reactively: undefined while it is not held, and
    // while its join stands refused.
    doc(key: Key): D | undefined
}

// Documents kept by topics, one per held key: holdTopics, with the document of
// `typeOf(key)` kept for each membership, from just before the join that
// begins it to the leave that ends it, whether that goes out or is skipped. A
// membership's document starts as `empty()`, after any earlier leave of the key
// has settled, so a patch sent before that leave cannot reach it: the leave's
// answer comes after any patch sent ahead of it on the one socket. A release
// re-held within the flush keeps it; one that ends the membership drops it, so
// a later hold shows `empty()` until it is greeted. While the key's join stands
// refused — a rejoin after a drop, say — nothing more comes, so what the
// document holds is shown as nothing rather than as live.
export const keepTopicDocuments = <Key, T extends string, D extends object, PEvents extends PushMapConstraint<PEvents>, PData extends PushMapConstraint<PData> & Record<T, StateFrame<D>>>(transport: WsTransport<PEvents, PData>, options: TopicDocumentsOptions<Key, T, NoInfer<D>>): TopicDocuments<Key, D> => {
    const kept = shallowReactive(new Map<Key, ReactiveDocument<D>>())
    const holder = holdTopics(transport, {
        join: options.join,
        leave: options.leave,
        opens: key => {
            kept.set(key, reactiveDocument<T, D, PEvents, PData>(transport, options.typeOf(key), options.empty()))
        },
        closes: key => {
            kept.get(key)?.release()
            kept.delete(key)
        }
    })
    return { ...holder, doc: key => (holder.failure(key) ? undefined : kept.get(key)?.doc) }
}
