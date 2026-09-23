import { Emitter } from '@greendrake/util'
import { WebSocketClosedError } from './errors'

export interface WebSocketPipeOptions {
    url: string
    // Connection-level credential appended to the URL as a query parameter and
    // resolved by the server at the handshake (before the first frame). Because
    // the token rides the URL, reconnects re-authenticate automatically. For
    // in-band per-session auth use WsTransport's auth strategy instead.
    queryAuth?: {
        param: string
        token: string
    }
    // Delay before reconnect attempt N (0-based, reset on successful open).
    reconnectDelayMs?: (attempt: number) => number
}

export interface WebSocketPipeEvents {
    open: []
    close: [error: WebSocketClosedError]
    message: [raw: string]
    [key: string]: unknown[]
}

const DEFAULT_RECONNECT_DELAY_MS = 3000

// Reconnecting WebSocket carrier. Owns only the connection lifecycle and raw
// string frames — request framing, codecs and push-envelope parsing live in
// WsTransport, above the wire.
export class WebSocketPipe extends Emitter<WebSocketPipeEvents> {
    readonly #url: string
    readonly #delayMs: (attempt: number) => number
    #ws?: WebSocket
    #promise?: Promise<void>
    // A reconnect waiting out its delay: the timer to cancel, and the resolver
    // that advances the armed promise into the attempt. Set only while
    // waiting — cleared the moment the attempt starts — so its presence is
    // exactly the condition reconnectNow() acts on.
    #scheduledReconnect?: { timer: ReturnType<typeof setTimeout>; start: () => void }
    #attempt = 0
    #disposed = false

    constructor(options: WebSocketPipeOptions) {
        super()
        const { url, queryAuth } = options
        this.#url = queryAuth ? `${url}${url.includes('?') ? '&' : '?'}${queryAuth.param}=${encodeURIComponent(queryAuth.token)}` : url
        this.#delayMs = options.reconnectDelayMs ?? (() => DEFAULT_RECONNECT_DELAY_MS)
    }

    // `#promise` always tracks the current-or-next connection attempt: armed
    // when an attempt starts, re-armed on close (pending through the reconnect
    // delay). Awaiters of a failed attempt get its rejection (err hard), while
    // callers arriving after the failure await the upcoming attempt instead of
    // a stale settled promise.
    #arm(promise: Promise<void>): Promise<void> {
        this.#promise = promise
        // Background attempts (auto-reconnects) can reject with no awaiter;
        // mark the rejection handled so it doesn't surface as
        // unhandledrejection noise. ensureConnected() callers still receive it.
        promise.catch(() => {})
        return promise
    }

    #connect(): Promise<void> {
        const ws = new WebSocket(this.#url)
        this.#ws = ws
        ws.addEventListener('message', event => {
            // A dropped socket may still deliver: it is disowned, not silenced.
            if (this.#ws === ws && typeof event.data === 'string') {
                this.emit('message', event.data)
            }
        })
        return this.#arm(
            new Promise<void>((resolve, reject) => {
                let opened = false
                ws.addEventListener('open', () => {
                    opened = true
                    this.#attempt = 0
                    this.emit('open')
                    resolve()
                })
                // The 'close' event (always emitted, and the only one carrying
                // a code/reason — 'error' carries neither) drives three things:
                // notify listeners so in-flight calls fail instead of hanging;
                // reject the connect attempt if it never opened; and arm the
                // reconnect.
                ws.addEventListener('close', event => {
                    // drop() disowns the socket before abandoning it, and this
                    // fires later on the zombie: everything below already ran.
                    if (this.#ws !== ws) {
                        return
                    }
                    const closed = new WebSocketClosedError(event)
                    this.#reportClosed(closed, opened ? undefined : reject)
                })
            })
        )
    }

    // What a close means, wherever it comes from: the socket is let go, its
    // in-flight calls fail instead of hanging, a never-opened attempt rejects,
    // and the next attempt is armed behind its delay. Called by the socket's
    // own close event, and by drop() for a socket whose peer will never send
    // one. Letting go here — rather than in each caller — is what makes "no
    // socket" mean the same thing everywhere: a closed socket left in #ws would
    // let a later drop() run this a second time, arming a second reconnect over
    // the first and orphaning whichever attempt loses.
    #reportClosed(closed: WebSocketClosedError, rejectAttempt?: (e: unknown) => void): void {
        this.#ws = undefined
        this.emit('close', closed)
        rejectAttempt?.(closed)
        if (this.#disposed) {
            return
        }
        const delay = this.#delayMs(this.#attempt++)
        this.#arm(
            new Promise<void>(res => {
                const start = (): void => {
                    this.#scheduledReconnect = undefined
                    res()
                }
                this.#scheduledReconnect = { timer: setTimeout(start, delay), start }
            }).then(() => this.#connect())
        )
    }

    // Abandon a socket that cannot be trusted to close itself — one whose peer
    // is gone without a FIN, so nothing has failed and nothing will. Closing it
    // and waiting is not enough: the closing handshake has no one to answer it,
    // so the browser holds the socket in CLOSING until its own timeout, and the
    // close path — which is what fails in-flight calls and arms the reconnect —
    // would not run until then. So the close path runs here and now — disowning
    // the socket, which makes its later events no-ops (the handlers registered
    // in #connect check that #ws is still theirs) — and the zombie is closed
    // without being waited for. Unlike dispose() this is not terminal:
    // reconnection continues. A no-op when there is no socket to abandon.
    drop(): void {
        const ws = this.#ws
        if (!ws) {
            return
        }
        this.#reportClosed(new WebSocketClosedError(new CloseEvent('close', { code: 1006, reason: 'dropped' })))
        ws.close()
    }

    get connected(): boolean {
        return this.#ws?.readyState === WebSocket.OPEN
    }

    // Collapse a reconnect's remaining delay and attempt it now — for a signal
    // that connectivity is likely back (the device's online event, a native
    // app resuming from background) which would otherwise be waited out. A
    // no-op unless a reconnect is actually waiting: an attempt already in
    // flight, an open socket, and a disposed pipe all have nothing to bring
    // forward. The armed promise is resolved through its own path, just
    // earlier, so awaiters are unaffected.
    reconnectNow(): void {
        const scheduled = this.#scheduledReconnect
        if (!scheduled) {
            return
        }
        clearTimeout(scheduled.timer)
        scheduled.start()
    }

    // Resolves when the current-or-next connection attempt opens. Starts the
    // first attempt if none has been made — also the eager-connect entry point.
    ensureConnected(): Promise<void> {
        if (this.#disposed) {
            throw new Error('WebSocketPipe disposed')
        }
        return this.#promise ?? this.#connect()
    }

    send(raw: string): void {
        if (!this.#ws || this.#ws.readyState !== WebSocket.OPEN) {
            throw new Error('WebSocket not open — await ensureConnected() first')
        }
        this.#ws.send(raw)
    }

    // Deliberate teardown: closes cleanly and stops reconnecting for good.
    dispose(): void {
        this.#disposed = true
        if (this.#scheduledReconnect) {
            clearTimeout(this.#scheduledReconnect.timer)
            this.#scheduledReconnect = undefined
        }
        this.#ws?.close(1000)
    }
}
