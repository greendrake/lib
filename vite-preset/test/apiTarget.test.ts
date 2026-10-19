import { describe, expect, test } from 'bun:test'
import { resolveApiTarget, type ApiTargetEnv } from '../src/apiTarget'

describe('resolveApiTarget', () => {
    test('dev local: http/ws against localhost:apiPort', () => {
        expect(resolveApiTarget({ dev: true, apiPort: '4321' })).toEqual({
            httpOrigin: 'http://localhost:4321',
            wsOrigin: 'ws://localhost:4321'
        })
    })

    test('dev remote: https/wss against publicApiHost, apiPort ignored', () => {
        expect(
            resolveApiTarget({
                dev: true,
                apiPort: '4321',
                publicApiHost: 'api.example.com'
            })
        ).toEqual({
            httpOrigin: 'https://api.example.com',
            wsOrigin: 'wss://api.example.com'
        })
    })

    test('dev local without apiPort throws', () => {
        expect(() => resolveApiTarget({ dev: true })).toThrow('apiPort')
    })

    test('prod on the public FE host: API on its own host, proto follows the page', () => {
        const env: ApiTargetEnv = {
            dev: false,
            publicFeHost: 'app.example.com',
            publicApiHost: 'api.example.com'
        }
        expect(resolveApiTarget(env, { host: 'app.example.com', https: true })).toEqual({
            httpOrigin: 'https://api.example.com',
            wsOrigin: 'wss://api.example.com'
        })
    })

    test('prod on any other host: same-origin fallback', () => {
        const env: ApiTargetEnv = {
            dev: false,
            publicFeHost: 'app.example.com',
            publicApiHost: 'api.example.com'
        }
        expect(resolveApiTarget(env, { host: 'internal.test', https: false })).toEqual({
            httpOrigin: 'http://internal.test',
            wsOrigin: 'ws://internal.test'
        })
        expect(resolveApiTarget(env, { host: 'other.example', https: true }).httpOrigin).toBe('https://other.example')
    })

    test('prod without public hosts configured: always same-origin', () => {
        expect(resolveApiTarget({ dev: false }, { host: 'some.host', https: true }).wsOrigin).toBe('wss://some.host')
    })

    test('prod without runtime location throws', () => {
        expect(() => resolveApiTarget({ dev: false })).toThrow('runtime')
    })

    test('prod on publicFeHost without publicApiHost throws', () => {
        expect(() => resolveApiTarget({ dev: false, publicFeHost: 'app.example.com' }, { host: 'app.example.com', https: true })).toThrow('publicApiHost')
    })
})
