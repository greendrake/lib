import { afterAll, beforeAll, describe, expect, test } from 'bun:test'
import { ApiError, HttpTransport, RpcClient, TOKEN_PARAM, WebSocketPipe, WsTransport, liveDocument } from '@greendrake/rpc'
import type { RunningService } from '@greendrake/server'
import type { ConnectionData } from '@greendrake/rpc-server'
import { SERVICE_STATE_DOCUMENT } from '@greendrake/service-state'
import type { ServiceState, ServiceStatePushes, ServiceStates } from '@greendrake/service-state'
import { startApi } from '../src/service'
import type { ApiMethods, Operator } from '../src/methods'

const KEY = 'test-admin-key'
const DASH_ORIGIN = 'http://localhost:5180'
// The service most calls name, and the one beside it that they leave alone.
const WEB = { service: 'web' }
// The mocks' three seconds are for a person watching a dashboard; a test only
// needs the transition to be observable.
const TRANSITION_MS = 20

let service: RunningService<ConnectionData<Operator>>
let host: string

beforeAll(async () => {
    service = await startApi({
        port: 0,
        adminKey: KEY,
        dashOrigin: DASH_ORIGIN,
        transitionMs: TRANSITION_MS
    })
    host = service.server.url.host
})

// The service's own shutdown sequence, which is what stops the machines'
// re-reads along with the listener.
afterAll(async () => expect(await service.stop()).toBe('clean'))

const httpClient = (token?: string): RpcClient<ApiMethods> => new RpcClient<ApiMethods>(new HttpTransport({ url: `http://${host}/v1/api`, headers: (): Record<string, string> => (token ? { Authorization: `Bearer ${token}` } : {}) }))

interface Dashboard {
    client: RpcClient<ApiMethods>
    // The document, as the socket keeps it.
    states: ServiceStates
    // What the dashboard would have shown for a service, frame by frame: its
    // state, or null while it has no entry.
    seen: (service: string) => (ServiceState | null)[]
    // Called after each frame has been applied to the document; returns the
    // release.
    onFrame: (listener: () => void) => () => void
    close: () => void
}

// A dashboard's socket: calls and the document over one connection,
// authenticated at the handshake.
const dashboard = async (): Promise<Dashboard> => {
    const pipe = new WebSocketPipe({ url: `ws://${host}/v1/ws`, queryAuth: { param: TOKEN_PARAM, token: KEY } })
    const transport = new WsTransport<Record<never, never>, ServiceStatePushes>({ pipe })
    const states: ServiceStates = {}
    liveDocument(transport, SERVICE_STATE_DOCUMENT, states)
    // Every listener after liveDocument's runs once it has applied the frame.
    const onFrame = (listener: () => void): (() => void) => transport.onData(SERVICE_STATE_DOCUMENT, listener)
    const frames: ServiceStates[] = []
    onFrame(() => frames.push(structuredClone(states)))
    // A dashboard that only watches never sends anything, and the pipe
    // connects on demand — so the socket has to be asked for.
    await transport.connect()
    return {
        client: new RpcClient<ApiMethods>(transport),
        states,
        seen: name => frames.map(doc => doc[name]?.state ?? null).filter((state, i, all) => i === 0 || state !== all[i - 1]),
        onFrame,
        close: () => pipe.dispose()
    }
}

// The document showing a service in a state: now, or once the frame that
// brings it has been applied. One that never comes fails on the test's own
// timeout.
const until = (at: Dashboard, name: string, state: ServiceState): Promise<void> =>
    new Promise(resolve => {
        const reached = (): boolean => at.states[name]?.state === state
        if (reached()) {
            resolve()
            return
        }
        const release = at.onFrame(() => {
            if (reached()) {
                release()
                resolve()
            }
        })
    })

describe('the example API', () => {
    test('a call needs the admin credential, and a command answers nothing', async () => {
        await expect(httpClient().call('service.refresh', WEB)).rejects.toThrow(new ApiError('AUTH_REQUIRED'))
        expect(await httpClient(KEY).call('service.refresh', WEB)).toBeUndefined()
    })

    // First of the socket tests: no socket has been open, so no reading has
    // been published — the refresh above was taken with nobody watching.
    test('the first socket is greeted with no reading, and shown each service once it has been read', async () => {
        const first = await dashboard()
        await until(first, 'web', 'OFF')
        await until(first, 'worker', 'OFF')
        expect(first.seen('web')).toEqual([null, 'OFF'])
        first.close()
    })

    test('a full round trip, with every state change published to every socket', async () => {
        const [operator, onlooker] = await Promise.all([dashboard(), dashboard()])
        await until(operator, 'web', 'OFF')
        await until(onlooker, 'web', 'OFF')

        expect(await operator.client.call('service.start', WEB)).toBeUndefined()
        await until(operator, 'web', 'ON')
        await operator.client.call('service.stop', WEB)
        await until(operator, 'web', 'OFF')
        await until(onlooker, 'web', 'OFF')

        expect(operator.seen('web').slice(-5)).toEqual(['OFF', 'STARTING', 'ON', 'STOPPING', 'OFF'])
        // The onlooker asked for nothing and saw the same thing: a service has
        // one state, and it is everybody's.
        expect(onlooker.seen('web').slice(-5)).toEqual(['OFF', 'STARTING', 'ON', 'STOPPING', 'OFF'])
        // The service beside it was never touched.
        expect(operator.seen('worker').at(-1)).toBe('OFF')
        operator.close()
        onlooker.close()
    })

    test('a transition that fails leaves ERROR carrying why, and a refresh reads past it', async () => {
        const operator = await dashboard()
        await until(operator, 'web', 'OFF')
        await operator.client.call('mock.fail_next', {
            ...WEB,
            command: 'start',
            at_once: false
        })
        await operator.client.call('service.start', WEB)
        await until(operator, 'web', 'ERROR')

        expect(operator.states.web).toEqual({
            state: 'ERROR',
            error: 'mock service failed to start',
            changed_at: expect.any(Number)
        })
        await operator.client.call('service.refresh', WEB)
        expect(operator.states.web).toEqual({ state: 'OFF', changed_at: expect.any(Number) })
        operator.close()
    })

    // The answer and the document share one ordered stream: the ERROR is in the
    // document before the answer to the start that led to it.
    test('a start that fails at once is in ERROR by the time its answer arrives', async () => {
        const operator = await dashboard()
        await until(operator, 'web', 'OFF')
        await operator.client.call('mock.fail_next', {
            ...WEB,
            command: 'start',
            at_once: true
        })
        await operator.client.call('service.start', WEB)
        expect(operator.states.web?.state).toBe('ERROR')
        expect(operator.seen('web').slice(-3)).toEqual(['OFF', 'STARTING', 'ERROR'])
        await operator.client.call('service.refresh', WEB)
        operator.close()
    })

    test("the dashboard's page may open a socket, and no other page may", async () => {
        const opens = (origin: string): Promise<boolean> =>
            new Promise(resolve => {
                const socket = new WebSocket(`ws://${host}/v1/ws`, { headers: { Origin: origin } })
                socket.addEventListener('open', () => {
                    socket.close()
                    resolve(true)
                })
                socket.addEventListener('error', () => resolve(false))
            })
        expect(await opens(DASH_ORIGIN)).toBe(true)
        expect(await opens('https://evil.example')).toBe(false)
    })

    test('an argument the schema refuses comes back named', async () => {
        await expect(httpClient(KEY).call('mock.fail_next', { ...WEB, command: 'explode' } as never)).rejects.toMatchObject({
            message: 'INVALID_ARGUMENT',
            details: [{ field: 'command' }]
        })
    })

    test('arming a service the API does not serve is not found', async () => {
        await expect(
            httpClient(KEY).call('mock.fail_next', {
                service: 'db',
                command: 'start',
                at_once: false
            })
        ).rejects.toThrow(new ApiError('SERVICE_NOT_FOUND'))
    })
})
