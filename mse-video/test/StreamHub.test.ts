import { afterAll, describe, expect, test } from 'bun:test'
import type { ServerWebSocket } from 'bun'
import type { AudioFrame } from '../src/AudioPlayer'
import { StreamHub, type StreamSubscription } from '../src/StreamHub'

// The stream hub's server end: answers every subscription with channel 5, records every
// op it is sent, and broadcasts to every socket through Bun's pub/sub.
const received: Record<string, unknown>[] = []
let latest: ServerWebSocket<unknown>
const server = Bun.serve({
    port: 0,
    fetch: (req, srv) => (srv.upgrade(req) ? undefined : new Response('not ws', { status: 400 })),
    websocket: {
        open(ws) {
            ws.subscribe('viewers')
            latest = ws
        },
        message(ws, raw) {
            const op = JSON.parse(String(raw)) as Record<string, unknown>
            received.push(op)
            if (op.op === 'sub') {
                ws.send(JSON.stringify({
                    op: 'subbed',
                    id: op.id,
                    ch: 5
                }))
            }
        }
    }
})
const url = `ws://localhost:${server.port}`

// Not awaited: the stop promise can outlive the hook's timeout, and the process exits
// regardless.
afterAll(() => void server.stop(true))

const waitFor = async (what: string, condition: () => boolean): Promise<void> => {
    const deadline = Date.now() + 2000
    while (!condition()) {
        if (Date.now() > deadline) {
            throw new Error(`timed out waiting for ${what}`)
        }
        await Bun.sleep(5)
    }
}

const broadcast = (...bytes: number[]): void => void server.publish('viewers', Uint8Array.from(bytes))

const audioOps = (): Record<string, unknown>[] => received.filter(op => op.op === 'audio')

// A hub with one subscription, once the server has confirmed it.
const subscribed = async (): Promise<{ hub: StreamHub; subscription: StreamSubscription }> => {
    received.length = 0
    const hub = new StreamHub(url, () => undefined)
    hub.retain()
    const subscription = hub.subscribe('cam/1')
    await new Promise<void>(resolve => subscription.once('subscribed', resolve))
    return { hub, subscription }
}

describe('StreamHub', () => {
    test('routes each binary frame by its kind', async () => {
        const { hub, subscription } = await subscribed()
        const packets: number[][] = []
        const audio: AudioFrame[] = []
        subscription.on('packet', payload => packets.push(Array.from(new Uint8Array(payload))))
        subscription.on('audio', frame => audio.push(frame))
        broadcast(5, 0, 1, 2, 3)
        broadcast(5, 1, 0, 0, 0, 60, 0x1f, 0x40, 1, 9, 9)
        broadcast(5)
        broadcast(6, 0, 7)
        await waitFor('every frame', () => packets.length === 2)
        expect(packets).toEqual([[1, 2, 3], []])
        expect(audio.map(frame => ({ ...frame, samples: Array.from(frame.samples) }))).toEqual([
            {
                ptsMs: 60,
                sampleRate: 8000,
                codec: 'ulaw',
                samples: [9, 9]
            }
        ])
        hub.release()
    })

    test('asks for audio once the subscription has a channel, and again on every new one', async () => {
        received.length = 0
        const hub = new StreamHub(url, () => undefined)
        hub.retain()
        hub.subscribe('cam/1').setAudio(true)
        const sub = {
            op: 'sub',
            id: 1,
            key: 'cam/1'
        }
        const audioOn = {
            op: 'audio',
            ch: 5,
            on: true
        }
        await waitFor('audio to be asked for', () => audioOps().length === 1)
        expect(received).toEqual([sub, audioOn])
        // The socket drops; the hub subscribes again on a new one.
        latest.close()
        await waitFor('audio to be asked for again', () => audioOps().length === 2)
        expect(received).toEqual([sub, audioOn, sub, audioOn])
        hub.release()
    })

    test('turns audio off on the channel it is on', async () => {
        const { hub, subscription } = await subscribed()
        subscription.setAudio(true)
        subscription.setAudio(false)
        await waitFor('audio to be turned off', () => audioOps().length === 2)
        expect(audioOps().map(op => op.on)).toEqual([true, false])
        hub.release()
    })
})
