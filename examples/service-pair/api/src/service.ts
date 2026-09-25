import { serve } from '@greendrake/server'
import type { RunningService } from '@greendrake/server'
import { ConnRegistry, clientIp, createDispatcher, httpHandler, keyAuthResolver, memoryNonceStore, websocketHandler } from '@greendrake/rpc-server'
import type { ConnectionData } from '@greendrake/rpc-server'
import { createServiceStateMachine } from '@greendrake/service-state/server'
import { apiMethods } from './methods'
import { MOCK_SERVICE, createMockService } from './mock'

// How long a nonce is remembered, which is how long a retry of the same call
// is recognised as a repeat rather than a second request.
const NONCE_TTL_MS = 5 * 60 * 1000

export interface ApiOptions {
    port: number
    // The one credential this service knows. A real one would resolve tokens
    // against a user store instead; the shape of the resolver is the same.
    adminKey: string
    // Left at the mock's own three seconds by anything but a test, which has
    // no patience and nothing to watch.
    transitionMs?: number
}

export const startApi = async (options: ApiOptions): Promise<RunningService<ConnectionData>> => {
    const mock = createMockService(options.transitionMs)
    // The registry is the machine's event sink as well as the socket index: a
    // state change reaches every dashboard watching, which is the whole point
    // of the pair.
    const registry = new ConnRegistry()
    const machine = await createServiceStateMachine(MOCK_SERVICE, mock, registry)

    const dispatcher = createDispatcher(apiMethods(machine, mock), {
        auth: keyAuthResolver(options.adminKey, {
            userId: 'operator',
            isAdmin: true,
            phantom: false
        }),
        nonces: memoryNonceStore(NONCE_TTL_MS)
    })
    const api = httpHandler(dispatcher)
    const ws = websocketHandler(dispatcher, registry)

    return serve<ConnectionData>({
        name: 'example-service-api',
        port: options.port,
        // Readiness follows the service being readable at all, not its being
        // on: a service deliberately OFF is a fine thing for this API to serve.
        checks: [{ name: 'service', check: () => mock.check().then(() => undefined) }],
        probeTimeoutMs: 2000,
        shutdown: { deadlineMs: 10_000, lameDuckMs: 0 },
        websocket: ws.handlers,
        fetch: async (request, server) => {
            const { pathname } = new URL(request.url)
            if (pathname === '/v1/ws') {
                return (await ws.upgrade(request, server)) ? undefined : new Response(null, { status: 400 })
            }
            if (pathname === '/v1/api') {
                return api(request, clientIp(request, server))
            }
            return new Response(null, { status: 404 })
        }
    })
}
