import { decodeG711, type G711Codec } from './g711'

// One frame of a camera's audio, as the stream hub delivers it.
export interface AudioFrame {
    // When the frame is due on the timeline of the video it belongs to.
    ptsMs: number
    sampleRate: number
    codec: G711Codec
    samples: Uint8Array
}

// What the player needs of the video element showing the stream: its clock, and the
// events that say whether that clock is running.
export type AudioClock = EventTarget & Pick<HTMLVideoElement, 'currentTime' | 'paused' | 'seeking' | 'readyState'>

// Where the player's samples go: a clock, and the scheduling of samples on it.
export interface AudioOutput {
    readonly currentTime: number
    // Plays the samples at the given time on the output's clock, calling ended once
    // they have played or been stopped. Returns what stops them.
    schedule(samples: Float32Array<ArrayBuffer>, sampleRate: number, at: number, ended: () => void): () => void
}

// How far the next frame may be due from where the last one ends and still follow it
// without a gap. Beyond it the frame is put where the video says, at the cost of a gap
// or of what was queued.
const TOLERANCE_S = 0.06

// The video stops showing what is due on its clock on these.
const HALTS = ['waiting', 'pause', 'seeking', 'emptied'] as const

// HTMLMediaElement.HAVE_FUTURE_DATA: the video has what it needs to keep playing.
const HAVE_FUTURE_DATA = 3

let context: AudioContext | undefined

// The page's one audio context.
const audioContext = (): AudioContext => {
    context ??= new AudioContext()
    return context
}

// Browsers keep an audio context silent until a user gesture lets it play, and some
// only during the gesture itself: call this from the handler of the click that turns
// sound on.
export const unlockAudio = (): void => {
    void audioContext().resume()
}

const contextOutput = (context: AudioContext): AudioOutput => ({
    get currentTime() {
        return context.currentTime
    },
    schedule(samples, sampleRate, at, ended) {
        const buffer = context.createBuffer(1, samples.length, sampleRate)
        buffer.copyToChannel(samples, 0)
        const source = context.createBufferSource()
        source.buffer = buffer
        source.connect(context.destination)
        source.addEventListener('ended', ended)
        source.start(at)
        return () => source.stop()
    }
})

// Plays a stream's audio in step with the video element showing it. The video's clock
// is the master: each frame is scheduled for when the video will reach it, frames
// follow each other back to back while they stay within reach of that, and nothing
// plays while the video stands still.
export class AudioPlayer {
    readonly #video: AudioClock
    readonly #output: AudioOutput
    readonly #abort = new AbortController()
    #running: boolean
    // Output time at which the last scheduled frame ends.
    #queuedUntil = 0
    // What stops each scheduled frame that has yet to end.
    readonly #queued = new Set<() => void>()

    constructor(video: AudioClock, output: AudioOutput = contextOutput(audioContext())) {
        this.#video = video
        this.#output = output
        this.#running = !video.paused && !video.seeking && video.readyState >= HAVE_FUTURE_DATA
        const signal = this.#abort.signal
        video.addEventListener('playing', () => (this.#running = true), { signal })
        for (const event of HALTS) {
            video.addEventListener(
                event,
                () => {
                    this.#running = false
                    this.flush()
                },
                { signal }
            )
        }
    }

    push(frame: AudioFrame): void {
        if (!this.#running) {
            return
        }
        const now = this.#output.currentTime
        const due = now + frame.ptsMs / 1000 - this.#video.currentTime
        if (due < now) {
            return
        }
        let at = this.#queuedUntil
        if (Math.abs(due - at) > TOLERANCE_S) {
            if (due < at) {
                // The video has moved on from what is queued.
                this.flush()
            }
            at = due
        }
        const stop = this.#output.schedule(decodeG711(frame.codec, frame.samples), frame.sampleRate, at, () => this.#queued.delete(stop))
        this.#queued.add(stop)
        this.#queuedUntil = at + frame.samples.length / frame.sampleRate
    }

    // Silences whatever is queued.
    flush(): void {
        this.#queued.forEach(stop => stop())
        this.#queued.clear()
        this.#queuedUntil = 0
    }

    close(): void {
        this.#abort.abort()
        this.flush()
    }
}
