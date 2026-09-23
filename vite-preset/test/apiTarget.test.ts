import { describe, expect, test } from 'bun:test'
import { resolveApiTarget, type ApiTargetEnv } from '../src/apiTarget'

describe('resolveApiTarget', () => {
    test('dev local: http/ws against localhost:apiPort', () => {
        expect(resolveApiTarget({ dev: true, apiPort: '4321' })).toEqual({
            apiUrl: 'http://localhost:4321/v1/api',
            wsUrl: 'ws://localhost:4321/v1/ws'
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
            apiUrl: 'https://api.example.com/v1/api',
            wsUrl: 'wss://api.example.com/v1/ws'
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
            apiUrl: 'https://api.example.com/v1/api',
            wsUrl: 'wss://api.example.com/v1/ws'
        })
    })

    test('prod on any other host: same-origin fallback', () => {
        const env: ApiTargetEnv = {
            dev: false,
            publicFeHost: 'app.example.com',
            publicApiHost: 'api.example.com'
        }
        expect(resolveApiTarget(env, { host: 'internal.test', https: false })).toEqual({
            apiUrl: 'http://internal.test/v1/api',
            wsUrl: 'ws://internal.test/v1/ws'
        })
        expect(resolveApiTarget(env, { host: 'other.example', https: true }).apiUrl).toBe('https://other.example/v1/api')
    })

    test('prod without public hosts configured: always same-origin', () => {
        expect(resolveApiTarget({ dev: false }, { host: 'some.host', https: true }).wsUrl).toBe('wss://some.host/v1/ws')
    })

    test('prod without runtime location throws', () => {
        expect(() => resolveApiTarget({ dev: false })).toThrow('runtime')
    })

    test('prod on publicFeHost without publicApiHost throws', () => {
        expect(() => resolveApiTarget({ dev: false, publicFeHost: 'app.example.com' }, { host: 'app.example.com', https: true })).toThrow('publicApiHost')
    })

    test('apiPath/wsPath override the default endpoint paths (root-path PHP backends)', () => {
        expect(
            resolveApiTarget({
                dev: true,
                apiPort: '8099',
                apiPath: ''
            })
        ).toEqual({
            apiUrl: 'http://localhost:8099',
            wsUrl: 'ws://localhost:8099/v1/ws'
        })
        const env: ApiTargetEnv = {
            dev: false,
            publicFeHost: 'site.example.org',
            publicApiHost: 'api.example.org',
            apiPath: ''
        }
        expect(resolveApiTarget(env, { host: 'site.example.org', https: true }).apiUrl).toBe('https://api.example.org')
        expect(
            resolveApiTarget({
                dev: true,
                apiPort: '1',
                apiPath: '/rpc',
                wsPath: '/sock'
            })
        ).toEqual({
            apiUrl: 'http://localhost:1/rpc',
            wsUrl: 'ws://localhost:1/sock'
        })
    })
})
