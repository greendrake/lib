import { afterAll, describe, expect, test } from 'bun:test'
import { WebSocketPipe, WsTransport, liveBinding } from '../src/main'
import { nextTick, startWsServer } from './wsServer'

interface Events {
    'thing.changed': { id: string }
    'list.changed': Record<string, never>
}

interface DataPushes {
    telemetry: { volts: number }
}

const server = startWsServer()
const wsUrl = server.url

afterAll(server.stop)

// A transport on its own pipe, connected. Reconnect is near-instant so a test
// that kills the socket does not spend its budget waiting for the return.
const connected = async (): Promise<{ transport: WsTransport<Events, DataPushes>; pipe: WebSocketPipe }> => {
    const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10 })
    const transport = new WsTransport<Events, DataPushes>({ pipe })
    await transport.connect()
    return { transport, pipe }
}

const pushEvent = (event: string, data: unknown): void =>
    server.broadcast({
        type: 'event',
        event,
        data
    })

// A drop the binding witnesses, and the return from it: awaited on the very
// report the binding reads, so the test is over exactly when the reconnect is
// rather than after a guessed interval.
const bounce = async (transport: WsTransport<Events, DataPushes>): Promise<void> => {
    const returned = new Promise<void>(resolve => {
        const off = transport.onConnectionChange(connected => {
            if (!connected) return
            off()
            resolve()
        })
    })
    server.killAllSockets()
    await returned
}

describe('liveBinding', () => {
    test('a push with apply folds in; when filters by relevance', async () => {
        const { transport, pipe } = await connected()
        const applied: string[] = []
        let resyncs = 0
        const release = liveBinding(transport, {
            events: {
                'thing.changed': {
                    when: data => data.id === 'T1',
                    apply: data => applied.push(data.id)
                }
            },
            resync: () => {
                resyncs++
            }
        })

        pushEvent('thing.changed', { id: 'T2' })
        pushEvent('thing.changed', { id: 'T1' })
        await nextTick()

        expect(applied).toEqual(['T1'])
        // Folded in locally: the push answered itself, so nothing re-reads.
        expect(resyncs).toBe(0)
        release()
        pipe.dispose()
    })

    test('a push without apply re-reads, and an empty handler takes every push', async () => {
        const { transport, pipe } = await connected()
        let resyncs = 0
        const release = liveBinding(transport, {
            events: { 'list.changed': {} },
            resync: () => {
                resyncs++
            }
        })

        pushEvent('list.changed', {})
        await nextTick()
        expect(resyncs).toBe(1)
        release()
        pipe.dispose()
    })

    test('bare data frames bind the same way', async () => {
        const { transport, pipe } = await connected()
        const volts: number[] = []
        const release = liveBinding(transport, { data: { telemetry: { apply: data => volts.push(data.volts) } } })

        server.broadcast({ type: 'telemetry', data: { volts: 48 } })
        await nextTick()

        expect(volts).toEqual([48])
        release()
        pipe.dispose()
    })

    test('a handler that neither applies nor has a resync to fall back on is refused', async () => {
        const { transport, pipe } = await connected()
        expect(() => liveBinding(transport, { events: { 'list.changed': {} } })).toThrow('list.changed')
        pipe.dispose()
    })

    test('the first connect is not a return from a drop', async () => {
        // Bound before the socket is up, so the binding sees the initial
        // connect: there is nothing yet for it to have missed.
        const pipe = new WebSocketPipe({ url: wsUrl, reconnectDelayMs: () => 10 })
        const transport = new WsTransport<Events, DataPushes>({ pipe })
        let resyncs = 0
        const release = liveBinding(transport, {
            events: { 'list.changed': {} },
            resync: () => {
                resyncs++
            }
        })

        await transport.connect()
        await nextTick()
        expect(resyncs).toBe(0)
        release()
        pipe.dispose()
    })

    test('a socket that drops and returns re-reads', async () => {
        const { transport, pipe } = await connected()
        let resyncs = 0
        const release = liveBinding(transport, {
            events: { 'thing.changed': { apply: () => {} } },
            resync: () => {
                resyncs++
            }
        })

        await bounce(transport)

        expect(resyncs).toBe(1)
        expect(pipe.connected).toBe(true)
        release()
        pipe.dispose()
    })

    test('a burst during an in-flight re-read collapses into one follow-up', async () => {
        const { transport, pipe } = await connected()
        let started = 0
        let releaseFirst!: () => void
        const firstDone = new Promise<void>(resolve => {
            releaseFirst = resolve
        })
        const release = liveBinding(transport, {
            events: { 'list.changed': {} },
            resync: () => {
                started++
                return started === 1 ? firstDone : Promise.resolve()
            }
        })

        pushEvent('list.changed', {})
        await nextTick()
        expect(started).toBe(1)

        // Three more while the first is still out: they ask the same question,
        // so one answer is owed, not three.
        pushEvent('list.changed', {})
        pushEvent('list.changed', {})
        pushEvent('list.changed', {})
        await nextTick()
        expect(started).toBe(1)

        releaseFirst()
        await nextTick()
        expect(started).toBe(2)
        release()
        pipe.dispose()
    })

    test('a rejected re-read frees the slot and still answers what arrived during it', async () => {
        const { transport, pipe } = await connected()
        const outcomes: string[] = []
        let releaseFirst!: () => void
        const firstDone = new Promise<void>((_, reject) => {
            releaseFirst = () => reject(new Error('read failed'))
        })
        const release = liveBinding(transport, {
            events: { 'list.changed': {} },
            resync: () => {
                outcomes.push('run')
                return outcomes.length === 1 ? firstDone : Promise.resolve()
            }
        })

        pushEvent('list.changed', {})
        await nextTick()
        pushEvent('list.changed', {})
        releaseFirst()
        await nextTick()

        expect(outcomes).toEqual(['run', 'run'])

        // And the slot is free for what comes after the failure.
        pushEvent('list.changed', {})
        await nextTick()
        expect(outcomes).toEqual(['run', 'run', 'run'])
        release()
        pipe.dispose()
    })

    test('a re-read that throws synchronously frees the slot like a rejection does', async () => {
        const { transport, pipe } = await connected()
        let runs = 0
        const release = liveBinding(transport, {
            events: { 'list.changed': {} },
            resync: () => {
                runs++
                if (runs === 1) throw new Error('read failed')
            }
        })

        pushEvent('list.changed', {})
        await nextTick()
        expect(runs).toBe(1)

        // Held still, the binding would be dead here: every later push would
        // only queue behind a re-read that never finished.
        pushEvent('list.changed', {})
        await nextTick()
        expect(runs).toBe(2)
        release()
        pipe.dispose()
    })

    test('release detaches every subscription, reconnect included, and repeats harmlessly', async () => {
        const { transport, pipe } = await connected()
        let applied = 0
        let resyncs = 0
        const release = liveBinding(transport, {
            events: { 'thing.changed': { apply: () => applied++ } },
            data: { telemetry: { apply: () => applied++ } },
            resync: () => {
                resyncs++
            }
        })

        release()
        release()

        pushEvent('thing.changed', { id: 'T1' })
        server.broadcast({ type: 'telemetry', data: { volts: 12 } })
        await nextTick()
        await bounce(transport)

        expect(applied).toBe(0)
        expect(resyncs).toBe(0)
        pipe.dispose()
    })
})
