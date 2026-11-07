import { describe, expect, test } from 'bun:test'
import { valueAt } from '../src/InfiniteScrollTable.types'

describe('valueAt', () => {
    const row = {
        title: 'Kayak',
        original: { name: 'kayak.jpg', size: 2048 },
        empty: null
    }

    test('a plain field reads the row', () => {
        expect(valueAt(row, 'title')).toBe('Kayak')
    })

    test('a dotted field reads a path into it', () => {
        expect(valueAt(row, 'original.size')).toBe(2048)
        expect(valueAt(row, 'original.name')).toBe('kayak.jpg')
    })

    test('a step through no object reads as undefined', () => {
        expect(valueAt(row, 'empty.size')).toBeUndefined()
        expect(valueAt(row, 'missing.size')).toBeUndefined()
        expect(valueAt(row, 'title.length')).toBeUndefined()
        expect(valueAt(row, 'original.missing')).toBeUndefined()
    })
})
