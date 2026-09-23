import { describe, expect, test } from 'bun:test'
import { Emitter } from '../src/Emitter'

interface Events {
    hit: [count: number]
    flat: []
    [key: string]: unknown[]
}

describe('Emitter', () => {
    test('on receives payloads until unsubscribed', () => {
        const e = new Emitter<Events>()
        const seen: number[] = []
        const off = e.on('hit', n => seen.push(n))
        e.emit('hit', 1)
        e.emit('hit', 2)
        off()
        e.emit('hit', 3)
        expect(seen).toEqual([1, 2])
    })

    test('once fires exactly once', () => {
        const e = new Emitter<Events>()
        let count = 0
        e.once('flat', () => count++)
        e.emit('flat')
        e.emit('flat')
        expect(count).toBe(1)
    })

    test('off detaches both kinds', () => {
        const e = new Emitter<Events>()
        let count = 0
        const fn = (): void => {
            count++
        }
        e.on('flat', fn)
        e.once('flat', fn)
        e.off('flat', fn)
        e.emit('flat')
        expect(count).toBe(0)
    })
})
