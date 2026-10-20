import { serve } from '@greendrake/server'
import type { RunningService } from '@greendrake/server'
import { ConnRegistry, createDispatcher, httpHandler, keyAuthResolver, memoryNonceStore, websocketHandler } from '@greendrake/rpc-server'
import type { ConnectionData } from '@greendrake/rpc-server'
import { createServiceStateMachine, serviceStateBoard } from '@greendrake/service-state/server'
import { OPERATOR, apiMethods, type Operator } from './methods'
import { MOCK_SERVICES, createMockService } from './mock'

// How long a nonce is remembered, which is how long a retry of the same call
// is recognised as a repeat rather than a second request.
const NONCE_TTL_MS = 5 * 60 * 1000

// How old a reading of a service may get before its machine takes another.
// The mocks change only through their machines, but a real service is changed
// by other hands too, and this is how long a machine may go without noticing.
const MAX_AGE_MS = 5000

export interface ApiOptions {
    port: number
    // The one credential this service knows. A real one would resolve tokens
    // against a user store instead; the shape of the resolver is the same.
    adminKey: string
    // The dashboard's origin: the page that may call the API and open its
    // socket, besides the API's own.
    dashOrigin: string
    // Left at the mocks' own three seconds by anything but a test, which has
    // no patience and nothing to watch.
    transitionMs?: number
}

export const startApi = async (options: ApiOptions): Promise<RunningService<ConnectionData<Operator>>> => {
    const mocks = new Map(MOCK_SERVICES.map(name => [name, createMockService(options.transitionMs)]))
    // The registry is where the board publishes every service's state as well
    // as the socket index: a state change reaches every dashboard watching,
    // which is the whole point of the pair.
    const registry = new ConnRegistry<Operator>()
    const board = serviceStateBoard(registry)
    const machines = [...mocks].map(([name, mock]) => createServiceStateMachine(name, mock, board, MAX_AGE_MS))

    const dispatcher = createDispatcher(apiMethods(board, mocks), {
        auth: keyAuthResolver<Operator>(options.adminKey, OPERATOR),
        nonces: memoryNonceStore(NONCE_TTL_MS)
    })
    const api = httpHandler(dispatcher)
    const origins = [options.dashOrigin]
    const ws = websocketHandler(dispatcher, registry, { origins })

    return serve({
        name: 'example-service-api',
        port: options.port,
        // Readiness follows the services being readable at all, not their being
        // on: a service deliberately OFF is a fine thing for this API to serve.
        checks: [{ name: 'services', check: () => Promise.all([...mocks.values()].map(mock => mock.check())).then(() => undefined) }],
        probeTimeoutMs: 2000,
        shutdown: { deadlineMs: 10_000, lameDuckMs: 0 },
        cors: { origin: origins },
        drain: () => machines.forEach(machine => machine.close()),
        websocket: ws.handlers,
        fetch: (request, server) => {
            const { pathname } = new URL(request.url)
            if (pathname === '/v1/ws') {
                return ws.upgrade(request, server)
            }
            if (pathname === '/v1/api') {
                return api(request, server)
            }
            return new Response(null, { status: 404 })
        }
    })
}
