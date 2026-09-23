import { describe, expect, test } from 'bun:test'
import { ApiError, NotFoundError, makeError, packRequest } from '../src/main'

describe('packRequest', () => {
    test('nonce and arguments only when applicable', () => {
        const nonced = packRequest('a.b', ['x'], true)
        expect(nonced.method).toBe('a.b')
        expect(typeof nonced.nonce).toBe('string')
        expect(nonced.arguments).toEqual(['x'])

        const bare = packRequest('a.b', [], false)
        expect(bare).toEqual({ method: 'a.b' })
        expect('nonce' in bare).toBe(false)
        expect('arguments' in bare).toBe(false)
    })
})

describe('makeError', () => {
    test('notfound codes map to NotFoundError', () => {
        expect(makeError('notfound')).toBeInstanceOf(NotFoundError)
        expect(makeError('LISTING_NOT_FOUND')).toBeInstanceOf(NotFoundError)
    })

    test('other codes map to ApiError with details', () => {
        const e = makeError('LISTING_CAP_EXCEEDED', { cap: 5 })
        expect(e).toBeInstanceOf(ApiError)
        expect(e.message).toBe('LISTING_CAP_EXCEEDED')
        expect((e as ApiError).details).toEqual({ cap: 5 })
    })
})
