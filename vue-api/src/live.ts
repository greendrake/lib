import { onUnmounted } from 'vue'
import { NetworkError } from '@greendrake/util'
import { NotFoundError, liveBinding } from '@greendrake/rpc'
import type { LiveOptions, PushMapConstraint, WsTransport } from '@greendrake/rpc'
import type { CallOptions } from './ApiClient'
import { getErrorSink } from './uxConfig'

// The call options a background read runs under, in one place so every resync
// and every background loader means the same thing by "background". `silent`
// is the option that matters: it bypasses the loading counter AND the
// error-UX pipeline, whereas `pending: false` suppresses only the spinner and
// would still put the blocking retry toast over a screen the user is reading.
export const backgroundCall = (background: boolean): CallOptions | undefined => (background ? { silent: true } : undefined)

// @greendrake/rpc's liveBinding under this package's UX policy: a resync runs
// unseen, and its failure neither interrupts the user nor vanishes.
//
// A resync must read in the background (see backgroundCall) and must not be
// served by a response cache the missed pushes would have invalidated —
// invalidate the entries its read would hit, then read, so the stale entry is
// repaired for the next reader rather than merely stepped around.
export const bindLive = <PEvents extends PushMapConstraint<PEvents>, PData extends PushMapConstraint<PData>>(transport: WsTransport<PEvents, PData>, options: LiveOptions<PEvents, PData>): (() => void) => {
    const { resync } = options
    if (!resync) return liveBinding(transport, options)
    return liveBinding(transport, {
        ...options,
        resync: async () => {
            try {
                await resync()
            } catch (e) {
                // Gone while the socket was down: there is nothing to show and
                // nothing to report — the next navigation says so.
                if (e instanceof NotFoundError) return
                // The socket went again mid-read (WebSocketClosedError is a
                // NetworkError). The binding's own reconnect rule owns this
                // one: it re-reads when the socket returns, so reporting it
                // would file one entry per binding per blip.
                if (e instanceof NetworkError) return
                getErrorSink()?.(e as Error, { resync: true })
            }
        }
    })
}

// Component-scoped live binding: releases with the component. The transport is
// passed explicitly — an HTTP-only app has none, which is a compile-time error
// here rather than a silent no-op.
export const useLive = <PEvents extends PushMapConstraint<PEvents>, PData extends PushMapConstraint<PData>>(transport: WsTransport<PEvents, PData>, options: LiveOptions<PEvents, PData>): void => {
    onUnmounted(bindLive(transport, options))
}
