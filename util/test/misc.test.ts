import { describe, expect, test } from 'bun:test'
import { arrayRemove, capitalise, createDeferred, dClone, fileMime, humanFileSize, humanSeconds, isEmpty, randomString } from '../src/misc'

describe('isEmpty', () => {
    test.each([
        [null, true],
        [undefined, true],
        ['', true],
        [NaN, true],
        [[], true],
        [{}, true],
        [0, false],
        ['x', false],
        [[1], false],
        [{ a: 1 }, false],
        [false, false]
    ] as [unknown, boolean][])('isEmpty(%p) → %p', (value, expected) => {
        expect(isEmpty(value)).toBe(expected)
    })

    test('empty arrays can be declared non-empty', () => {
        expect(isEmpty([], true)).toBe(false)
    })
})

describe('misc', () => {
    test('randomString prefixes and varies', () => {
        const a = randomString('x-')
        expect(a.startsWith('x-')).toBe(true)
        expect(randomString()).not.toBe(randomString())
    })

    test('dClone deep-copies', () => {
        const src = { a: { b: 1 } }
        const clone = dClone(src)
        clone.a.b = 2
        expect(src.a.b).toBe(1)
    })

    test('arrayRemove removes first occurrence only', () => {
        const a = [1, 2, 1]
        arrayRemove(a, 1)
        expect(a).toEqual([2, 1])
        arrayRemove(a, 99)
        expect(a).toEqual([2, 1])
    })

    test('capitalise', () => {
        expect(capitalise('abc')).toBe('Abc')
        expect(capitalise('')).toBe('')
    })

    test('humanSeconds keeps two most significant units', () => {
        expect(humanSeconds(3661)).toBe('1h 1m')
        expect(humanSeconds(59)).toBe('59s')
        expect(humanSeconds(86400 + 3600)).toBe('1d 1h')
    })

    test('humanFileSize binary and SI', () => {
        expect(humanFileSize(500)).toBe('500 B')
        expect(humanFileSize(1536)).toBe('1.5 KiB')
        expect(humanFileSize(1500, true)).toBe('1.5 kB')
    })

    test('fileMime falls back to extension map, then octet-stream', () => {
        expect(fileMime(new File([''], 'a.jpg', { type: 'image/jpeg' }))).toBe('image/jpeg')
        expect(fileMime(new File([''], 'a.heic'))).toBe('image/heic')
        expect(fileMime(new File([''], 'a.zzz'))).toBe('application/octet-stream')
    })

    test('createDeferred settles externally', async () => {
        const d = createDeferred<number>()
        d.resolve(7)
        expect(await d.promise).toBe(7)
    })
})
