import { afterAll, beforeAll, describe, expect, test } from 'bun:test'
import { ApiError, HttpTransport, RpcClient, WebSocketPipe, WsTransport } from '@greendrake/rpc'
import type { RunningService } from '@greendrake/server'
import type { ConnectionData } from '@greendrake/rpc-server'
import { SERVICE_STATE_EVENT } from '@greendrake/service-state'
import type { ServiceState, ServiceStatePushes, ServiceStatus } from '@greendrake/service-state'
import { startApi } from '../src/service'
import type { ApiMethods } from '../src/methods'

const KEY = 'test-admin-key'
// The mock's three seconds are for a person watching a dashboard; a test only
// needs the transition to be observable.
const TRANSITION_MS = 20

let service: RunningService<ConnectionData>
let host: string

beforeAll(async () => {
    service = await startApi({
        port: 0,
        adminKey: KEY,
        transitionMs: TRANSITION_MS
    })
    host = service.server.url.host
})

afterAll(() => void service.server.stop(true))

const httpClient = (token?: string): RpcClient<ApiMethods> => new RpcClient<ApiMethods>(new HttpTransport({ url: `http://${host}/v1/api`, headers: (): Record<string, string> => (token ? { Authorization: token } : {}) }))

// A dashboard's socket: calls and pushes over one connection, authenticated at
// the handshake.
const dashboard = async (): Promise<{ client: RpcClient<ApiMethods>; seen: ServiceStatus[]; close: () => void }> => {
    const pipe = new WebSocketPipe({ url: `ws://${host}/v1/ws`, queryAuth: { param: 'token', token: KEY } })
    const transport = new WsTransport<ServiceStatePushes>({ pipe })
    const seen: ServiceStatus[] = []
    transport.onEvent(SERVICE_STATE_EVENT, status => seen.push(status))
    // A dashboard that only watches never sends anything, and the pipe
    // connects on demand — so the socket has to be asked for.
    await transport.connect()
    return {
        client: new RpcClient<ApiMethods>(transport),
        seen,
        close: () => pipe.dispose()
    }
}

// The states pushed so far, waited for rather than slept towards.
const until = async (seen: ServiceStatus[], state: ServiceState): Promise<void> => {
    const deadline = Date.now() + 2000
    while (!seen.some(status => status.state === state)) {
        if (Date.now() > deadline) {
            throw new Error(`never reached ${state}; saw ${seen.map(s => s.state).join(' → ') || 'nothing'}`)
        }
        await Bun.sleep(5)
    }
}

describe('the example API', () => {
    test('a call needs the admin credential', async () => {
        await expect(httpClient().call('service.status')).rejects.toThrow(new ApiError('AUTH_REQUIRED'))
        expect(await httpClient(KEY).call('service.status')).toMatchObject({ state: 'OFF' })
    })

    test('a full round trip, with every state change pushed to every socket', async () => {
        const [operator, onlooker] = await Promise.all([dashboard(), dashboard()])

        expect(await operator.client.call('service.start')).toMatchObject({ state: 'STARTING' })
        await until(operator.seen, 'ON')
        expect(await operator.client.call('service.stop')).toMatchObject({ state: 'STOPPING' })
        await until(operator.seen, 'OFF')

        expect(operator.seen.map(status => status.state)).toEqual(['STARTING', 'ON', 'STOPPING', 'OFF'])
        // The onlooker asked for nothing and saw the same thing: a service has
        // one state, and it is everybody's.
        expect(onlooker.seen.map(status => status.state)).toEqual(['STARTING', 'ON', 'STOPPING', 'OFF'])
        operator.close()
        onlooker.close()
    })

    test('a transition that fails leaves ERROR carrying why, and a refresh reads past it', async () => {
        const operator = await dashboard()
        await operator.client.call('mock.fail_next', { command: 'start' })
        await operator.client.call('service.start')
        await until(operator.seen, 'ERROR')

        expect(operator.seen.at(-1)).toMatchObject({ state: 'ERROR', error: 'mock service failed to start' })
        expect(await operator.client.call('service.refresh')).toMatchObject({ state: 'OFF', error: null })
        operator.close()
    })

    test('an argument the schema refuses comes back named', async () => {
        await expect(httpClient(KEY).call('mock.fail_next', { command: 'explode' } as never)).rejects.toMatchObject({
            message: 'INVALID_ARGUMENT',
            details: [{ field: 'command' }]
        })
    })
})
