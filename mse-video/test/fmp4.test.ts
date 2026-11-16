import { describe, expect, test } from 'bun:test'
import { concatArrayBuffers } from '../src/bytes'
import { readSegment } from '../src/fmp4'
import { DEPENDENT_FRAME, KEY_FRAME, fragment, fullBox, i32, initSegment, mediaSegment, u32, u64 } from './segments'

const tfdt = fullBox('tfdt', 1, 0, u64(90000))

describe('readSegment', () => {
    test('reads the timescale of an initialization segment', () => {
        expect(readSegment(initSegment(90000))).toEqual({ kind: 'init', timescale: 90000 })
        expect(readSegment(initSegment(48000, 1))).toEqual({ kind: 'init', timescale: 48000 })
    })

    test('reads whether a media segment is a key frame, and when it is presented', () => {
        expect(readSegment(mediaSegment(KEY_FRAME, 900000))).toEqual({
            kind: 'media',
            keyFrame: true,
            time: 900000
        })
        expect(readSegment(mediaSegment(DEPENDENT_FRAME, 903600))).toEqual({
            kind: 'media',
            keyFrame: false,
            time: 903600
        })
    })

    test('reads a 32-bit decode time', () => {
        const segment = fragment(fullBox('tfhd', 0, 0x20, u32(1), u32(KEY_FRAME)), fullBox('tfdt', 0, 0, u32(1800)), fullBox('trun', 0, 0x1, u32(1), u32(0)))
        expect(readSegment(segment)).toEqual({
            kind: 'media',
            keyFrame: true,
            time: 1800
        })
    })

    test("takes the trun's first sample flags over the tfhd's default", () => {
        const segment = fragment(fullBox('tfhd', 0, 0x20, u32(1), u32(DEPENDENT_FRAME)), tfdt, fullBox('trun', 0, 0x5, u32(1), u32(0), u32(KEY_FRAME)))
        expect(readSegment(segment)).toMatchObject({ keyFrame: true })
    })

    test("finds the first sample's own flags past the fields ahead of them", () => {
        const segment = fragment(fullBox('tfhd', 0, 0x23, u32(1), u64(0), u32(1), u32(DEPENDENT_FRAME)), tfdt, fullBox('trun', 0, 0x701, u32(1), u32(0), u32(3600), u32(4), u32(KEY_FRAME)))
        expect(readSegment(segment)).toMatchObject({ keyFrame: true })
    })

    test("adds the first sample's composition time offset", () => {
        const offset = (version: number, value: ArrayBuffer) => readSegment(fragment(fullBox('tfhd', 0, 0x20, u32(1), u32(KEY_FRAME)), tfdt, fullBox('trun', version, 0x801, u32(1), u32(0), value)))
        expect(offset(0, u32(3000))).toMatchObject({ time: 93000 })
        expect(offset(1, i32(-3000))).toMatchObject({ time: 87000 })
    })

    test('refuses a fragment that leaves its sample flags to the trex', () => {
        const segment = fragment(fullBox('tfhd', 0, 0, u32(1)), tfdt, fullBox('trun', 0, 0x1, u32(1), u32(0)))
        expect(() => readSegment(segment)).toThrow('No sample flags in the MP4 fragment')
    })

    test('refuses a segment missing a box it reads', () => {
        const segment = fragment(fullBox('tfhd', 0, 0x20, u32(1), u32(KEY_FRAME)), fullBox('trun', 0, 0x1, u32(1), u32(0)))
        expect(() => readSegment(segment)).toThrow('No tfdt box in the MP4 traf')
    })

    test('refuses a box of a size it does not read', () => {
        expect(() => readSegment(concatArrayBuffers(u32(1), u32(0)))).toThrow('Unsupported MP4 box size: 1')
    })
})
