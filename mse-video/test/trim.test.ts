import { describe, expect, test } from 'bun:test'
import { Trimmer } from '../src/trim'
import { DEPENDENT_FRAME, KEY_FRAME, initSegment, mediaSegment } from './segments'
import { video } from './video'

const TIMESCALE = 90000

const keyFrame = (seconds: number): ArrayBuffer => mediaSegment(KEY_FRAME, seconds * TIMESCALE)

// A Trimmer that has read a stream with key frames at the given seconds.
const trimmer = (...keyFrames: number[]): Trimmer => {
    const t = new Trimmer()
    t.read(initSegment(TIMESCALE))
    keyFrames.forEach(seconds => t.read(keyFrame(seconds)))
    return t
}

describe('Trimmer', () => {
    test('removes nothing with no key frame at or before the playhead', () => {
        expect(trimmer().range(video(100, [0, 100.2]))).toBeUndefined()
        expect(trimmer(10).range(video(9, [0, 9.2]))).toBeUndefined()
    })

    test('removes nothing when nothing is buffered', () => {
        expect(trimmer(0, 10).range(video(12))).toBeUndefined()
    })

    test('removes nothing until the key frame on screen is more than 5 s into the buffer', () => {
        expect(trimmer(0, 5).range(video(6, [0, 6.2]))).toBeUndefined()
        expect(trimmer(0, 5.5).range(video(6, [0, 6.2]))).toEqual([0, 5.5 - 0.001])
    })

    test('removes up to just short of the key frame on screen, however long ago it was', () => {
        expect(trimmer(0, 10, 20).range(video(15, [0, 20.2]))).toEqual([0, 10 - 0.001])
        expect(trimmer(0, 40).range(video(39, [0, 39.2]))).toBeUndefined()
        expect(trimmer(0, 40).range(video(79, [0, 79.2]))).toEqual([0, 40 - 0.001])
    })

    test('takes no frame but a key frame for one', () => {
        const t = trimmer(0)
        t.read(mediaSegment(DEPENDENT_FRAME, 10 * TIMESCALE))
        expect(t.range(video(11, [0, 11.2]))).toBeUndefined()
    })

    test('removes from the start of the first range across a gap', () => {
        expect(trimmer(0, 10, 60).range(video(65, [1, 20], [21, 65.2]))).toEqual([1, 60 - 0.001])
    })

    test('goes on from where the last removal left the buffer', () => {
        const t = trimmer(0, 10, 20)
        expect(t.range(video(21, [0, 21.2]))).toEqual([0, 20 - 0.001])
        expect(t.range(video(21, [20, 21.2]))).toBeUndefined()
        t.read(keyFrame(30))
        expect(t.range(video(31, [20, 31.2]))).toEqual([20, 30 - 0.001])
    })

    test('refuses a media segment ahead of the initialization segment', () => {
        expect(() => new Trimmer().read(keyFrame(0))).toThrow('A media segment ahead of the initialization segment')
    })
})
