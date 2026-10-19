import { afterAll, describe, expect, test } from 'bun:test'
import type { Server } from 'bun'
import { ApiError, HttpTransport, RpcClient, WebSocketPipe, WsTransport, tokenAuth } from '@greendrake/rpc'
import type { Codec, WsAuthStrategy } from '@greendrake/rpc'
import { ConnRegistry, createDispatcher, httpHandler, keyAuthResolver, method, topicsOf, trustProxy, websocketHandler } from '../src/main'
import type { Connection, ConnectionData, Methods, RpcWebSocket } from '../src/main'
import { schema } from './standardSchema'

// The interop test: the server in this package, driven by the very client
// @greendrake/rpc ships. Nothing in between is a stand-in, so a change to
// either end that breaks the wire fails here.

const KEY = 's3cret'
// The principal the key names: a user id, which is its own key in the
// registry.
const OPERATOR = 'operator'

interface ApiMethods {
    add: (args: { a: number; b: number }) => number
    whoami: () => string
    whereFrom: () => string
    hang: (args: { label: string }) => never
    watch: (args: { topic: string }) => void
    unwatch: (args: { topic: string }) => void
    'watch.later': (args: { topic: string }) => void
}

interface Events {
    'thing.changed': { id: string }
}

interface Subscriber {
    client: RpcClient<ApiMethods>
    pipe: WebSocketPipe
    // The ids of every `thing.changed` pushed to it, in order.
    seen: string[]
}

// Settled when the `hang` call labelled so learns its caller has gone or
// aborted it.
const abortWaiters = new Map<string, () => void>()
const aborted = (label: string): Promise<void> => new Promise(resolve => abortWaiters.set(label, resolve))

// Released by the test that needs a handler to join only after its caller has
// gone.
let allowJoin: () => void
const joinAllowed = new Promise<void>(resolve => {
    allowJoin = resolve
})

const addends = schema<{ a: number; b: number }>(value => {
    const { a, b } = (value ?? {}) as { a?: unknown; b?: unknown }
    return typeof a === 'number' && typeof b === 'number' ? { value: { a, b } } : { issues: [{ message: 'two numbers, please' }] }
})

const named = schema<{ topic: string }>(value => {
    const topic = (value as { topic?: unknown } | undefined)?.topic
    return typeof topic === 'string' ? { value: { topic } } : { issues: [{ message: 'a topic, please' }] }
})

const labelled = schema<{ label: string }>(value => {
    const label = (value as { label?: unknown } | undefined)?.label
    return typeof label === 'string' ? { value: { label } } : { issues: [{ message: 'a label, please' }] }
})

const methods: Methods<string> = {
    add: method({
        auth: 'none',
        args: addends,
        handler: (_ctx, args) => args.a + args.b
    }),
    whoami: { auth: 'required', handler: ctx => ctx.principal },
    whereFrom: { auth: 'none', handler: ctx => ctx.clientIp },
    hang: method({
        auth: 'none',
        args: labelled,
        handler: (ctx, { label }) =>
            new Promise<never>(() => {
                ctx.signal.addEventListener('abort', () => abortWaiters.get(label)?.(), { once: true })
            })
    }),
    watch: method({
        auth: 'none',
        args: named,
        handler: (ctx, args) => topicsOf(ctx).join(args.topic)
    }),
    unwatch: method({
        auth: 'none',
        args: named,
        handler: (ctx, args) => topicsOf(ctx).leave(args.topic)
    }),
    'watch.later': method({
        auth: 'none',
        args: named,
        handler: async (ctx, args) => {
            await joinAllowed
            topicsOf(ctx).join(args.topic)
        }
    })
}

const registry = new ConnRegistry<string>()
const dispatcher = createDispatcher(methods, { auth: keyAuthResolver(KEY, OPERATOR) })
const api = httpHandler(dispatcher)
// The sockets as the server sees them, so a test can cut one the way a lost
// link does rather than closing it politely from the client.
const serverSockets = new Set<Connection<string>>()
const ws = websocketHandler(dispatcher, registry, {
    onOpen: conn => serverSockets.add(conn),
    onClose: conn => serverSockets.delete(conn)
})

const server = Bun.serve({
    port: 0,
    websocket: ws.handlers,
    fetch: (request, srv) => (new URL(request.url).pathname === '/ws' ? ws.upgrade(request, srv) : api(request, srv))
})

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
    new ConnRegistry<string>()
)

// A server that is one WebSocket handler and nothing else, for the tests that
// need a dispatcher or a registry of their own.
const wsServer = <P>(handler: RpcWebSocket<P>): Server<ConnectionData<P>> =>
    Bun.serve({
        port: 0,
        websocket: handler.handlers,
        fetch: (request, srv) => handler.upgrade(request, srv)
    })

const httpUrl = `http://localhost:${server.port}/api`
const wsUrl = `ws://localhost:${server.port}/ws`

afterAll(() => void server.stop(true))

// Long enough for a frame to cross the loopback and be dispatched.
const settle = (): Promise<void> => new Promise(resolve => setTimeout(resolve, 20))

// A socket speaking the wire by hand, for frames the client never sends.
const rawSocket = async (url: string): Promise<{ send: (frame: object) => void; response: (id: string) => Promise<unknown>; received: unknown[]; close: () => void }> => {
    const socket = new WebSocket(url)
    const received: { id?: string }[] = []
    const waiting = new Map<string, (frame: unknown) => void>()
    socket.addEventListener('message', event => {
        const frame = JSON.parse(String(event.data)) as { id?: string }
        received.push(frame)
        if (frame.id !== undefined) {
            waiting.get(frame.id)?.(frame)
        }
    })
    await new Promise(resolve => socket.addEventListener('open', resolve, { once: true }))
    return {
        send: frame => socket.send(JSON.stringify(frame)),
        response: id => new Promise(resolve => waiting.set(id, resolve)),
        received,
        close: () => socket.close()
    }
}

describe('HTTP transport', () => {
    test('a call over the client the browser uses', async () => {
        const client = new RpcClient<ApiMethods>(new HttpTransport({ url: httpUrl }))
        expect(await client.call('add', { a: 2, b: 3 })).toBe(5)
    })

    test('a bearer credential names the caller', async () => {
        const client = new RpcClient<ApiMethods>(new HttpTransport({ url: httpUrl, headers: () => ({ Authorization: `Bearer ${KEY}` }) }))
        expect(await client.call('whoami')).toBe(OPERATOR)
    })

    test.each([
        ['an unrecognised credential', `Bearer nope`],
        ['a bare key, which is no credential', KEY]
    ])('%s leaves the caller anonymous', async (_name, authorization) => {
        const client = new RpcClient<ApiMethods>(new HttpTransport({ url: httpUrl, headers: () => ({ Authorization: authorization }) }))
        await expect(client.call('whoami')).rejects.toThrow(new ApiError('AUTH_REQUIRED'))
    })

    test('the credential is wherever the application says', async () => {
        const custom = httpHandler(dispatcher, { credential: request => request.headers.get('x-api-key') ?? '' })
        const srv = Bun.serve({ port: 0, fetch: (request, s) => custom(request, s) })
        const client = new RpcClient<ApiMethods>(new HttpTransport({ url: `http://localhost:${srv.port}/`, headers: () => ({ 'X-Api-Key': KEY }) }))
        expect(await client.call('whoami')).toBe(OPERATOR)
        await srv.stop(true)
    })

    test('a method that subscribes its caller is refused, there being no socket to push to', async () => {
        const client = new RpcClient<ApiMethods>(new HttpTransport({ url: httpUrl }))
        await expect(client.call('watch', { topic: 'anything' })).rejects.toThrow(new ApiError('WEBSOCKET_REQUIRED'))
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
        const coded = Bun.serve({ port: 0, fetch: (request, srv) => codedApi(request, srv) })
        const client = new RpcClient<ApiMethods>(new HttpTransport({ url: `http://localhost:${coded.port}/`, codec: reversed }))
        expect(await client.call('add', { a: 1, b: 1 })).toBe(2)
        await coded.stop(true)
    })
})

describe('client address', () => {
    const forwardedFrom = (url: string): RpcClient<ApiMethods> => new RpcClient<ApiMethods>(new HttpTransport({ url, headers: () => ({ 'X-Forwarded-For': '6.6.6.6, 203.0.113.9' }) }))

    test("is the socket's, whatever a header claims", async () => {
        expect(await forwardedFrom(httpUrl).call('whereFrom')).toMatch(/^(127\.0\.0\.1|::1|::ffff:127\.0\.0\.1)$/)
    })

    test('is what the proxy in front saw, behind one the application trusts', async () => {
        const proxied = httpHandler(createDispatcher(methods, { auth: keyAuthResolver(KEY, OPERATOR), clientIp: trustProxy }))
        const srv = Bun.serve({ port: 0, fetch: (request, s) => proxied(request, s) })
        expect(await forwardedFrom(`http://localhost:${srv.port}/`).call('whereFrom')).toBe('203.0.113.9')
        await srv.stop(true)
    })
})

describe('WebSocket transport', () => {
    test('the handshake credential binds the connection', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl, queryAuth: { param: 'token', token: KEY } })
        const client = new RpcClient<ApiMethods>(new WsTransport({ pipe }))
        expect(await client.call('whoami')).toBe(OPERATOR)
        expect(await client.call('add', { a: 4, b: 4 })).toBe(8)
        pipe.dispose()
    })

    test('an in-band auth frame binds and unbinds, and the calls behind it see the result', async () => {
        // A resolver slow enough that a call issued right after the bind would
        // overtake it were the two not serialised — the gate's whole purpose,
        // and a race a blip in a real deployment would otherwise lose.
        const slow = wsServer(slowWs)
        const pipe = new WebSocketPipe({ url: `ws://localhost:${slow.port}/` })
        const transport = new WsTransport({ pipe, auth: tokenAuth })
        const client = new RpcClient<ApiMethods>(transport)
        await expect(client.call('whoami')).rejects.toThrow(new ApiError('AUTH_REQUIRED'))

        // Deliberately not awaited: the call goes out while the bind is still
        // being resolved, and must still be dispatched as the bound principal.
        void transport.authenticate(KEY)
        expect(await client.call('whoami')).toBe(OPERATOR)

        await transport.deauthenticate()
        await expect(client.call('whoami')).rejects.toThrow(new ApiError('AUTH_REQUIRED'))
        pipe.dispose()
        await slow.stop(true)
    })

    test('a token naming nobody does not bind', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl })
        const transport = new WsTransport({ pipe, auth: tokenAuth })
        await expect(transport.authenticate('nope')).rejects.toThrow(new ApiError('UNAUTHENTICATED'))
        pipe.dispose()
    })

    test('the handshake parameter and the auth frames are the application’s to name', async () => {
        const renamed = wsServer(websocketHandler(dispatcher, new ConnRegistry<string>(), { tokenParam: 'key', authFrames: { token: 'session.bind', logout: 'session.unbind' } }))
        const url = `ws://localhost:${renamed.port}/`

        const byParam = new WebSocketPipe({ url, queryAuth: { param: 'key', token: KEY } })
        expect(await new RpcClient<ApiMethods>(new WsTransport({ pipe: byParam })).call('whoami')).toBe(OPERATOR)
        byParam.dispose()

        const sessionAuth: WsAuthStrategy = {
            authenticate: (send, token) => send({ method: 'session.bind', arguments: [token] }),
            deauthenticate: send => send({ method: 'session.unbind' })
        }
        const byFrame = new WebSocketPipe({ url })
        const transport = new WsTransport({ pipe: byFrame, auth: sessionAuth })
        await transport.authenticate(KEY)
        expect(await new RpcClient<ApiMethods>(transport).call('whoami')).toBe(OPERATOR)
        // The wire's own names are, here, methods like any other: unknown ones.
        await expect(transport.send({ method: 'auth.logout' })).rejects.toThrow(new ApiError('UNKNOWN_METHOD'))
        byFrame.dispose()
        await renamed.stop(true)
    })

    test('an auth frame on a service with no resolver says so', async () => {
        const srv = wsServer(websocketHandler(createDispatcher({}), new ConnRegistry()))
        const pipe = new WebSocketPipe({ url: `ws://localhost:${srv.port}/` })
        const transport = new WsTransport({ pipe, auth: tokenAuth })
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

        registry.emitToUser(OPERATOR, 'thing.changed', { id: 'addressed' })
        registry.broadcastEvent('thing.changed', { id: 'everyone' })
        await settle()

        expect(seenByOperator).toEqual(['addressed', 'everyone'])
        expect(seenByStranger).toEqual(['everyone'])
        bound.dispose()
        stranger.dispose()
    })

    test('an object principal is filed under the key the application names', async () => {
        // @ts-expect-error: a fresh object for every socket would file each apart.
        void new ConnRegistry<{ account: string }>()
        const accounts = new ConnRegistry<{ account: string }>({ userKey: principal => principal.account })
        const srv = wsServer(websocketHandler(createDispatcher({}, { auth: keyAuthResolver(KEY, { account: 'acc-1' }) }), accounts))
        const pipe = new WebSocketPipe({ url: `ws://localhost:${srv.port}/`, queryAuth: { param: 'token', token: KEY } })
        const transport = new WsTransport<Events>({ pipe })
        const seen: string[] = []
        transport.onEvent('thing.changed', data => seen.push(data.id))
        await transport.connect()
        await settle()

        accounts.emitToUser('acc-1', 'thing.changed', { id: 'for acc-1' })
        await settle()
        expect(seen).toEqual(['for acc-1'])
        pipe.dispose()
        await srv.stop(true)
    })

    test('the registry says when its first socket opens and when its last one closes, and only then', async () => {
        // A registry and a listener of its own: the file's shared one has
        // other tests' sockets coming and going.
        const audience = new ConnRegistry<string>()
        const presentServer = wsServer(websocketHandler(dispatcher, audience))
        const seen: boolean[] = []
        const stop = audience.onPresence(present => seen.push(present))
        const connect = async (): Promise<WsTransport<Events>> => {
            const transport = new WsTransport<Events>({ pipe: new WebSocketPipe({ url: `ws://localhost:${presentServer.port}/` }), auth: tokenAuth })
            await transport.connect()
            await settle()
            return transport
        }

        const first = await connect()
        // Binding and unbinding the one socket open registers it again, which
        // is not its arriving.
        await first.authenticate(KEY)
        await first.deauthenticate()
        expect(seen).toEqual([true])
        const second = await connect()
        expect(audience.size).toBe(2)
        expect(seen).toEqual([true])

        first.pipe.dispose()
        await settle()
        expect(seen).toEqual([true])
        second.pipe.dispose()
        await settle()
        expect(seen).toEqual([true, false])

        stop()
        const third = await connect()
        expect(seen).toEqual([true, false])
        third.pipe.dispose()
        await presentServer.stop(true)
    })

    test('a socket that drops and comes back is the same principal again', async () => {
        const pipe = new WebSocketPipe({
            url: wsUrl,
            queryAuth: { param: 'token', token: KEY },
            reconnectDelayMs: () => 0
        })
        const client = new RpcClient<ApiMethods>(new WsTransport({ pipe }))
        expect(await client.call('whoami')).toBe(OPERATOR)

        const reconnected = new Promise<void>(resolve => pipe.on('open', () => resolve()))
        serverSockets.forEach(conn => conn.terminate())
        await reconnected

        expect(await client.call('whoami')).toBe(OPERATOR)
        pipe.dispose()
    })

    test('a handler learns its caller has gone', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl })
        const client = new RpcClient<ApiMethods>(new WsTransport({ pipe }))
        const gone = aborted('gone')
        void client.call('hang', { label: 'gone' }).catch(() => {})
        await settle()
        pipe.dispose()
        await gone
    })
})

describe('aborts', () => {
    // A call that runs until it is aborted, as the frame `1` carrying the nonce
    // `n1`: settled `label` when its handler learns it was.
    const hanging = async (label: string): Promise<{ socket: Awaited<ReturnType<typeof rawSocket>>; cancelled: Promise<void> }> => {
        const socket = await rawSocket(wsUrl)
        const cancelled = aborted(label)
        socket.send({
            id: '1',
            method: 'hang',
            nonce: 'n1',
            arguments: [{ label }]
        })
        await settle()
        return { socket, cancelled }
    }

    test('an abort cancels the request its nonce names, which is never answered, and is answered with nothing itself', async () => {
        const { socket, cancelled } = await hanging('debounced')
        socket.send({ abort: 'n1' })
        await cancelled
        await settle()
        expect(socket.received).toEqual([])
        socket.close()
    })

    test('one frame aborts a request and makes the next', async () => {
        const { socket, cancelled } = await hanging('superseded')
        const answer = socket.response('2')
        socket.send({
            abort: 'n1',
            id: '2',
            method: 'add',
            nonce: 'n2',
            arguments: [{ a: 1, b: 2 }]
        })
        await cancelled
        expect(await answer).toEqual({
            type: 'response',
            id: '2',
            success: true,
            data: 3
        })
        socket.close()
    })

    test('an abort naming nothing in flight changes nothing', async () => {
        const socket = await rawSocket(wsUrl)
        const answer = socket.response('1')
        socket.send({
            abort: 'unheard-of',
            id: '1',
            method: 'add',
            arguments: [{ a: 2, b: 2 }]
        })
        expect(await answer).toMatchObject({ success: true, data: 4 })
        socket.close()
    })
})

describe('origins', () => {
    // Whether a browser page on `origin` gets a socket.
    const opens = (url: string, origin: string): Promise<boolean> =>
        new Promise(resolve => {
            const socket = new WebSocket(url, { headers: { Origin: origin } })
            socket.addEventListener('open', () => {
                socket.close()
                resolve(true)
            })
            socket.addEventListener('error', () => resolve(false))
        })

    test("a page of the endpoint's own origin is admitted, and any other refused", async () => {
        expect(await opens(wsUrl, `http://localhost:${server.port}`)).toBe(true)
        expect(await opens(wsUrl, 'https://evil.example')).toBe(false)
    })

    test('the refusal is a 403, before any credential is looked at', async () => {
        const response = await fetch(`${wsUrl.replace('ws:', 'http:')}?token=${KEY}`, {
            headers: {
                Origin: 'https://evil.example',
                Connection: 'Upgrade',
                Upgrade: 'websocket'
            }
        })
        expect(response.status).toBe(403)
    })

    test('the origins the application lists are admitted too', async () => {
        const listed = wsServer(websocketHandler(dispatcher, new ConnRegistry<string>(), { origins: ['https://dash.example.com'] }))
        const url = `ws://localhost:${listed.port}/`
        expect(await opens(url, 'https://dash.example.com')).toBe(true)
        expect(await opens(url, 'https://evil.example')).toBe(false)
        await listed.stop(true)
    })

    test('a request that is no WebSocket handshake is a 400', async () => {
        expect((await fetch(wsUrl.replace('ws:', 'http:'))).status).toBe(400)
    })
})

describe('topics', () => {
    // A registry of their own: membership is asserted whole, and the file's
    // shared registry has other tests' sockets in it.
    const topicRegistry = new ConnRegistry<string>()
    const topicServer = wsServer(websocketHandler(dispatcher, topicRegistry))
    afterAll(() => void topicServer.stop(true))

    // A socket that subscribes itself through the methods above, and what it
    // has been pushed.
    const subscriber = async (token?: string): Promise<Subscriber> => {
        const pipe = new WebSocketPipe({ url: `ws://localhost:${topicServer.port}/`, ...(token === undefined ? {} : { queryAuth: { param: 'token', token } }) })
        const transport = new WsTransport<Events>({ pipe })
        const seen: string[] = []
        transport.onEvent('thing.changed', data => seen.push(data.id))
        await transport.connect()
        return {
            client: new RpcClient<ApiMethods>(transport),
            pipe,
            seen
        }
    }

    const publish = async (topic: string, id: string): Promise<void> => {
        topicRegistry.publish(topic, 'thing.changed', { id })
        await settle()
    }

    test('a publish reaches exactly the sockets in its topic, whoever they are bound as', async () => {
        const [operator, stranger, elsewhere, idle] = await Promise.all([subscriber(KEY), subscriber(), subscriber(), subscriber()])
        await operator.client.call('watch', { topic: 'a' })
        await stranger.client.call('watch', { topic: 'a' })
        await elsewhere.client.call('watch', { topic: 'b' })
        expect(topicRegistry.topics()).toEqual(new Set(['a', 'b']))

        await publish('a', 'for a')
        await publish('c', 'for nobody')

        expect(operator.seen).toEqual(['for a'])
        expect(stranger.seen).toEqual(['for a'])
        expect(elsewhere.seen).toEqual([])
        expect(idle.seen).toEqual([])
        for (const one of [operator, stranger, elsewhere, idle]) one.pipe.dispose()
        await settle()
        expect(topicRegistry.topics()).toEqual(new Set())
    })

    test('leaving a topic and closing the socket each end a membership', async () => {
        const [leaver, stayer] = await Promise.all([subscriber(), subscriber()])
        await leaver.client.call('watch', { topic: 'a' })
        await leaver.client.call('watch', { topic: 'b' })
        await stayer.client.call('watch', { topic: 'a' })

        await leaver.client.call('unwatch', { topic: 'b' })
        // Leaving what it is not in changes nothing.
        await stayer.client.call('unwatch', { topic: 'b' })
        expect(topicRegistry.topics()).toEqual(new Set(['a']))
        await publish('b', 'for b')
        expect(leaver.seen).toEqual([])

        leaver.pipe.dispose()
        await settle()
        expect(topicRegistry.topics()).toEqual(new Set(['a']))
        await publish('a', 'for a')
        expect(stayer.seen).toEqual(['for a'])

        stayer.pipe.dispose()
        await settle()
        expect(topicRegistry.topics()).toEqual(new Set())
    })

    test('a handler that joins after its caller has gone joins nothing', async () => {
        const late = await subscriber()
        void late.client.call('watch.later', { topic: 'late' }).catch(() => {})
        await settle()
        late.pipe.dispose()
        await settle()
        allowJoin()
        await settle()
        expect(topicRegistry.topics()).toEqual(new Set())
    })
})
