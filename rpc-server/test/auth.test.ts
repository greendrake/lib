import { describe, expect, test } from 'bun:test'
import { bearerToken, keyAuthResolver } from '../src/main'
import type { AuthInfo } from '../src/main'

const OPERATOR: AuthInfo = {
    userId: 'operator',
    isAdmin: true,
    phantom: false
}

const withHeader = (value?: string): Request => new Request('http://x/', value === undefined ? undefined : { headers: { authorization: value } })

describe('bearerToken', () => {
    test.each([
        ['no header', undefined, ''],
        ['a bearer token', 'Bearer abc123', 'abc123'],
        ['a bearer token, however it is cased', 'bEaReR abc123', 'abc123'],
        ['a bare pre-shared key', 'abc123', 'abc123'],
        ['some other scheme', 'Basic abc123', '']
    ])('reads %s', (_name, header, expected) => {
        expect(bearerToken(withHeader(header))).toBe(expected)
    })
})

describe('keyAuthResolver', () => {
    const signal = AbortSignal.any([])

    test('the configured key yields the configured principal', async () => {
        expect(await keyAuthResolver('secret', OPERATOR).resolveToken('secret', signal)).toEqual(OPERATOR)
    })

    test('anything else names nobody', async () => {
        expect(await keyAuthResolver('secret', OPERATOR).resolveToken('guess', signal)).toBeNull()
    })

    test('an unconfigured key is not a match-everything hole', async () => {
        expect(await keyAuthResolver('', OPERATOR).resolveToken('', signal)).toBeNull()
    })
})
