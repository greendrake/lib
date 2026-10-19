import { Emitter } from '@greendrake/util'
import type { AudioFrame } from './AudioPlayer'
import { Backoff } from './Backoff'
import type { G711Codec } from './g711'

export interface StreamSubscriptionEvents {
    // The subscription is live on the server. Fired on the first subscribe and again
    // every time the hub re-establishes it on a new socket, where the server restarts
    // the stream from a fresh initialization segment: the consumer must rebuild its
    // decoding pipeline each time.
    subscribed: []
    // One fMP4 payload. A zero-length payload is the server's reset acknowledgement.
    packet: [payload: ArrayBuffer]
    // One frame of the camera's audio, while the subscription has it on.
    audio: [frame: AudioFrame]
    // The server will not serve this stream, or has stopped serving it. The consumer
    // decides whether and when to subscribe again.
    failed: [reason: string]
    [key: string]: unknown[]
}

export interface StreamHubOptions {
    // Base delay before reconnecting a dropped socket, doubling up to maxReopenDelayMs.
    reopenDelayMs?: number
    maxReopenDelayMs?: number
}

const DEFAULT_REOPEN_DELAY_MS = 1000
const DEFAULT_MAX_REOPEN_DELAY_MS = 30000

interface ServerMessage {
    op: 'subbed' | 'error' | 'gone'
    id?: number
    ch?: number
    key?: string
}

// What a binary frame's payload is: the byte after the channel byte.
const KIND_VIDEO = 0
const KIND_AUDIO = 1

// An audio payload's codec byte.
const AUDIO_CODECS: G711Codec[] = ['alaw', 'ulaw']

// An audio payload is [u32 ms][u16 sample rate][u8 codec][samples], big-endian.
const AUDIO_HEADER = 7

const audioFrame = (payload: DataView): AudioFrame => ({
    ptsMs: payload.getUint32(0),
    sampleRate: payload.getUint16(4),
    codec: AUDIO_CODECS[payload.getUint8(6)],
    samples: new Uint8Array(payload.buffer, payload.byteOffset + AUDIO_HEADER, payload.byteLength - AUDIO_HEADER)
})

export class StreamSubscription extends Emitter<StreamSubscriptionEvents> {
    readonly key: string
    readonly id: number
    readonly #hub: StreamHub
    // Assigned by the server, and cleared whenever the socket carrying it goes away.
    channel?: number
    // Whether the server is to send the audio: off until asked for, on every socket.
    #audio = false

    constructor(hub: StreamHub, key: string, id: number) {
        super()
        this.#hub = hub
        this.key = key
        this.id = id
    }

    get audio(): boolean {
        return this.#audio
    }

    setAudio(on: boolean): void {
        this.#audio = on
        this.#hub.sendAudio(this)
    }

    // Ask the server to restart this stream from a fresh initialization segment. It
    // answers with a zero-length packet.
    reset(): void {
        this.#hub.resetSubscription(this)
    }

    close(): void {
        this.#hub.unsubscribe(this)
    }
}

// One WebSocket per host, shared by every player on the page and demultiplexed by the
// channel byte the server puts in front of each payload.
//
// Browsers hold at most one connection per host in CONNECTING (RFC 6455 4.1), so a grid
// of N tiles on N sockets pays N serialized handshakes plus the browser's own growing
// inter-handshake delay. One socket removes that entirely.
export class StreamHub {
    readonly #url: string
    readonly #onIdle: () => void
    readonly #backoff: Backoff
    #ws?: WebSocket
    #abort?: AbortController
    #reopenTimer?: ReturnType<typeof setTimeout>
    #refs = 0
    #nextId = 1
    #subscriptions = new Set<StreamSubscription>()
    #byChannel = new Map<number, StreamSubscription>()
    // Subscribe requests awaiting an answer, by the id they were sent with.
    #awaiting = new Map<number, StreamSubscription>()

    constructor(url: string, onIdle: () => void, options: StreamHubOptions = {}) {
        this.#url = url
        this.#onIdle = onIdle
        this.#backoff = new Backoff(options.reopenDelayMs ?? DEFAULT_REOPEN_DELAY_MS, options.maxReopenDelayMs ?? DEFAULT_MAX_REOPEN_DELAY_MS)
    }

    retain(): void {
        this.#refs++
    }

    release(): void {
        this.#refs--
        if (this.#refs <= 0) {
            this.#close()
            this.#onIdle()
        }
    }

    subscribe(key: string): StreamSubscription {
        const subscription = new StreamSubscription(this, key, this.#nextId++)
        this.#subscriptions.add(subscription)
        if (this.#ws?.readyState === WebSocket.OPEN) {
            this.#request(subscription)
        } else {
            this.#connect()
        }
        return subscription
    }

    unsubscribe(subscription: StreamSubscription): void {
        this.#subscriptions.delete(subscription)
        this.#awaiting.delete(subscription.id)
        if (subscription.channel !== undefined) {
            this.#byChannel.delete(subscription.channel)
            this.#send({ op: 'unsub', ch: subscription.channel })
            subscription.channel = undefined
        }
    }

    resetSubscription(subscription: StreamSubscription): void {
        this.#sendOn(subscription, { op: 'reset' })
    }

    // Tells the server whether to send the subscription's audio. One without a channel
    // yet gets it with its next one.
    sendAudio(subscription: StreamSubscription): void {
        this.#sendOn(subscription, { op: 'audio', on: subscription.audio })
    }

    // Sends an op about a subscription's channel, if it has one.
    #sendOn(subscription: StreamSubscription, op: object): void {
        if (subscription.channel !== undefined) {
            this.#send({ ...op, ch: subscription.channel })
        }
    }

    #request(subscription: StreamSubscription): void {
        this.#awaiting.set(subscription.id, subscription)
        this.#send({
            op: 'sub',
            id: subscription.id,
            key: subscription.key
        })
    }

    #send(message: object): void {
        if (this.#ws?.readyState === WebSocket.OPEN) {
            this.#ws.send(JSON.stringify(message))
        }
    }

    #connect(): void {
        if (this.#ws || this.#reopenTimer) {
            return
        }
        try {
            this.#ws = new WebSocket(this.#url)
        } catch {
            this.#onDown()
            return
        }
        this.#ws.binaryType = 'arraybuffer'
        this.#abort = new AbortController()
        const signal = this.#abort.signal
        this.#ws.addEventListener('open', () => this.#onOpen(), { signal })
        this.#ws.addEventListener('message', event => this.#onMessage(event.data), { signal })
        this.#ws.addEventListener('error', () => this.#onDown(), { signal })
        this.#ws.addEventListener('close', () => this.#onDown(), { signal })
    }

    #onOpen(): void {
        this.#subscriptions.forEach(subscription => this.#request(subscription))
    }

    #onMessage(data: ArrayBuffer | string): void {
        // The socket is not merely open but carrying traffic, so the next drop starts a
        // fresh backoff. Clearing it on the handshake alone would leave no backoff at all
        // against a server that accepts connections and then drops them.
        this.#backoff.reset()
        if (typeof data === 'string') {
            this.#onServerMessage(JSON.parse(data) as ServerMessage)
            return
        }
        const frame = new DataView(data)
        const subscription = this.#byChannel.get(frame.getUint8(0))
        if (!subscription) {
            return
        }
        if (data.byteLength === 1) {
            // A bare channel byte acknowledges a reset.
            subscription.emit('packet', new ArrayBuffer(0))
            return
        }
        const kind = frame.getUint8(1)
        if (kind === KIND_VIDEO) {
            subscription.emit('packet', data.slice(2))
        } else if (kind === KIND_AUDIO) {
            subscription.emit('audio', audioFrame(new DataView(data, 2)))
        }
    }

    #onServerMessage(message: ServerMessage): void {
        if (message.op === 'gone') {
            const subscription = this.#byChannel.get(message.ch!)
            if (subscription) {
                this.#byChannel.delete(message.ch!)
                this.#subscriptions.delete(subscription)
                subscription.channel = undefined
                subscription.emit('failed', 'Stream gone')
            }
            return
        }
        const subscription = this.#awaiting.get(message.id!)
        this.#awaiting.delete(message.id!)
        if (!subscription) {
            // A subscription cancelled while its request was in flight. The channel the
            // server just allocated for it would otherwise stay open forever.
            if (message.op === 'subbed') {
                this.#send({ op: 'unsub', ch: message.ch })
            }
            return
        }
        if (message.op === 'subbed') {
            subscription.channel = message.ch
            this.#byChannel.set(message.ch!, subscription)
            // A new channel starts with its audio off.
            if (subscription.audio) {
                this.sendAudio(subscription)
            }
            subscription.emit('subscribed')
        } else {
            this.#subscriptions.delete(subscription)
            subscription.emit('failed', `Server refused ${subscription.key}`)
        }
    }

    #onDown(): void {
        this.#dropSocket()
        this.#byChannel.clear()
        this.#awaiting.clear()
        this.#subscriptions.forEach(subscription => {
            subscription.channel = undefined
        })
        if (this.#refs <= 0 || this.#subscriptions.size === 0) {
            return
        }
        // Every live subscription is re-sent once the socket is back, and each one is
        // told it has restarted so it can rebuild around the fresh init segment.
        this.#reopenTimer = setTimeout(() => {
            this.#reopenTimer = undefined
            this.#connect()
        }, this.#backoff.next())
    }

    #dropSocket(): void {
        this.#abort?.abort()
        this.#abort = undefined
        this.#ws?.close()
        this.#ws = undefined
    }

    #close(): void {
        if (this.#reopenTimer) {
            clearTimeout(this.#reopenTimer)
            this.#reopenTimer = undefined
        }
        this.#dropSocket()
        this.#subscriptions.clear()
        this.#byChannel.clear()
        this.#awaiting.clear()
    }
}

const hubs = new Map<string, StreamHub>()

// Hubs are shared by URL and reference counted: every player on a page talks to the same
// host over the same socket, and the socket goes away with the last of them.
export const acquireStreamHub = (url: string, options?: StreamHubOptions): StreamHub => {
    let hub = hubs.get(url)
    if (!hub) {
        hub = new StreamHub(url, () => hubs.delete(url), options)
        hubs.set(url, hub)
    }
    hub.retain()
    return hub
}
