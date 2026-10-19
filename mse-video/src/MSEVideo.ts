import { Emitter } from '@greendrake/util'
import { AudioPlayer } from './AudioPlayer'
import { acquireStreamHub, type StreamHub, type StreamHubOptions, type StreamSubscription } from './StreamHub'
import { Backoff } from './Backoff'
import { canPlay, mediaSourceOf, streamCodec } from './codec'
import { followLiveEdge } from './liveEdge'

export type MSEVideoState = 'closed' | 'opening' | 'listening' | 'playing' | 'resetting' | 'closing'

export interface MSEVideoEvents {
    // Fired on every state transition.
    state: [state: MSEVideoState]
    // Fired when a recoverable failure triggers a reset or reopen; the player
    // recovers by itself, so this is diagnostic, not fatal.
    error: [reason: string]
    [key: string]: unknown[]
}

export interface MSEVideoOptions {
    // The shared socket's, taken from whichever player opens it: every player on
    // the page pointed at one hub URL shares that one socket.
    hub?: StreamHubOptions
    // Base delay before rebuilding the pipeline, doubling on each reopen that brings
    // no data and capping at maxReopenDelayMs. The first reopen after a packet has
    // arrived is immediate.
    reopenDelayMs?: number
    maxReopenDelayMs?: number
    // While playing: max silence between appended packets before an in-place reset.
    noDataTimeoutMs?: number
    // After subscribing or resetting: max wait for the first packet before a full reopen.
    startTimeoutMs?: number
    // Max wait for the hub to confirm the subscription, which covers the shared socket
    // connecting or reconnecting underneath it.
    connectTimeoutMs?: number
}

const DEFAULT_REOPEN_DELAY_MS = 3000
const DEFAULT_MAX_REOPEN_DELAY_MS = 30000
const DEFAULT_NO_DATA_TIMEOUT_MS = 3000
const DEFAULT_START_TIMEOUT_MS = 9000
const DEFAULT_CONNECT_TIMEOUT_MS = 10000

const concatArrayBuffers = (...bufs: ArrayBuffer[]): ArrayBuffer => {
    const result = new Uint8Array(bufs.reduce((totalSize, buf) => totalSize + buf.byteLength, 0))
    bufs.reduce((offset, buf) => {
        result.set(new Uint8Array(buf), offset)
        return offset + buf.byteLength
    }, 0)
    return result.buffer
}

// MSE live playback engine, fed by one stream of a StreamHub's shared socket.
//
// The server streams an fMP4 initialization segment followed by media segments;
// each payload the hub routes here is appended to a single SourceBuffer.
//
// On Safari the source is a ManagedMediaSource, which leaves buffer eviction to
// the user agent. Its startstreaming/endstreaming throttling hints are not
// honoured: this is a live feed with no seekable past, so there is nothing to
// prefetch and nothing to defer.
//
// Reset protocol (application-level): when playback stalls (no packet appended
// within noDataTimeoutMs while playing), the player keeps the subscription but
// tears down and rebuilds the MediaSource + SourceBuffer, then asks the server to
// reset. The server acknowledges with a zero-length payload and restarts the
// stream from a fresh initialization segment; on receiving the ack while
// 'resetting' the player returns to 'listening' and the next payload resumes
// playback. If nothing arrives within startTimeoutMs, the player falls back to a
// full reopen: drop the subscription and take out a new one.
//
// Every other failure is a full reopen too: a subscription the hub does not
// confirm within connectTimeoutMs, a stream the server refuses or stops serving,
// a MediaSource or SourceBuffer that cannot be built. Consecutive reopens back off
// exponentially, and the first packet to arrive clears the backoff.
//
// A socket that drops is the hub's business, not this class's: it reconnects and
// re-subscribes, and the 'subscribed' event that follows rebuilds the pipeline here
// around the fresh initialization segment the server starts with.
//
// Playback is kept close to the newest frame by followLiveEdge.
//
// Audio, when on, arrives as frames of its own and plays through an AudioPlayer slaved
// to the video element's clock.
export class MSEVideo extends Emitter<MSEVideoEvents> {
    readonly #videoEl: HTMLVideoElement
    // Safari is the only engine that attaches a media source object directly,
    // and the only one with ManagedMediaSource, so one flag selects both. Read
    // as a player is made rather than as the module loads, which may be where
    // there is no window.
    readonly #managed = Boolean(window.ManagedMediaSource)
    readonly #backoff: Backoff
    readonly #noDataTimeoutMs: number
    readonly #startTimeoutMs: number
    readonly #connectTimeoutMs: number
    readonly #hub: StreamHub
    // Removes the listeners this player puts on the video element.
    readonly #elementAbort = new AbortController()
    #key: string
    #subscription?: StreamSubscription
    #state: MSEVideoState = 'closed'
    #mse?: MediaSource
    #sourceBuffer?: SourceBuffer
    #bufferAbort?: AbortController
    #pendingBuffer?: ArrayBuffer
    #watchdog?: ReturnType<typeof setTimeout>
    #reopenTimer?: ReturnType<typeof setTimeout>
    // Exists while the audio is on.
    #audio?: AudioPlayer

    // Construct one only in a browser canPlay says can play the stream.
    constructor(videoEl: HTMLVideoElement, hubURL: string, key: string, options: MSEVideoOptions = {}) {
        super()
        // Before anything that has to be handed back, the hub reference in particular.
        if (!canPlay()) {
            throw new Error(`Unsupported codec: ${streamCodec}`)
        }
        this.#videoEl = videoEl
        this.#hub = acquireStreamHub(hubURL, options.hub)
        this.#key = key
        this.#backoff = new Backoff(
            options.reopenDelayMs ?? DEFAULT_REOPEN_DELAY_MS,
            options.maxReopenDelayMs ?? DEFAULT_MAX_REOPEN_DELAY_MS
        )
        this.#noDataTimeoutMs = options.noDataTimeoutMs ?? DEFAULT_NO_DATA_TIMEOUT_MS
        this.#startTimeoutMs = options.startTimeoutMs ?? DEFAULT_START_TIMEOUT_MS
        this.#connectTimeoutMs = options.connectTimeoutMs ?? DEFAULT_CONNECT_TIMEOUT_MS
        // A MediaSource cannot be handed to an AirPlay receiver, so WebKit keeps
        // a ManagedMediaSource inert — sourceopen never fires — unless the element
        // either opts out of remote playback or offers an HLS <source> alternative.
        // Must be set before any src assignment.
        videoEl.disableRemotePlayback = true
        const signal = this.#elementAbort.signal
        // play() is interrupted by the load() a reset/close performs; that
        // rejection is part of normal teardown, not a failure to surface.
        videoEl.addEventListener('loadeddata', () => void videoEl.play().catch(() => undefined), { signal })
        videoEl.addEventListener('timeupdate', () => followLiveEdge(videoEl), { signal })
        videoEl.addEventListener('progress', () => followLiveEdge(videoEl), { signal })
        this.#open()
    }

    get state(): MSEVideoState {
        return this.#state
    }

    setKey(key: string): void {
        this.#key = key
        // A different stream is a fresh start, not a retry of the failing one.
        this.#backoff.reset()
        this.#restart(0)
    }

    // Turns the stream's audio on or off. Call unlockAudio from the user gesture that
    // turns it on, or the browser may keep it silent.
    setAudio(on: boolean): void {
        if (on === Boolean(this.#audio)) {
            return
        }
        if (on) {
            this.#audio = new AudioPlayer(this.#videoEl)
        } else {
            this.#audio!.close()
            this.#audio = undefined
        }
        this.#subscription?.setAudio(on)
    }

    close(): void {
        this.#elementAbort.abort()
        this.#audio?.close()
        this.#audio = undefined
        this.#teardown()
        this.#hub.release()
    }

    #teardown(): void {
        if (this.#reopenTimer) {
            clearTimeout(this.#reopenTimer)
            this.#reopenTimer = undefined
        }
        this.#clearWatchdog()
        this.#setState('closing')
        this.#closeVideo()
        this.#subscription?.close()
        this.#subscription = undefined
        this.#setState('closed')
    }

    #setState(state: MSEVideoState): void {
        if (this.#state === state) {
            return
        }
        this.#state = state
        if (state === 'opening') {
            this.#armWatchdog(this.#connectTimeoutMs)
        } else if (state === 'listening' || state === 'resetting') {
            this.#armWatchdog(this.#startTimeoutMs)
        }
        this.emit('state', state)
    }

    // The MediaSource is built only once the subscription is live, because that is when
    // the server starts a fresh initialization segment — and it does so again on every
    // re-subscribe the hub performs after a socket reconnect.
    #open(): void {
        this.#setState('opening')
        const subscription = this.#hub.subscribe(this.#key)
        this.#subscription = subscription
        subscription.setAudio(Boolean(this.#audio))
        subscription.on('subscribed', () => this.#onSubscribed())
        subscription.on('packet', payload => this.#readPacket(payload))
        subscription.on('audio', frame => {
            // While resetting, what arrives belongs to the timeline being discarded.
            if (this.#state !== 'resetting') {
                this.#audio?.push(frame)
            }
        })
        subscription.on('failed', reason => this.#reopen(reason))
    }

    #onSubscribed(): void {
        this.#closeVideo()
        this.#setState('opening')
        this.#armWatchdog(this.#startTimeoutMs)
        this.#setUp()
    }

    #setUp(): void {
        this.#mse = new (mediaSourceOf(window)!)()
        if (this.#managed) {
            this.#videoEl.srcObject = this.#mse
        } else {
            this.#videoEl.src = URL.createObjectURL(this.#mse)
            this.#videoEl.srcObject = null
        }
        this.#mse.addEventListener('sourceopen', () => this.#onSourceOpen(), { once: true })
    }

    #onSourceOpen(): void {
        if (!this.#managed) {
            URL.revokeObjectURL(this.#videoEl.src)
        }
        try {
            this.#sourceBuffer = this.#mse!.addSourceBuffer(streamCodec)
        } catch {
            this.#reopen('Could not create SourceBuffer')
            return
        }
        this.#sourceBuffer.mode = 'segments'
        this.#bufferAbort = new AbortController()
        this.#sourceBuffer.addEventListener('updateend', () => this.#eatPacket(), { signal: this.#bufferAbort.signal })
        if (this.#state === 'opening') {
            this.#setState('listening')
            // Whatever arrived while the pipeline was being built, initialization
            // segment included.
            this.#eatPacket()
        } else if (this.#state === 'resetting') {
            this.#subscription!.reset()
        } else {
            throw new Error(`Invalid state to set up: ${this.#state}`)
        }
    }

    #readPacket(packet: ArrayBuffer): void {
        // The server is alive and talking, so the next failure starts a fresh backoff.
        this.#backoff.reset()
        if (this.#state === 'resetting') {
            if (packet.byteLength === 0) {
                // The server's reset acknowledgement.
                this.#setState('listening')
            }
            // Anything ahead of it belongs to the stream being discarded.
            return
        }
        // The subscription goes live a turn of the event loop before the SourceBuffer
        // it feeds exists, so packets accumulate whether or not there is one yet.
        this.#pendingBuffer = this.#pendingBuffer ? concatArrayBuffers(this.#pendingBuffer, packet) : packet
        this.#eatPacket()
    }

    // Append the accumulated bytes to the SourceBuffer once there is one and it is not
    // busy; until then packets pile up in #pendingBuffer, and the 'updateend' listener
    // re-enters here to flush them.
    #eatPacket(): void {
        if (!this.#sourceBuffer || !this.#pendingBuffer) {
            return
        }
        // Data with a pipeline to take it: playing, whether it has just arrived or has
        // been waiting for the SourceBuffer to be built or to finish updating.
        this.#setState('playing')
        if (this.#sourceBuffer.updating) {
            return
        }
        this.#armWatchdog(this.#noDataTimeoutMs)
        const packet = this.#pendingBuffer
        this.#pendingBuffer = undefined
        try {
            this.#sourceBuffer.appendBuffer(packet)
        } catch {
            this.#reset('Append buffer error')
        }
    }

    // In-place reset: rebuild the MediaSource pipeline but keep the WebSocket,
    // asking the server (via #onSourceOpen) to restart the stream.
    #reset(reason: string): void {
        if (this.#state !== 'playing') {
            return
        }
        this.#clearWatchdog()
        this.emit('error', reason)
        this.#setState('resetting')
        this.#closeVideo()
        this.#setUp()
    }

    // Full recovery: drop the subscription and take out a new one. Repeated reopens back
    // off, so a camera that is down does not have every player watching it churning a
    // subscription — and a Stream node, and a camera dial — on every timeout.
    #reopen(reason: string): void {
        this.emit('error', reason)
        this.#restart(this.#backoff.next())
    }

    #restart(delayMs: number): void {
        this.#teardown()
        if (delayMs > 0) {
            this.#reopenTimer = setTimeout(() => this.#open(), delayMs)
        } else {
            this.#open()
        }
    }

    #closeVideo(): void {
        this.#bufferAbort?.abort()
        this.#bufferAbort = undefined
        this.#pendingBuffer = undefined
        this.#sourceBuffer = undefined
        this.#videoEl.pause()
        if (this.#managed) {
            this.#videoEl.srcObject = null
        } else {
            this.#videoEl.removeAttribute('src')
        }
        this.#videoEl.load()
        this.#mse = undefined
    }

    #armWatchdog(ms: number): void {
        this.#clearWatchdog()
        this.#watchdog = setTimeout(() => {
            if (this.#state === 'playing') {
                this.#reset('No video timeout')
            } else {
                this.#reopen(`No video timeout on ${this.#state}`)
            }
        }, ms)
    }

    #clearWatchdog(): void {
        if (this.#watchdog) {
            clearTimeout(this.#watchdog)
            this.#watchdog = undefined
        }
    }
}
