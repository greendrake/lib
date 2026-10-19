import { afterAll, describe, expect, test } from 'bun:test'
import { ref } from 'vue'
import { WebSocketPipe, WsTransport, type Caller } from '@greendrake/rpc'
import { cameraApi, useCameras, type Methods, type Pushes } from '../src/api'
import type { CameraState } from '../src/types'
import { undialled } from './cameras'

/** A client that records the calls made through it, and answers none. */
class Recorder implements Caller<Methods> {
    readonly calls: { method: string; args: unknown[] }[] = []

    call<K extends keyof Methods & string>(method: K, ...args: Parameters<Methods[K]>): Promise<Awaited<ReturnType<Methods[K]>>> {
        this.calls.push({ method, args })
        return new Promise(() => {})
    }
}

describe('a camera’s API', () => {
    test('names the camera on every call, beside what else the method takes', () => {
        const client = new Recorder()
        const api = cameraApi(client, 'gate')
        void api.call('settings.groups')
        void api.call('settings.get', { Group: 'ntp' })
        expect(client.calls.map(c => c.method)).toEqual(['settings.groups', 'settings.get'])
        expect(client.calls.map(c => c.args)).toEqual([[{ Camera: 'gate' }], [{ Group: 'ntp', Camera: 'gate' }]])
    })
    test('names the camera a ref names as each call is made', () => {
        const client = new Recorder()
        const name = ref('gate')
        const api = cameraApi(client, name)
        name.value = 'porch'
        void api.call('camera.time')
        expect(client.calls[0]!.args).toEqual([{ Camera: 'porch' }])
    })
})

const state = (State: CameraState['State']): CameraState => ({ ...undialled, State })

// The engine's socket: the document of every camera's state on opening, then a patch of
// the one that changed.
const server = Bun.serve({
    port: 0,
    fetch(req, srv) {
        if (srv.upgrade(req)) return
        return new Response('not ws', { status: 400 })
    },
    websocket: {
        open(ws) {
            ws.send(JSON.stringify({ type: 'cameras', data: { doc: { gate: state('online'), porch: state('online') } } }))
            ws.send(JSON.stringify({ type: 'cameras', data: { patch: { gate: { State: 'offline', Error: 'refused' } } } }))
        },
        message() {}
    }
})
afterAll(() => server.stop(true))

test('the cameras’ states are the document, with each patch merged in', async () => {
    const pipe = new WebSocketPipe({ url: `ws://localhost:${server.port}` })
    const transport = new WsTransport<Record<never, never>, Pushes>({ pipe })
    const cameras = useCameras(transport)
    const patched = new Promise<void>(resolve => {
        transport.onData('cameras', frame => {
            if ('patch' in frame) resolve()
        })
    })
    await transport.connect()
    await patched
    expect(cameras.gate).toEqual({ ...state('offline'), Error: 'refused' })
    expect(cameras.porch).toEqual(state('online'))
    pipe.dispose()
})
