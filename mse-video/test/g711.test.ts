import { describe, expect, test } from 'bun:test'
import { decodeG711 } from '../src/g711'

const pcm = (codec: 'alaw' | 'ulaw', ...codes: number[]): number[] => Array.from(decodeG711(codec, Uint8Array.from(codes)), sample => sample * 32768)

describe('decodeG711', () => {
    test('A-law: the smallest steps either side of zero and the extremes', () => {
        expect(pcm('alaw', 0xd5, 0x55, 0xaa, 0x2a)).toEqual([8, -8, 32256, -32256])
    })

    test('µ-law: zero and the extremes', () => {
        expect(pcm('ulaw', 0xff, 0x80, 0x00)).toEqual([0, 32124, -32124])
    })

    test('every code decodes into [-1, 1), A-law to 256 distinct levels', () => {
        const codes = Uint8Array.from({ length: 256 }, (_, code) => code)
        for (const codec of ['alaw', 'ulaw'] as const) {
            for (const sample of decodeG711(codec, codes)) {
                expect(sample).toBeGreaterThanOrEqual(-1)
                expect(sample).toBeLessThan(1)
            }
        }
        expect(new Set(decodeG711('alaw', codes)).size).toBe(256)
    })
})
