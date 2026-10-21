import { afterAll, beforeAll, describe, expect, test } from 'bun:test'
import { ApiError, HttpTransport, RpcClient, TOKEN_PARAM, WebSocketPipe, WsTransport, liveDocument } from '@greendrake/rpc'
import type { RunningService } from '@greendrake/server'
import type { ConnectionData } from '@greendrake/rpc-server'
import { serviceStateDocument } from '@greendrake/service-state'
import type { ServiceDocument, ServiceState, ServiceStatePushes, ServiceStatus } from '@greendrake/service-state'
import { startApi } from '../src/service'
import { MOCK_SERVICES } from '../src/mock'
import type { ApiMethods, Operator } from '../src/methods'

const KEY = 'test-admin-key'
const DASH_ORIGIN = 'http://localhost:5180'
// The service most calls name, and the one beside it that they leave alone.
const WEB = { service: 'web' }
const WORKER = { service: 'worker' }
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

// How many times each service has been checked so far.
const reads = (): Promise<number[]> => Promise.all([httpClient(KEY).call('mock.reads', WEB), httpClient(KEY).call('mock.reads', WORKER)])

interface Dashboard {
    client: RpcClient<ApiMethods>
    // The service's status, as the socket keeps its document.
    status: (name: string) => ServiceStatus | undefined
    // What the dashboard would have shown for a service, frame by frame: its
    // state, or null while it has no status.
    seen: (name: string) => (ServiceState | null)[]
    // Called after each frame of any service's document has been applied;
    // returns the release.
    onFrame: (listener: () => void) => () => void
    close: () => void
}

// A dashboard's socket: calls and the documents over one connection,
// authenticated at the handshake. It watches nothing until it calls
// service.watch.
const dashboard = async (): Promise<Dashboard> => {
    const pipe = new WebSocketPipe({ url: `ws://${host}/v1/ws`, queryAuth: { param: TOKEN_PARAM, token: KEY } })
    const transport = new WsTransport<Record<never, never>, ServiceStatePushes>({ pipe })
    const kept = new Map<string, { doc: ServiceDocument; frames: (ServiceState | null)[] }>()
    const listeners = new Set<() => void>()
    for (const name of MOCK_SERVICES) {
        const entry: { doc: ServiceDocument; frames: (ServiceState | null)[] } = { doc: {}, frames: [] }
        kept.set(name, entry)
        liveDocument(transport, serviceStateDocument(name), entry.doc)
        // After liveDocument's, so the frame has been applied.
        transport.onData(serviceStateDocument(name), () => {
            entry.frames.push(entry.doc.status?.state ?? null)
            listeners.forEach(listener => listener())
        })
    }
    // A dashboard that only watches never sends anything, and the pipe
    // connects on demand — so the socket has to be asked for.
    await transport.connect()
    return {
        client: new RpcClient<ApiMethods>(transport),
        status: name => kept.get(name)?.doc.status,
        seen: name => (kept.get(name)?.frames ?? []).filter((state, i, all) => i === 0 || state !== all[i - 1]),
        onFrame: listener => {
            listeners.add(listener)
            return () => listeners.delete(listener)
        },
        close: () => pipe.dispose()
    }
}

// The document showing a service in a state: now, or once the frame that
// brings it has been applied. One that never comes fails on the test's own
// timeout.
const until = (at: Dashboard, name: string, state: ServiceState): Promise<void> =>
    new Promise(resolve => {
        const reached = (): boolean => at.status(name)?.state === state
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

    test('a connected socket that has not watched has nothing read', async () => {
        const before = await reads()
        const idle = await dashboard()
        // Answered, the socket is the server's.
        await idle.client.call('mock.reads', WEB)
        expect(await reads()).toEqual(before)
        idle.close()
    })

    // A reading taken with nobody watching is kept until it turns MAX_AGE_MS
    // old, so the first socket to watch is greeted with it.
    test('watching a service just read is greeted with the reading, has nothing read, and leaves the service beside it alone', async () => {
        await httpClient(KEY).call('service.refresh', WEB)
        const before = await reads()
        const watcher = await dashboard()
        await watcher.client.call('service.watch', WEB)
        // Greeted ahead of the answer.
        expect(watcher.seen('web')).toEqual(['OFF'])
        expect(await reads()).toEqual(before)
        expect(watcher.seen('worker')).toEqual([])
        watcher.close()
    })

    test('a full round trip, with every state change sent to every socket watching', async () => {
        const [operator, onlooker] = await Promise.all([dashboard(), dashboard()])
        await Promise.all([operator.client.call('service.watch', WEB), onlooker.client.call('service.watch', WEB)])
        await until(operator, 'web', 'OFF')
        await until(onlooker, 'web', 'OFF')

        expect(await operator.client.call('service.start', WEB)).toBeUndefined()
        await until(operator, 'web', 'ON')
        await operator.client.call('service.stop', WEB)
        await until(operator, 'web', 'OFF')
        await until(onlooker, 'web', 'OFF')

        expect(operator.seen('web').slice(-5)).toEqual(['OFF', 'STARTING', 'ON', 'STOPPING', 'OFF'])
        // The onlooker asked for nothing but to watch, and saw the same thing:
        // a service has one state, and it is everybody's.
        expect(onlooker.seen('web').slice(-5)).toEqual(['OFF', 'STARTING', 'ON', 'STOPPING', 'OFF'])
        operator.close()
        onlooker.close()
    })

    test('a transition that fails leaves ERROR carrying why, and a refresh reads past it', async () => {
        const operator = await dashboard()
        await operator.client.call('service.watch', WEB)
        await until(operator, 'web', 'OFF')
        await operator.client.call('mock.fail_next', {
            ...WEB,
            command: 'start',
            at_once: false
        })
        await operator.client.call('service.start', WEB)
        await until(operator, 'web', 'ERROR')

        expect(operator.status('web')).toEqual({
            state: 'ERROR',
            error: 'mock service failed to start',
            changed_at: expect.any(Number)
        })
        await operator.client.call('service.refresh', WEB)
        expect(operator.status('web')).toEqual({ state: 'OFF', changed_at: expect.any(Number) })
        operator.close()
    })

    // The answer and the document share one ordered stream: the ERROR is in the
    // document before the answer to the start that led to it.
    test('a start that fails at once is in ERROR by the time its answer arrives', async () => {
        const operator = await dashboard()
        await operator.client.call('service.watch', WEB)
        await until(operator, 'web', 'OFF')
        await operator.client.call('mock.fail_next', {
            ...WEB,
            command: 'start',
            at_once: true
        })
        await operator.client.call('service.start', WEB)
        expect(operator.status('web')?.state).toBe('ERROR')
        expect(operator.seen('web').slice(-3)).toEqual(['OFF', 'STARTING', 'ERROR'])
        await operator.client.call('service.refresh', WEB)
        operator.close()
    })

    test('a socket that watched and dropped, replaced by one that watches again, is greeted with the document afresh', async () => {
        const dropped = await dashboard()
        await dropped.client.call('service.watch', WEB)
        await until(dropped, 'web', 'OFF')
        dropped.close()

        const replacement = await dashboard()
        await replacement.client.call('service.watch', WEB)
        await until(replacement, 'web', 'OFF')
        replacement.close()
    })

    test('watching needs a WebSocket', async () => {
        await expect(httpClient(KEY).call('service.watch', WEB)).rejects.toThrow(new ApiError('WEBSOCKET_REQUIRED'))
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

    test('a mocked service the API does not serve is not found', async () => {
        await expect(
            httpClient(KEY).call('mock.fail_next', {
                service: 'db',
                command: 'start',
                at_once: false
            })
        ).rejects.toThrow(new ApiError('SERVICE_NOT_FOUND'))
        await expect(httpClient(KEY).call('mock.reads', { service: 'db' })).rejects.toThrow(new ApiError('SERVICE_NOT_FOUND'))
    })
})
