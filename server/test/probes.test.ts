import { describe, expect, test } from 'bun:test'
import { healthz, readyz, runChecks } from '../src/main'
import type { DependencyCheck } from '../src/main'

const ok = (name: string): DependencyCheck => ({ name, check: () => Promise.resolve() })
const broken = (name: string, reason: string): DependencyCheck => ({ name, check: () => Promise.reject(new Error(reason)) })
// Ignores its signal, as a dependency that has stopped answering tends to.
const wedged = (name: string): DependencyCheck => ({ name, check: () => new Promise<never>(() => {}) })

describe('runChecks', () => {
    test('reports each dependency by name', async () => {
        expect(await runChecks([ok('db'), broken('search', 'connection refused')], 100)).toEqual({
            db: { ok: true, message: 'ok' },
            search: { ok: false, message: 'fail: connection refused' }
        })
    })

    test('a check that never answers fails on the deadline rather than hanging', async () => {
        const started = Date.now()
        const results = await runChecks([wedged('cache')], 20)
        expect(results.cache).toEqual({ ok: false, message: 'fail: timed out after 20ms' })
        expect(Date.now() - started).toBeLessThan(500)
    })

    test('the checks run at once, not one after another', async () => {
        const slow = (name: string): DependencyCheck => ({ name, check: () => Bun.sleep(40) })
        const started = Date.now()
        await runChecks([slow('a'), slow('b'), slow('c')], 200)
        expect(Date.now() - started).toBeLessThan(120)
    })
})

describe('healthz', () => {
    test('a serving process is alive', async () => {
        const response = healthz(false)
        expect(response.status).toBe(200)
        expect(await response.json()).toEqual({ status: 'ok' })
    })

    test('a draining one says so, and says it with a 503', async () => {
        const response = healthz(true)
        expect(response.status).toBe(503)
        expect(await response.json()).toEqual({ status: 'draining' })
    })
})

describe('readyz', () => {
    test('ready when every dependency answers', async () => {
        const response = await readyz([ok('db'), ok('search')], 100, false)
        expect(response.status).toBe(200)
        expect(await response.json()).toEqual({ db: 'ok', search: 'ok' })
    })

    test('one failing dependency makes the whole service unready, and names itself', async () => {
        const response = await readyz([ok('db'), broken('search', 'connection refused')], 100, false)
        expect(response.status).toBe(503)
        expect(await response.json()).toEqual({ db: 'ok', search: 'fail: connection refused' })
    })

    test('draining is unready however healthy the dependencies are', async () => {
        const response = await readyz([ok('db')], 100, true)
        expect(response.status).toBe(503)
        expect(await response.json()).toEqual({ db: 'ok', draining: true })
    })

    test('a service with no dependencies is ready', async () => {
        const response = await readyz([], 100, false)
        expect(response.status).toBe(200)
        expect(await response.json()).toEqual({})
    })
})
