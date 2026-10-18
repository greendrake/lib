import { describe, expect, test } from 'bun:test'
import { WebSocketPipe, WsTransport, liveDocument, type StateFrame } from '@greendrake/rpc'
import { ConnRegistry, createDispatcher, websocketHandler } from '../src/main'

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
}

// A server over a registry of the test's own, its documents being the test's alone.
const serving = (registry: ConnRegistry): { url: string; stop: () => void } => {
    const ws = websocketHandler(createDispatcher({}), registry)
    const server = Bun.serve({
        port: 0,
        websocket: ws.handlers,
        fetch: async (request, srv) => ((await ws.upgrade(request, srv)) ? undefined : new Response(null, { status: 400 }))
    })
    return { url: `ws://localhost:${server.port}/`, stop: () => void server.stop(true) }
}

// A page on the server, keeping the document as a browser does.
const page = (url: string): { transport: WsTransport<Record<never, never>, Pushes>; cameras: Cameras; dispose: () => void } => {
    const pipe = new WebSocketPipe({ url })
    const transport = new WsTransport<Record<never, never>, Pushes>({ pipe })
    const cameras: Cameras = {}
    liveDocument(transport, 'cameras', cameras)
    return {
        transport,
        cameras,
        dispose: () => pipe.dispose()
    }
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

describe('delivery', () => {
    // A frame lost would leave a client silently wrong, so the transport closes a
    // socket its client has stopped reading rather than letting Bun drop its frames.
    test('a socket past its backpressure limit is closed, not dropped from', () => {
        expect(websocketHandler(createDispatcher({}), new ConnRegistry()).handlers.closeOnBackpressureLimit).toBe(true)
    })
})
