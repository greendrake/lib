import { describe, expect, test } from 'bun:test'
import { RpcClient, WebSocketPipe, WsTransport, liveDocument, type StateFrame } from '@greendrake/rpc'
import { ConnRegistry, createDispatcher, method, topicsOf, websocketHandler } from '../src/main'

// The document's own format — what a document is, the patch between two — is
// @greendrake/rpc's, and is tested there. Here: the registry publishing documents to
// sockets, as the client end keeps them.

interface Camera {
    State: string
    Error?: string
}

type Cameras = Record<string, Camera>

interface Pushes {
    cameras: StateFrame<Cameras>
    // A frame the test sends a socket after whatever it is waiting to see did or
    // did not come: the socket's frames arrive in order, so a document frame not
    // in by the mark was never sent.
    mark: number
}

// The topic a socket joins to keep the topic's documents, and the calls that
// join and leave it.
const WATCHERS = 'cameras.watchers'

interface Calls {
    watch: () => void
    unwatch: () => void
    // Answers nothing: answered at all, it says the server holds the socket.
    hello: () => void
}

// A server over a registry of the test's own, its documents being the test's alone.
const serving = (registry: ConnRegistry): { url: string; stop: () => void } => {
    const methods = {
        watch: method({ auth: 'none', handler: ctx => topicsOf(ctx).join(WATCHERS) }),
        unwatch: method({ auth: 'none', handler: ctx => topicsOf(ctx).leave(WATCHERS) }),
        hello: method({ auth: 'none', handler: () => undefined })
    }
    const ws = websocketHandler(createDispatcher(methods), registry)
    const server = Bun.serve({
        port: 0,
        websocket: ws.handlers,
        fetch: (request, srv) => ws.upgrade(request, srv)
    })
    return { url: `ws://localhost:${server.port}/`, stop: () => void server.stop(true) }
}

interface Page {
    transport: WsTransport<Record<never, never>, Pushes>
    client: RpcClient<Calls>
    cameras: Cameras
    // Every frame of the document the page has been sent.
    frames: StateFrame<Cameras>[]
    // Resolves once the mark of that number has reached the page.
    marked: (mark: number) => Promise<void>
    dispose: () => void
}

// A page on the server, keeping the document as a browser does.
const page = (url: string): Page => {
    const pipe = new WebSocketPipe({ url })
    const transport = new WsTransport<Record<never, never>, Pushes>({ pipe })
    const cameras: Cameras = {}
    liveDocument(transport, 'cameras', cameras)
    const frames: StateFrame<Cameras>[] = []
    transport.onData('cameras', frame => frames.push(frame))
    return {
        transport,
        client: new RpcClient<Calls>(transport),
        cameras,
        frames,
        marked: mark =>
            new Promise(resolve => {
                const off = transport.onData('mark', got => {
                    if (got === mark) {
                        off()
                        resolve()
                    }
                })
            }),
        dispose: () => pipe.dispose()
    }
}

// A page whose socket the server holds.
const held = async (url: string): Promise<Page> => {
    const at = page(url)
    await at.client.call('hello')
    return at
}

// Sends every page the mark, and waits for each to have it.
let marks = 0
const mark = async (registry: ConnRegistry, pages: Page[]): Promise<void> => {
    marks += 1
    const arrived = pages.map(at => at.marked(marks))
    registry.broadcastData('mark', marks)
    await Promise.all(arrived)
}

// The next frame of the document to reach the page.
const nextFrame = (transport: WsTransport<Record<never, never>, Pushes>): Promise<StateFrame<Cameras>> =>
    new Promise(resolve => {
        const off = transport.onData('cameras', frame => {
            off()
            resolve(frame)
        })
    })

describe('documents', () => {
    test('an opening socket is greeted with the document, and kept by each patch after it', async () => {
        const registry = new ConnRegistry()
        const state: Cameras = { gate: { State: 'online' }, porch: { State: 'online' } }
        const doc = registry.document('cameras', () => state)
        const server = serving(registry)
        const { transport, cameras, dispose } = page(server.url)

        const greeted = nextFrame(transport)
        await transport.connect()
        expect(await greeted).toEqual({ doc: { gate: { State: 'online' }, porch: { State: 'online' } } })
        expect(cameras).toEqual(state)

        state.gate = { State: 'offline', Error: 'refused' }
        const changed = nextFrame(transport)
        doc.refresh()
        expect(await changed).toEqual({ patch: { gate: { State: 'offline', Error: 'refused' } } })

        // Nothing changed, so nothing goes: the next frame is the removal after it.
        doc.refresh()
        delete state.porch
        const removed = nextFrame(transport)
        doc.refresh()
        expect(await removed).toEqual({ patch: { porch: null } })
        expect(cameras).toEqual({ gate: { State: 'offline', Error: 'refused' } })

        dispose()
        server.stop()
    })

    test('a document made while a socket is open greets it', async () => {
        const registry = new ConnRegistry()
        const server = serving(registry)
        const { transport, cameras, dispose } = page(server.url)
        await transport.connect()
        // The server may not have registered the socket by the time the client
        // has it open.
        if (registry.size === 0) {
            await new Promise<void>(resolve => {
                const off = registry.onPresence(() => {
                    off()
                    resolve()
                })
            })
        }

        const greeted = nextFrame(transport)
        registry.document('cameras', () => ({ gate: { State: 'online' } }))
        expect(await greeted).toEqual({ doc: { gate: { State: 'online' } } })
        expect(cameras).toEqual({ gate: { State: 'online' } })

        dispose()
        server.stop()
    })

    test('a member produced as null is dropped', async () => {
        const registry = new ConnRegistry()
        registry.document('cameras', () => ({ gate: { State: 'online', Error: null }, porch: null }))
        const server = serving(registry)
        const { transport, dispose } = page(server.url)

        const greeted = nextFrame(transport)
        await transport.connect()
        expect(await greeted).toEqual({ doc: { gate: { State: 'online' } } })

        dispose()
        server.stop()
    })

    test('a state that is no JSON object is refused', () => {
        expect(() => new ConnRegistry().document('cameras', () => [])).toThrow('document cameras is not a JSON object')
    })
})

describe('documents kept by a topic', () => {
    test("a topic's document reaches its members alone: whole as a socket joins, ahead of the answer, then each patch", async () => {
        const registry = new ConnRegistry()
        const state: Cameras = { gate: { State: 'online' } }
        const doc = registry.document('cameras', () => state, { topic: WATCHERS })
        const server = serving(registry)
        const [member, other] = await Promise.all([held(server.url), held(server.url)])
        await mark(registry, [member, other])
        expect([member.frames, other.frames]).toEqual([[], []])

        await member.client.call('watch')
        expect(member.frames).toEqual([{ doc: { gate: { State: 'online' } } }])

        state.gate = { State: 'offline' }
        doc.refresh()
        await mark(registry, [member, other])
        expect(member.frames.at(-1)).toEqual({ patch: { gate: { State: 'offline' } } })
        expect(member.cameras).toEqual(state)
        expect(other.frames).toEqual([])

        // A socket already in the topic is not greeted again.
        await member.client.call('watch')
        await mark(registry, [member])
        expect(member.frames).toHaveLength(2)

        member.dispose()
        other.dispose()
        server.stop()
    })

    test("a member that leaves, or closes, is sent nothing further, and the topic's presence follows its first member and its last", async () => {
        const registry = new ConnRegistry()
        const state: Cameras = { gate: { State: 'online' } }
        const doc = registry.document('cameras', () => state, { topic: WATCHERS })
        const presence: boolean[] = []
        doc.onPresence(present => presence.push(present))
        const server = serving(registry)
        const [first, second] = await Promise.all([held(server.url), held(server.url)])

        await first.client.call('watch')
        expect([presence, doc.size]).toEqual([[true], 1])
        await second.client.call('watch')
        expect([presence, doc.size]).toEqual([[true], 2])
        await first.client.call('unwatch')
        expect([presence, doc.size]).toEqual([[true], 1])

        // A closing socket leaves the topic with it.
        const gone = new Promise<void>(resolve => {
            const off = doc.onPresence(present => {
                if (!present) {
                    off()
                    resolve()
                }
            })
        })
        second.dispose()
        await gone
        expect([presence, doc.size]).toEqual([[true, false], 0])

        const sent = first.frames.length
        state.gate = { State: 'offline' }
        doc.refresh()
        await mark(registry, [first])
        expect(first.frames).toHaveLength(sent)

        first.dispose()
        server.stop()
    })
})

describe('delivery', () => {
    // A frame lost would leave a client silently wrong, so the transport closes a
    // socket its client has stopped reading rather than letting Bun drop its frames.
    test('a socket past its backpressure limit is closed, not dropped from', () => {
        expect(websocketHandler(createDispatcher({}), new ConnRegistry()).handlers.closeOnBackpressureLimit).toBe(true)
    })
})
