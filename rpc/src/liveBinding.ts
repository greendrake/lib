import { coalesced } from '@greendrake/util'
import type { PushMapConstraint, WsTransport } from './WsTransport'

// What one push means to the thing the binding keeps current.
export interface LiveHandler<D> {
    // Whether this push concerns what the binding shows. Omitted: every push does.
    when?: (data: D) => boolean
    // Folds the push in locally. Omitted: the push triggers `resync`.
    apply?: (data: D) => void
}

export interface LiveOptions<PEvents, PData> {
    // `{type:'event', event}` frames, subscribed via onEvent.
    events?: { [K in keyof PEvents]?: LiveHandler<PEvents[K]> }
    // Bare `{type, data}` frames, subscribed via onData.
    data?: { [K in keyof PData]?: LiveHandler<PData[K]> }
    // The full re-read of what the binding shows. Runs for a relevant push
    // with no `apply`, and when the socket comes back from a drop — the two
    // are the same event as far as the data is concerned, since a drop is
    // exactly where a push goes missing.
    //
    // It owns its own errors: the binding awaits it only to know when the slot
    // is free, and interprets neither outcome. It must also not be served by a
    // response cache the missed pushes would have invalidated — invalidate,
    // then read.
    resync?: () => Promise<unknown> | void
}

// Binds a view, store or cache to the server pushes that say it changed: each
// push is filtered for relevance and then either folded in locally (`apply`)
// or answered by re-reading (`resync`), and a socket that drops and returns
// re-reads too — a connection has no memory, so whatever it missed is only
// recoverable by asking again.
//
// Returns the release function; call it when what the binding keeps current
// goes away.
export const liveBinding = <PEvents extends PushMapConstraint<PEvents>, PData extends PushMapConstraint<PData>>(transport: WsTransport<PEvents, PData>, options: LiveOptions<PEvents, PData>): (() => void) => {
    const { resync } = options
    const releases: (() => void)[] = []
    let released = false

    // One re-read at a time, and at most one more behind it however many
    // pushes land while it runs: they all ask the same question, and the answer
    // to the last one is the answer to all of them. One still queued when the
    // binding is released has nobody left to answer, and does nothing.
    const reread = coalesced(() => (released ? undefined : resync?.()))
    // The outcome belongs to resync — swallowed here rather than left
    // unhandled because this runs from a subscription callback nobody awaits.
    const runResync = (): void => {
        void reread().catch(() => {})
    }

    const attach = <P>(handlers: { [K in keyof P]?: LiveHandler<P[K]> } | undefined, subscribe: <K extends keyof P & string>(key: K, handler: (data: P[K]) => void) => () => void): (() => () => void)[] =>
        (Object.keys(handlers ?? {}) as (keyof P & string)[]).map(key => {
            const handler = handlers![key]!
            if (!handler.apply && !resync) {
                throw new Error(`liveBinding: the handler for '${key}' neither applies the push nor has a resync to answer it with`)
            }
            return () =>
                subscribe(key, data => {
                    if (handler.when && !handler.when(data)) return
                    if (handler.apply) handler.apply(data)
                    else runResync()
                })
        })

    // Every handler is checked before the first subscription goes on, so a
    // misconfigured one throws with nothing left attached behind it.
    const subscriptions = [...attach<PEvents>(options.events, (event, handler) => transport.onEvent(event, handler)), ...attach<PData>(options.data, (type, handler) => transport.onData(type, handler))]
    subscriptions.forEach(subscribe => releases.push(subscribe()))

    if (resync) {
        // A return from a drop this binding witnessed, not any connect: the
        // first `true` after a boot reports the connection the binding was
        // created for, and there is nothing yet to have missed.
        let dropped = false
        releases.push(
            transport.onConnectionChange(connected => {
                if (!connected) {
                    dropped = true
                    return
                }
                if (!dropped) return
                dropped = false
                runResync()
            })
        )
    }

    return () => {
        released = true
        releases.forEach(release => release())
        releases.length = 0
    }
}
