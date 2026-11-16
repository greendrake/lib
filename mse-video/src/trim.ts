// Dropping played video from a live stream's buffer. There is no seeking back in a live
// view, so nothing behind the playhead is needed again, and a buffer left to grow costs
// memory and, in browsers whose work per append grows with it, CPU.
import { readSegment } from './fmp4'

// Seconds the key frame on screen has to be past the start of the buffer before what lies
// between is removed, so a removal happens every few seconds rather than with every packet.
const TRIM_SLACK = 5
// How far short of a key frame a removal ends: inside the frame before it, which no
// engine's rounding of the times to its own precision carries to the key frame itself.
const KEY_FRAME_MARGIN = 0.001

// Says what played video one MediaSource can drop. Removing frames also removes those
// after them up to the next key frame, which depend on them, so a removal ends just short
// of the key frame the frame on screen depends on, whatever the stream's key frame interval.
export class Trimmer {
    #timescale?: number
    // In seconds, the presentation times of the key frames read, oldest first, from the one
    // the frame on screen depends on.
    readonly #keyFrames: number[] = []

    // Takes note of each segment of the stream, in order, the initialization segment first.
    read(segment: ArrayBuffer): void {
        const info = readSegment(segment)
        if (info.kind === 'init') {
            this.#timescale = info.timescale
        } else if (info.keyFrame) {
            if (this.#timescale === undefined) {
                throw new Error('A media segment ahead of the initialization segment')
            }
            this.#keyFrames.push(info.time / this.#timescale)
        }
    }

    // The range to remove: from the start of the buffer to the key frame the frame on screen
    // depends on, once that is more than TRIM_SLACK seconds in.
    range(video: Pick<HTMLVideoElement, 'buffered' | 'currentTime'>): [start: number, end: number] | undefined {
        const onScreen = this.#keyFrames.findLastIndex(time => time <= video.currentTime)
        if (onScreen < 0) {
            return undefined
        }
        // No later frame depends on those before it.
        this.#keyFrames.splice(0, onScreen)
        const buffered = video.buffered
        const end = this.#keyFrames[0] - KEY_FRAME_MARGIN
        if (buffered.length > 0 && end - buffered.start(0) > TRIM_SLACK) {
            return [buffered.start(0), end]
        }
        return undefined
    }
}
