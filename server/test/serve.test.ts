import { afterEach, describe, expect, test } from 'bun:test'
import { serve } from '../src/main'
import type { RunningService, ServeOptions } from '../src/main'

const running: RunningService<undefined>[] = []

// Anything a test leaves listening would hold its port against the next one,
// and anything left live would keep the process's signal handlers installed.
afterEach(async () => {
    await Promise.all(
        running.splice(0).map(async service => {
            await service.stop()
            await service.server.stop(true)
        })
    )
})

const DASH = 'https://dash.example.com'

const start = (options: Partial<Parameters<typeof serve<undefined>>[0]> = {}): RunningService<undefined> => {
    const service = serve<undefined>({
        name: 'test',
        port: 0,
        probeTimeoutMs: 100,
        shutdown: { deadlineMs: 200, lameDuckMs: 0 },
        fetch: () => new Response('app'),
        cors: { origin: [DASH] },
        ...options
    })
    running.push(service)
    return service
}

const url = (service: RunningService<undefined>, path: string): string => `http://localhost:${service.server.port}${path}`

// A request as a page on `origin` makes it.
const from = (origin: string, method = 'GET'): RequestInit => ({ method, headers: { Origin: origin } })

// A handler the test asserts is never reached, by failing loudly if it is.
const unreachable =
    (why: string): ServeOptions<undefined>['fetch'] =>
    () => {
        throw new Error(`the application must not be reached for ${why}`)
    }

describe('serve', () => {
    test('the probes are answered before the application sees the request', async () => {
        const service = start({ fetch: unreachable('a probe') })
        expect((await fetch(url(service, '/healthz'))).status).toBe(200)
        expect((await fetch(url(service, '/readyz'))).status).toBe(200)
    })

    test('everything else is the application', async () => {
        const service = start()
        expect(await (await fetch(url(service, '/anything'))).text()).toBe('app')
    })

    test('a preflight is answered without reaching the application', async () => {
        const service = start({ fetch: unreachable('a preflight') })
        const response = await fetch(url(service, '/v1/api'), from(DASH, 'OPTIONS'))
        expect(response.status).toBe(204)
        expect(response.headers.get('access-control-allow-origin')).toBe(DASH)
        expect(response.headers.get('vary')).toBe('Origin')
    })

    test('an admitted origin is answered with itself, on a real response too', async () => {
        const service = start({
            cors: {
                origin: [DASH],
                credentials: true,
                exposeHeaders: ['X-Total']
            }
        })
        const response = await fetch(url(service, '/anything'), from(DASH))
        expect(response.headers.get('access-control-allow-origin')).toBe(DASH)
        expect(response.headers.get('access-control-expose-headers')).toBe('X-Total')
        expect(response.headers.get('access-control-allow-credentials')).toBe('true')
        expect(response.headers.get('vary')).toBe('Origin')
    })

    test('any other origin gets no CORS headers, and its browser keeps the response from the page', async () => {
        const service = start({ cors: { origin: candidate => candidate === DASH } })
        for (const init of [from('https://evil.example'), from('https://evil.example', 'OPTIONS')]) {
            const response = await fetch(url(service, '/anything'), init)
            expect(response.headers.get('access-control-allow-origin')).toBeNull()
            expect(response.headers.get('access-control-allow-methods')).toBeNull()
            expect(response.headers.get('vary')).toBe('Origin')
        }
    })

    test('a wildcard answers every origin with itself, and offers no credentials', async () => {
        const service = start({ cors: { origin: '*' } })
        const response = await fetch(url(service, '/anything'), from('https://anywhere.example'))
        expect(response.headers.get('access-control-allow-origin')).toBe('*')
        expect(response.headers.get('access-control-allow-credentials')).toBeNull()
    })

    test('credentials for any origin are refused as the service is built', () => {
        expect(() => start({ cors: { origin: '*', credentials: true } })).toThrow('withCors: credentials offered to any origin')
    })

    test('CORS can be left off entirely', async () => {
        const service = start({ cors: false })
        const response = await fetch(url(service, '/anything'), from(DASH))
        expect(response.headers.get('access-control-allow-origin')).toBeNull()
    })
})

describe('shutdown', () => {
    test('unready while still serving through the lame duck, then drain', async () => {
        const order: string[] = []
        const service = start({
            shutdown: { deadlineMs: 500, lameDuckMs: 80 },
            drain: () => {
                order.push('drain')
            }
        })
        expect(service.draining).toBe(false)

        const stopping = service.stop()
        expect(service.draining).toBe(true)
        // Still listening, and already telling whatever routes traffic to stop
        // sending it: that gap is the whole point of the lame-duck period.
        const response = await fetch(url(service, '/readyz'))
        expect(response.status).toBe(503)
        expect(await response.json()).toEqual({ draining: true })
        expect(order).toEqual([])

        expect(await stopping).toBe('clean')
        expect(order).toEqual(['drain'])
    })

    test('a drain that will not finish is cut off at the deadline', async () => {
        const service = start({
            shutdown: { deadlineMs: 50, lameDuckMs: 0 },
            drain: () => new Promise<void>(() => {})
        })
        expect(await service.stop()).toBe('forced')
    })

    test('stopping again joins the sequence under way', async () => {
        let drains = 0
        const service = start({
            drain: () => {
                drains += 1
            }
        })
        expect(await Promise.all([service.stop(), service.stop()])).toEqual(['clean', 'clean'])
        expect(drains).toBe(1)
    })

    test('the signal handlers are one pair for the process, there while a service is', async () => {
        const before = process.listenerCount('SIGTERM')
        const [one, two] = [start(), start()]
        expect(process.listenerCount('SIGTERM')).toBe(before + 1)
        expect(process.listenerCount('SIGINT')).toBe(process.listenerCount('SIGTERM'))
        await one.stop()
        expect(process.listenerCount('SIGTERM')).toBe(before + 1)
        await two.stop()
        expect(process.listenerCount('SIGTERM')).toBe(before)
    })
})
