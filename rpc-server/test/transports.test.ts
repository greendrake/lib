import { afterAll, describe, expect, test } from 'bun:test'
import { ApiError, HttpTransport, RpcClient, WebSocketPipe, WsTransport } from '@greendrake/rpc'
import type { Codec, WsAuthStrategy } from '@greendrake/rpc'
import { ConnRegistry, clientIp, createDispatcher, httpHandler, keyAuthResolver, method, websocketHandler } from '../src/main'
import type { AuthInfo, Connection, Methods } from '../src/main'
import { schema } from './standardSchema'

// The interop test: the server in this package, driven by the very client
// @greendrake/rpc ships. Nothing in between is a stand-in, so a change to
// either end that breaks the wire fails here.

const KEY = 's3cret'
const OPERATOR: AuthInfo = {
    userId: 'operator',
    isAdmin: true,
    phantom: false
}

interface ApiMethods {
    add: (args: { a: number; b: number }) => number
    whoami: () => unknown
    hang: () => never
}

interface Events {
    'thing.changed': { id: string }
}

let abortSeen: (reason: string) => void
const handlerAborted = new Promise<string>(resolve => {
    abortSeen = resolve
})

const addends = schema<{ a: number; b: number }>(value => {
    const { a, b } = (value ?? {}) as { a?: unknown; b?: unknown }
    return typeof a === 'number' && typeof b === 'number' ? { value: { a, b } } : { issues: [{ message: 'two numbers, please' }] }
})

const methods: Methods = {
    add: method({
        auth: 'none',
        args: addends,
        handler: (_ctx, args) => args.a + args.b
    }),
    whoami: { auth: 'user', handler: ctx => ctx.auth.userId },
    hang: {
        auth: 'none',
        handler: ctx =>
            new Promise<never>(() => {
                ctx.signal.addEventListener('abort', () => abortSeen('gone'), { once: true })
            })
    }
}

const registry = new ConnRegistry()
const dispatcher = createDispatcher(methods, { auth: keyAuthResolver(KEY, OPERATOR) })
const api = httpHandler(dispatcher)
// The sockets as the server sees them, so a test can cut one the way a lost
// link does rather than closing it politely from the client.
const serverSockets = new Set<Connection>()
const ws = websocketHandler(dispatcher, registry, {
    onOpen: conn => serverSockets.add(conn),
    onClose: conn => serverSockets.delete(conn)
})

const server = Bun.serve({
    port: 0,
    websocket: ws.handlers,
    async fetch(request, srv) {
        const { pathname } = new URL(request.url)
        if (pathname === '/ws') {
            return (await ws.upgrade(request, srv)) ? undefined : new Response(null, { status: 400 })
        }
        return api(request, clientIp(request, srv))
    }
})

// The handshake @greendrake/rpc's WsTransport performs, as an application
// declares it: which methods make up the bind is protocol, not transport.
const AUTH_STRATEGY: WsAuthStrategy = {
    authenticate: (send, token) => send({ method: 'auth.token', arguments: [token] }),
    deauthenticate: send => send({ method: 'auth.logout' })
}

// A second handler over a resolver that takes its time, for the ordering test
// below.
const slowWs = websocketHandler(
    createDispatcher(methods, {
        auth: {
            resolveToken: async token => {
                await Bun.sleep(30)
                return token === KEY ? OPERATOR : null
            }
        }
    }),
    new ConnRegistry()
)

const httpUrl = `http://localhost:${server.port}/api`
const wsUrl = `ws://localhost:${server.port}/ws`

afterAll(() => void server.stop(true))

// Long enough for a frame to cross the loopback and be dispatched.
const settle = (): Promise<void> => new Promise(resolve => setTimeout(resolve, 20))

describe('HTTP transport', () => {
    test('a call over the client the browser uses', async () => {
        const client = new RpcClient<ApiMethods>(new HttpTransport({ url: httpUrl }))
        expect(await client.call('add', { a: 2, b: 3 })).toBe(5)
    })

    test('a bearer credential names the caller', async () => {
        const client = new RpcClient<ApiMethods>(new HttpTransport({ url: httpUrl, headers: () => ({ Authorization: `Bearer ${KEY}` }) }))
        expect(await client.call('whoami')).toBe('operator')
    })

    test('an unrecognised credential leaves the caller anonymous', async () => {
        const client = new RpcClient<ApiMethods>(new HttpTransport({ url: httpUrl, headers: () => ({ Authorization: 'Bearer nope' }) }))
        await expect(client.call('whoami')).rejects.toThrow(new ApiError('AUTH_REQUIRED'))
    })

    test('only POST carries a call', async () => {
        expect((await fetch(httpUrl)).status).toBe(405)
    })

    test('a body that is not JSON is answered, not dropped', async () => {
        const response = await fetch(httpUrl, { method: 'POST', body: '{ not json' })
        expect(await response.json()).toEqual({ success: false, error: 'INVALID_JSON' })
    })

    test('a codec governs both directions', async () => {
        const reversed: Codec = { encode: s => [...s].reverse().join(''), decode: s => [...s].reverse().join('') }
        const codedApi = httpHandler(dispatcher, { codec: reversed })
        const coded = Bun.serve({ port: 0, fetch: (request, srv) => codedApi(request, clientIp(request, srv)) })
        const client = new RpcClient<ApiMethods>(new HttpTransport({ url: `http://localhost:${coded.port}/`, codec: reversed }))
        expect(await client.call('add', { a: 1, b: 1 })).toBe(2)
        await coded.stop(true)
    })
})

describe('WebSocket transport', () => {
    test('the handshake credential binds the connection', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl, queryAuth: { param: 'token', token: KEY } })
        const client = new RpcClient<ApiMethods>(new WsTransport({ pipe }))
        expect(await client.call('whoami')).toBe('operator')
        expect(await client.call('add', { a: 4, b: 4 })).toBe(8)
        pipe.dispose()
    })

    test('an in-band auth frame binds and unbinds, and the calls behind it see the result', async () => {
        // A resolver slow enough that a call issued right after the bind would
        // overtake it were the two not serialised — the gate's whole purpose,
        // and a race a blip in a real deployment would otherwise lose.
        const slow = Bun.serve({
            port: 0,
            websocket: slowWs.handlers,
            fetch: async (request, srv) => ((await slowWs.upgrade(request, srv)) ? undefined : new Response(null, { status: 400 }))
        })
        const pipe = new WebSocketPipe({ url: `ws://localhost:${slow.port}/` })
        const transport = new WsTransport({ pipe, auth: AUTH_STRATEGY })
        const client = new RpcClient<ApiMethods>(transport)
        await expect(client.call('whoami')).rejects.toThrow(new ApiError('AUTH_REQUIRED'))

        // Deliberately not awaited: the call goes out while the bind is still
        // being resolved, and must still be dispatched as the bound principal.
        void transport.authenticate(KEY)
        expect(await client.call('whoami')).toBe('operator')

        await transport.deauthenticate()
        await expect(client.call('whoami')).rejects.toThrow(new ApiError('AUTH_REQUIRED'))
        pipe.dispose()
        await slow.stop(true)
    })

    test('an auth frame on a service with no resolver says so', async () => {
        const bare = createDispatcher(methods)
        const bareWs = websocketHandler(bare, new ConnRegistry())
        const srv = Bun.serve({
            port: 0,
            websocket: bareWs.handlers,
            fetch: async (request, s) => ((await bareWs.upgrade(request, s)) ? undefined : new Response(null, { status: 400 }))
        })
        const pipe = new WebSocketPipe({ url: `ws://localhost:${srv.port}/` })
        const transport = new WsTransport({ pipe, auth: AUTH_STRATEGY })
        await expect(transport.authenticate(KEY)).rejects.toThrow(new ApiError('AUTH_NOT_CONFIGURED'))
        pipe.dispose()
        await srv.stop(true)
    })

    test('pushes reach the connection the event names, and every connection on a broadcast', async () => {
        const bound = new WebSocketPipe({ url: wsUrl, queryAuth: { param: 'token', token: KEY } })
        const stranger = new WebSocketPipe({ url: wsUrl })
        const toOperator = new WsTransport<Events>({ pipe: bound })
        const toStranger = new WsTransport<Events>({ pipe: stranger })
        await Promise.all([toOperator.connect(), toStranger.connect()])

        const seenByOperator: string[] = []
        const seenByStranger: string[] = []
        toOperator.onEvent('thing.changed', data => seenByOperator.push(data.id))
        toStranger.onEvent('thing.changed', data => seenByStranger.push(data.id))

        registry.emitToUser('operator', 'thing.changed', { id: 'addressed' })
        registry.broadcastEvent('thing.changed', { id: 'everyone' })
        await settle()

        expect(seenByOperator).toEqual(['addressed', 'everyone'])
        expect(seenByStranger).toEqual(['everyone'])
        bound.dispose()
        stranger.dispose()
    })

    test('a socket that drops and comes back is the same principal again', async () => {
        const pipe = new WebSocketPipe({
            url: wsUrl,
            queryAuth: { param: 'token', token: KEY },
            reconnectDelayMs: () => 0
        })
        const client = new RpcClient<ApiMethods>(new WsTransport({ pipe }))
        expect(await client.call('whoami')).toBe('operator')

        const reconnected = new Promise<void>(resolve => pipe.on('open', () => resolve()))
        serverSockets.forEach(conn => conn.terminate())
        await reconnected

        expect(await client.call('whoami')).toBe('operator')
        pipe.dispose()
    })

    test('a handler learns its caller has gone', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl })
        const client = new RpcClient<ApiMethods>(new WsTransport({ pipe }))
        void client.call('hang').catch(() => {})
        await settle()
        pipe.dispose()
        expect(await handlerAborted).toBe('gone')
    })
})
