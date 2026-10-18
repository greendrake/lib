import { describe, expect, test } from 'bun:test'
import { WebSocketClosedError, WebSocketPipe, WsTransport, liveDocument, type StateFrame } from '../src/main'

interface Camera {
    State: string
    Error?: string
    Streams?: Record<string, { Cast: boolean }>
}

type Cameras = Record<string, Camera>

interface DataPushes {
    cameras: StateFrame<Cameras>
    telemetry: { volts: number }
}

// A transport handed its frames by the test: nothing connects, and the pipe's events
// are emitted as a socket's would be.
const unconnected = (): { transport: WsTransport<Record<never, never>, DataPushes>; frame: (data: unknown) => void; drop: () => void } => {
    const pipe = new WebSocketPipe({ url: 'ws://localhost:1/' })
    const transport = new WsTransport<Record<never, never>, DataPushes>({ pipe })
    return {
        transport,
        frame: data => pipe.emit('message', JSON.stringify({ type: 'cameras', data })),
        drop: () => pipe.emit('close', new WebSocketClosedError(new CloseEvent('close', { code: 1006 })))
    }
}

describe('liveDocument', () => {
    test('a document replaces the target whole, and each patch merges into it', () => {
        const { transport, frame } = unconnected()
        const cameras: Cameras = { stale: { State: 'online' } }
        liveDocument(transport, 'cameras', cameras)

        frame({ doc: { gate: { State: 'offline', Error: 'refused' }, porch: { State: 'online' } } })
        expect(cameras).toEqual({ gate: { State: 'offline', Error: 'refused' }, porch: { State: 'online' } })

        frame({
            patch: {
                gate: {
                    State: 'online',
                    Error: null,
                    Streams: { main: { Cast: true } }
                }
            }
        })
        expect(cameras).toEqual({ gate: { State: 'online', Streams: { main: { Cast: true } } }, porch: { State: 'online' } })

        frame({ patch: { porch: null } })
        expect(cameras).toEqual({ gate: { State: 'online', Streams: { main: { Cast: true } } } })
    })

    test('a patch before any document is refused', () => {
        const { transport, frame } = unconnected()
        liveDocument(transport, 'cameras', {})
        expect(() => frame({ patch: { gate: { State: 'online' } } })).toThrow("a patch of 'cameras' came before its document")
    })

    test('a patch is only ever over the document its own socket sent', () => {
        const { transport, frame, drop } = unconnected()
        const cameras: Cameras = {}
        liveDocument(transport, 'cameras', cameras)
        frame({ doc: { gate: { State: 'online' } } })
        drop()
        expect(() => frame({ patch: { gate: { State: 'offline' } } })).toThrow()

        frame({ doc: { gate: { State: 'offline' } } })
        frame({ patch: { gate: { State: 'online' } } })
        expect(cameras).toEqual({ gate: { State: 'online' } })
    })

    test('released, it applies nothing more', () => {
        const { transport, frame } = unconnected()
        const cameras: Cameras = {}
        const release = liveDocument(transport, 'cameras', cameras)
        frame({ doc: { gate: { State: 'online' } } })
        release()
        frame({ doc: { porch: { State: 'online' } } })
        expect(cameras).toEqual({ gate: { State: 'online' } })
    })

    test('only a state stream can be kept', () => {
        const { transport } = unconnected()
        // @ts-expect-error telemetry's frames are no state stream's
        liveDocument(transport, 'telemetry', {})
    })
})
