import type { WebSocketPipe, WsTransport } from '@greendrake/rpc'

// What keeping a socket connected asks of it — @greendrake/rpc's WsTransport
// as-is.
export type Revivable = Pick<WsTransport, 'probe'> & { readonly pipe: Pick<WebSocketPipe, 'reconnectNow'> }

// How often a visible page asks whether its socket is still alive.
const BEAT_MS = 60_000

// Keeps the socket carrying the app's calls connected for as long as the page
// lives; returns the teardown.
//
// The device coming back is a hint, not a verdict — it only collapses a
// waiting reconnect's remaining delay so the socket re-attempts at once
// instead of sitting out a timer that started before connectivity returned.
// A device that reports wrongly (a WKWebView, a captive portal) costs nothing
// here: the attempt either opens the socket or it does not.
//
// A socket can also die without saying so: a backgrounded mobile app, an idle
// NAT or VPN mapping expiring under a tab that never lost focus. Nothing fires
// then — no close, no online, no visibility change — so the only way to learn
// it is to ask, with `method`, the cheapest question the API answers. Silence
// past the transport's deadline abandons the socket and reconnects, which puts
// everything hanging off the reconnect back in charge.
//
// Asked at the two moments it can matter: when the page comes back to the
// user, and — because a visible tab can be sitting on a dead socket just as
// easily — on a slow beat while it stays visible. Nothing runs while hidden: a
// hidden tab has no reader to be stale for, and its timers are throttled
// anyway.
//
// `method` is typed against the app's method map (`keepConnected<Methods>`)
// rather than taken as a bare string: a probe reads any answer as proof of
// life, a refusal included, so a method the server does not have would report
// every socket alive forever. This way a rename is a compile error.
export const keepConnected = <M>(transport: Revivable, method: keyof M & string): (() => void) => {
    let beat: ReturnType<typeof setInterval> | undefined

    const reconnect = (): void => transport.pipe.reconnectNow()

    const probe = (): void => {
        void transport.probe(method)
    }

    const onVisibility = (): void => {
        clearInterval(beat)
        beat = undefined
        if (document.visibilityState !== 'visible') {
            return
        }
        // A socket that DID close while hidden is waiting out a backoff whose
        // timer was throttled with the tab; collapse it rather than make the
        // reader wait.
        reconnect()
        probe()
        beat = setInterval(probe, BEAT_MS)
    }

    addEventListener('online', reconnect)
    addEventListener('visibilitychange', onVisibility)
    onVisibility()

    return () => {
        removeEventListener('online', reconnect)
        removeEventListener('visibilitychange', onVisibility)
        clearInterval(beat)
    }
}
