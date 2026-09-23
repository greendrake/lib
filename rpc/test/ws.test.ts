import { afterAll, describe, expect, test } from 'bun:test'
import { RpcClient, WebSocketClosedError, WebSocketPipe, WsTransport } from '../src/main'
import type { RpcRequest } from '../src/main'
import { nextTick, startWsServer } from './wsServer'

interface Methods {
    'auth.token': (token: string) => { user: string }
    'auth.logout': () => void
    add: (a: number, b: number) => number
    ping: () => void
    hang: () => never
}

interface Events {
    'things.updated': { count: number }
}

interface DataPushes {
    telemetry: { volts: number }
}

const server = startWsServer()
const { authCalls, broadcast, killAllSockets } = server
const wsUrl = server.url

afterAll(server.stop)

describe('WsTransport', () => {
    test('call correlation over one socket', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl })
        const client = new RpcClient<Methods>(new WsTransport({ pipe }))
        const [a, b] = await Promise.all([client.call('add', 1, 2), client.call('add', 10, 20)])
        expect(a).toBe(3)
        expect(b).toBe(30)
        pipe.dispose()
    })

    test('typed push events and data frames', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl })
        const transport = new WsTransport<Events, DataPushes>({ pipe })
        await transport.connect()

        const events: number[] = []
        const volts: number[] = []
        const offEvent = transport.onEvent('things.updated', data => events.push(data.count))
        transport.onData('telemetry', data => volts.push(data.volts))

        broadcast({
            type: 'event',
            event: 'things.updated',
            data: { count: 5 }
        })
        broadcast({ type: 'telemetry', data: { volts: 48 } })
        await nextTick()
        expect(events).toEqual([5])
        expect(volts).toEqual([48])

        offEvent()
        broadcast({
            type: 'event',
            event: 'things.updated',
            data: { count: 6 }
        })
        await nextTick()
        expect(events).toEqual([5])
        pipe.dispose()
    })

    test('in-flight calls reject with WebSocketClosedError on drop', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10_000 })
        const client = new RpcClient<Methods>(new WsTransport({ pipe }))
        await pipe.ensureConnected()
        const hanging = client.call('hang')
        await nextTick()
        killAllSockets()
        await expect(hanging).rejects.toBeInstanceOf(WebSocketClosedError)
        pipe.dispose()
    })

    test('auth is applied once and re-applied on reconnect', async () => {
        authCalls.length = 0
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10 })
        const transport = new WsTransport<Record<never, never>, Record<never, never>>({
            pipe,
            auth: {
                authenticate: (send, token) => send({ method: 'auth.token', arguments: [token] }),
                deauthenticate: send => send({ method: 'auth.logout' })
            }
        })
        await transport.authenticate('tok-1')
        expect(authCalls).toEqual(['tok-1'])

        killAllSockets()
        // wait for reconnect + re-auth
        await new Promise(resolve => setTimeout(resolve, 100))
        expect(authCalls).toEqual(['tok-1', 'tok-1'])
        pipe.dispose()
    })

    // The auth strategy every identity test below drives.
    const tokenAuth = {
        authenticate: (send: (payload: RpcRequest) => Promise<unknown>, token: string) => send({ method: 'auth.token', arguments: [token] }),
        deauthenticate: (send: (payload: RpcRequest) => Promise<unknown>) => send({ method: 'auth.logout' })
    }

    test('a refused bind still re-applies the identity on reconnect', async () => {
        authCalls.length = 0
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10 })
        const transport = new WsTransport<Record<never, never>, Record<never, never>>({ pipe, auth: tokenAuth })
        await expect(transport.authenticate('bad-1')).rejects.toThrow()
        expect(authCalls).toEqual(['bad-1'])

        killAllSockets()
        await new Promise(resolve => setTimeout(resolve, 100))
        // The credential states who the transport is, so the replacement socket
        // applies it too: a server that refused one attempt (or a bind that
        // never reached a server at all) is not grounds to forget the identity
        // and leave every later connection anonymous.
        expect(authCalls).toEqual(['bad-1', 'bad-1'])
        pipe.dispose()
    })

    test('re-binding the live identity sends no second frame, a new one does', async () => {
        authCalls.length = 0
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10 })
        const transport = new WsTransport<Record<never, never>, Record<never, never>>({ pipe, auth: tokenAuth })
        await transport.authenticate('tok-1')
        await transport.authenticate('tok-1')
        expect(authCalls).toEqual(['tok-1'])

        await transport.authenticate('tok-2')
        expect(authCalls).toEqual(['tok-1', 'tok-2'])
        pipe.dispose()
    })

    test('deauthenticate releases the identity, so the same credential binds again', async () => {
        authCalls.length = 0
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10 })
        const transport = new WsTransport<Record<never, never>, Record<never, never>>({ pipe, auth: tokenAuth })
        await transport.authenticate('tok-1')
        await transport.deauthenticate()
        await transport.authenticate('tok-1')
        expect(authCalls).toEqual(['tok-1', 'tok-1'])
        pipe.dispose()
    })

    test('dispose stops reconnecting', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10 })
        await pipe.ensureConnected()
        const socketsBefore = server.socketCount()
        pipe.dispose()
        await new Promise(resolve => setTimeout(resolve, 80))
        expect(server.socketCount()).toBe(socketsBefore - 1)
        expect(() => pipe.ensureConnected()).toThrow()
    })

    test('queryAuth token rides the URL', async () => {
        const seen: { token: string | null } = { token: null }
        const authServer = Bun.serve({
            port: 0,
            fetch(req, srv) {
                seen.token = new URL(req.url).searchParams.get('token')
                if (srv.upgrade(req)) return
                return new Response('not ws', { status: 400 })
            },
            websocket: { message() {} }
        })
        const pipe = new WebSocketPipe({ url: `ws://localhost:${authServer.port}/`, queryAuth: { param: 'token', token: 'admin-key' } })
        await pipe.ensureConnected()
        expect(seen.token).toBe('admin-key')
        pipe.dispose()
        authServer.stop(true)
    })

    test('reconnectNow collapses a scheduled attempt', async () => {
        // A delay long enough that only bringing the attempt forward can
        // reconnect within the test.
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10_000 })
        await pipe.ensureConnected()
        killAllSockets()
        await nextTick()
        expect(pipe.connected).toBe(false)

        pipe.reconnectNow()
        await new Promise(resolve => setTimeout(resolve, 100))
        expect(pipe.connected).toBe(true)
        pipe.dispose()
    })

    test('drop abandons the socket, fails its in-flight calls and arms a reconnect', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10_000 })
        const client = new RpcClient<Methods>(new WsTransport({ pipe }))
        const hanging = client.call('hang')
        await nextTick()

        pipe.drop()
        // Synchronous, without the peer's participation: the point of drop is
        // that the socket it abandons cannot be relied on to close itself.
        expect(pipe.connected).toBe(false)
        await expect(hanging).rejects.toThrow(WebSocketClosedError)

        // Not terminal, unlike dispose: the reconnect is armed and waiting.
        pipe.reconnectNow()
        await new Promise(resolve => setTimeout(resolve, 100))
        expect(pipe.connected).toBe(true)
        pipe.dispose()
    })

    test('a dropped socket delivering late is ignored', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10_000 })
        await pipe.ensureConnected()
        const seen: string[] = []
        pipe.on('message', raw => seen.push(raw))

        // The zombie is disowned, not silenced — a frame already on its way
        // must not be taken for the live socket's.
        const zombies = server.sockets()
        pipe.drop()
        zombies.forEach(ws => ws.send('{"late":true}'))
        await nextTick()

        expect(seen).toEqual([])
        pipe.dispose()
    })

    test('probe answers true on a live socket and leaves it alone', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10_000 })
        const transport = new WsTransport({ pipe })
        await pipe.ensureConnected()
        const before = server.socketCount()

        // A method that answers is all a probe wants; what it replies is
        // irrelevant, and even a refusal would count — the socket carried it.
        expect(await transport.probe('ping', 500)).toBe(true)
        expect(pipe.connected).toBe(true)
        expect(server.socketCount()).toBe(before)
        pipe.dispose()
    })

    test('probe drops and reconnects a socket that does not answer', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10_000 })
        const transport = new WsTransport({ pipe })
        await pipe.ensureConnected()

        // `hang` never replies — the half-open socket in miniature: open to the
        // browser, answering nothing.
        expect(await transport.probe('hang', 50)).toBe(false)
        expect(pipe.connected).toBe(false)

        // And it reconnects on its own, without waiting out the armed delay —
        // nothing else here collapses it.
        await new Promise(resolve => setTimeout(resolve, 100))
        expect(pipe.connected).toBe(true)
        pipe.dispose()
    })

    test('probe keeps a socket that is delivering, however late its own answer', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10_000 })
        const transport = new WsTransport({ pipe })
        const client = new RpcClient<Methods>(transport)
        await pipe.ensureConnected()

        // `hang` never answers, so the deadline expires — but the socket is
        // demonstrably carrying frames the whole time, which is what the probe
        // is actually asking about.
        const probing = transport.probe('hang', 300)
        expect(await client.call('add', 1, 2)).toBe(3)
        expect(await probing).toBe(true)
        expect(pipe.connected).toBe(true)
        pipe.dispose()
    })

    test('drop on a socket that already closed is a no-op', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10_000 })
        const closes: WebSocketClosedError[] = []
        pipe.on('close', e => closes.push(e))
        await pipe.ensureConnected()
        killAllSockets()
        await nextTick()
        expect(closes.length).toBe(1)

        // The close path has already run and armed the reconnect. Running it a
        // second time would not only report a close that already happened: it
        // arms a second attempt over the first, and whichever of the two
        // sockets loses is left open with nothing owning it.
        pipe.drop()
        expect(closes.length).toBe(1)
        pipe.dispose()
    })

    test('probe on an already-down transport reports down without probing', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10_000 })
        const transport = new WsTransport({ pipe })
        await pipe.ensureConnected()
        killAllSockets()
        await nextTick()

        // A reconnect is already in flight and reports for itself; there is no
        // half-open socket to discover.
        expect(await transport.probe('ping', 500)).toBe(false)
        pipe.dispose()
    })

    test('reconnectNow is a no-op while the socket is open', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10 })
        await pipe.ensureConnected()
        const before = server.socketCount()
        pipe.reconnectNow()
        await nextTick()
        // No second socket: there was no waiting attempt to bring forward.
        expect(server.socketCount()).toBe(before)
        expect(pipe.connected).toBe(true)
        pipe.dispose()
    })

    test('a dropped in-flight call is attributed as in-flight', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10_000 })
        const client = new RpcClient<Methods>(new WsTransport({ pipe }))
        await pipe.ensureConnected()
        const hanging = client.call('hang')
        await nextTick()
        killAllSockets()
        const error = (await hanging.catch((e: unknown) => e)) as WebSocketClosedError
        expect(error).toBeInstanceOf(WebSocketClosedError)
        // It was on the wire, so it may have run server-side with only its
        // response lost — re-issuing it is not automatically safe.
        expect(error.inFlight).toBe(true)
        pipe.dispose()
    })

    test('a call rejected by a connect that never opened is not attributed in-flight', async () => {
        const pipe = new WebSocketPipe({ url: 'ws://127.0.0.1:1/', reconnectDelayMs: () => 10_000 })
        const client = new RpcClient<Methods>(new WsTransport({ pipe }))
        const error = (await client.call('add', 1, 2).catch((e: unknown) => e)) as WebSocketClosedError
        expect(error).toBeInstanceOf(WebSocketClosedError)
        // Nothing was ever written, so re-issuing it cannot double-apply.
        expect(error.inFlight).toBe(false)
        pipe.dispose()
    })
})

// The readiness surface an app hands to its UX layer as a connectivity oracle:
// "connected" must mean calls can succeed, which for an identified connection
// is strictly later than the socket opening.
describe('WsTransport connection readiness', () => {
    const tokenAuth = {
        authenticate: (send: (payload: RpcRequest) => Promise<unknown>, token: string) => send({ method: 'auth.token', arguments: [token] }),
        deauthenticate: (send: (payload: RpcRequest) => Promise<unknown>) => send({ method: 'auth.logout' })
    }

    test('an open with no identity to bind reports connected at once', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl })
        const transport = new WsTransport<Record<never, never>, Record<never, never>>({ pipe })
        const reports: boolean[] = []
        transport.onConnectionChange(up => reports.push(up))
        await transport.connect()
        await nextTick()
        expect(reports).toEqual([true])
        expect(transport.connected).toBe(true)
        pipe.dispose()
    })

    test('readiness is withheld while the identity bind is still outstanding', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10 })
        const transport = new WsTransport<Record<never, never>, Record<never, never>>({
            // A bind the server never answers: the socket is open, but a call
            // resumed on that bare open would race a frame still unprocessed —
            // and lose, coming back UNAUTHENTICATED.
            pipe,
            auth: { authenticate: send => send({ method: 'hang' }), deauthenticate: send => send({ method: 'auth.logout' }) }
        })
        const reports: boolean[] = []
        transport.onConnectionChange(up => reports.push(up))
        // The bind is still pending at dispose(), which sweeps it — caught so
        // the teardown does not surface as an unhandled rejection.
        void transport.authenticate('tok-1').catch(() => {})
        await new Promise(resolve => setTimeout(resolve, 120))

        expect(pipe.connected).toBe(true)
        expect(transport.connected).toBe(false)
        expect(reports).toEqual([])
        pipe.dispose()
    })

    test('readiness is reported once the bind is answered', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10 })
        const transport = new WsTransport<Record<never, never>, Record<never, never>>({ pipe, auth: tokenAuth })
        const reports: boolean[] = []
        transport.onConnectionChange(up => reports.push(up))
        await transport.authenticate('tok-1')
        await nextTick()
        expect(reports).toEqual([true])
        expect(transport.connected).toBe(true)
        pipe.dispose()
    })

    test('a refused bind still reports connected — the session, not the connection, is what is gone', async () => {
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10 })
        const transport = new WsTransport<Record<never, never>, Record<never, never>>({ pipe, auth: tokenAuth })
        const reports: boolean[] = []
        transport.onConnectionChange(up => reports.push(up))
        await expect(transport.authenticate('bad-1')).rejects.toThrow()
        await nextTick()
        // Withholding readiness here would park every call forever on a
        // perfectly good socket; letting them through surfaces the dead
        // session, which is the correct outcome.
        expect(reports).toEqual([true])
        pipe.dispose()
    })

    test('a bind settling after its socket has gone reports nothing', async () => {
        let releaseBind!: () => void
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10_000 })
        const transport = new WsTransport<Record<never, never>, Record<never, never>>({
            pipe,
            auth: {
                authenticate: () =>
                    new Promise(resolve => {
                        releaseBind = () => resolve(null)
                    }),
                deauthenticate: () => Promise.resolve(null)
            }
        })
        const reports: boolean[] = []
        transport.onConnectionChange(up => reports.push(up))
        void transport.authenticate('tok-1')
        await transport.connect()
        await nextTick()
        expect(reports).toEqual([])

        killAllSockets()
        await nextTick()
        expect(reports).toEqual([false])

        releaseBind()
        await nextTick()
        // That bind belonged to a socket that no longer exists; reporting it
        // would announce a connection nothing can be sent on.
        expect(reports).toEqual([false])
        expect(transport.connected).toBe(false)
        pipe.dispose()
    })

    test('every failed attempt reports again, so consecutive failures are countable', async () => {
        const pipe = new WebSocketPipe({ url: 'ws://127.0.0.1:1/', reconnectDelayMs: () => 10 })
        const transport = new WsTransport<Record<never, never>, Record<never, never>>({ pipe })
        const reports: boolean[] = []
        transport.onConnectionChange(up => reports.push(up))
        void pipe.ensureConnected().catch(() => {})
        await new Promise(resolve => setTimeout(resolve, 150))

        // Deduplicating these would erase exactly the signal a consumer needs
        // to tell a momentary drop from a sustained outage.
        expect(reports.length).toBeGreaterThan(1)
        expect(reports.some(up => up)).toBe(false)
        pipe.dispose()
    })
})
