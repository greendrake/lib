import { describe, expect, test } from 'bun:test'
import { bearerToken, keyAuthResolver } from '../src/main'

const OPERATOR = { operator: true }

const withHeader = (value?: string): Request => new Request('http://x/', value === undefined ? undefined : { headers: { authorization: value } })

describe('bearerToken', () => {
    test.each([
        ['no header', undefined, ''],
        ['a bearer token', 'Bearer abc123', 'abc123'],
        ['a bearer token, however it is cased', 'bEaReR abc123', 'abc123'],
        ['a bare key, which is no credential', 'abc123', ''],
        ['a scheme with no token', 'Bearer', ''],
        ['some other scheme', 'Basic abc123', '']
    ])('reads %s', (_name, header, expected) => {
        expect(bearerToken(withHeader(header))).toBe(expected)
    })
})

describe('keyAuthResolver', () => {
    const signal = AbortSignal.any([])

    test('the configured key yields the configured principal', async () => {
        expect(await keyAuthResolver('secret', OPERATOR).resolveToken('secret', signal)).toBe(OPERATOR)
    })

    test.each([
        ['a wrong guess', 'guess!'],
        ['a prefix of the key', 'secre'],
        ['the key and more', 'secrets']
    ])('%s names nobody', async (_name, token) => {
        expect(await keyAuthResolver('secret', OPERATOR).resolveToken(token, signal)).toBeNull()
    })

    test('an unconfigured key is not a match-everything hole', async () => {
        expect(await keyAuthResolver('', OPERATOR).resolveToken('', signal)).toBeNull()
    })
})
