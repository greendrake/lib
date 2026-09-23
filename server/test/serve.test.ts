import { afterEach, describe, expect, test } from 'bun:test'
import { serve } from '../src/main'
import type { RunningService, ServeOptions } from '../src/main'

const running: RunningService<undefined>[] = []

// Anything a test leaves listening would hold its port against the next one.
afterEach(async () => {
    await Promise.all(running.splice(0).map(service => service.server.stop(true)))
})

const start = (options: Partial<Parameters<typeof serve<undefined>>[0]> = {}): RunningService<undefined> => {
    const service = serve<undefined>({
        name: 'test',
        port: 0,
        probeTimeoutMs: 100,
        shutdown: { deadlineMs: 200, lameDuckMs: 0 },
        fetch: () => new Response('app'),
        ...options
    })
    running.push(service)
    return service
}

const url = (service: RunningService<undefined>, path: string): string => `http://localhost:${service.server.port}${path}`

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
        const response = await fetch(url(service, '/v1/api'), { method: 'OPTIONS' })
        expect(response.status).toBe(204)
        expect(response.headers.get('access-control-allow-origin')).toBe('*')
    })

    test('a real response carries the CORS headers too', async () => {
        const service = start({ cors: { origin: 'https://dash.example.com', exposeHeaders: ['X-Total'] } })
        const response = await fetch(url(service, '/anything'))
        expect(response.headers.get('access-control-allow-origin')).toBe('https://dash.example.com')
        expect(response.headers.get('access-control-expose-headers')).toBe('X-Total')
        expect(response.headers.get('access-control-allow-credentials')).toBe('true')
    })

    test('a wildcard origin does not claim to allow credentials, which a browser would refuse anyway', async () => {
        const service = start()
        const response = await fetch(url(service, '/anything'))
        expect(response.headers.get('access-control-allow-origin')).toBe('*')
        expect(response.headers.get('access-control-allow-credentials')).toBeNull()
    })

    test('CORS can be left off entirely', async () => {
        const service = start({ cors: false })
        const response = await fetch(url(service, '/anything'))
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
})
