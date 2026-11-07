import { describe, expect, test } from 'bun:test'
import { ApiError, NotFoundError, makeError, packRequest, settleKickoff, tokenAuth } from '../src/main'
import type { RpcRequest } from '../src/main'

describe('packRequest', () => {
    test('nonce and arguments only when applicable', () => {
        const nonced = packRequest('a.b', ['x'], true)
        expect(nonced.method).toBe('a.b')
        expect(nonced.nonce).toMatch(/^[0-9a-f]{32}$/)
        expect(nonced.arguments).toEqual(['x'])

        const bare = packRequest('a.b', [], false)
        expect(bare).toEqual({ method: 'a.b' })
        expect('nonce' in bare).toBe(false)
        expect('arguments' in bare).toBe(false)
    })

    test('every nonce is fresh', () => {
        expect(packRequest('a.b', [], true).nonce).not.toBe(packRequest('a.b', [], true).nonce)
    })

    test('a locale only when given', () => {
        expect(packRequest('a.b', [], false, 'fr')).toEqual({ method: 'a.b', locale: 'fr' })
    })
})

describe('makeError', () => {
    test('a *_NOT_FOUND code maps to NotFoundError, which is an ApiError carrying it', () => {
        const e = makeError('THING_NOT_FOUND', { id: 'T1' })
        expect(e).toBeInstanceOf(NotFoundError)
        expect(e).toBeInstanceOf(ApiError)
        expect(e.code).toBe('THING_NOT_FOUND')
        expect(e.details).toEqual({ id: 'T1' })
    })

    test('other codes map to ApiError with details', () => {
        const e = makeError('THING_CAP_EXCEEDED', { cap: 5 })
        expect(e).not.toBeInstanceOf(NotFoundError)
        expect(e.code).toBe('THING_CAP_EXCEEDED')
        expect(e.message).toBe('THING_CAP_EXCEEDED')
        expect(e.details).toEqual({ cap: 5 })
    })

    test("an app's own not-found is NOT_FOUND", () => {
        expect(new NotFoundError().code).toBe('NOT_FOUND')
    })
})

describe('settleKickoff', () => {
    const reissue = (): Promise<string> => Promise.resolve('reissued')

    test("is the data of the envelope the kickoff fetched, or the server's error", async () => {
        expect(await settleKickoff(Promise.resolve('{"success":true,"data":"kicked"}'), 'app.init', reissue)).toBe('kicked')
        await expect(settleKickoff(Promise.resolve('{"success":false,"error":"THING_NOT_FOUND"}'), 'app.init', reissue)).rejects.toBeInstanceOf(NotFoundError)
    })

    test('re-issues a kickoff that failed, or that brought back no envelope', async () => {
        expect(await settleKickoff(Promise.reject(new TypeError('Failed to fetch')), 'app.init', reissue)).toBe('reissued')
        expect(await settleKickoff(Promise.resolve('<html>Bad gateway</html>'), 'app.init', reissue)).toBe('reissued')
    })
})

describe('tokenAuth', () => {
    test('binds and unbinds over the reserved methods', async () => {
        const sent: RpcRequest[] = []
        const send = (request: RpcRequest): Promise<unknown> => {
            sent.push(request)
            return Promise.resolve()
        }
        await tokenAuth.authenticate(send, 't0k')
        await tokenAuth.deauthenticate(send)
        expect(sent).toEqual([{ method: 'auth.token', arguments: ['t0k'] }, { method: 'auth.logout' }])
    })
})
