import { afterEach, beforeEach, describe, expect, jest, test } from 'bun:test'
import { keepConnected, type Revivable } from '../src/keepConnected'
import { removeDocument, setVisibility } from './support'

interface Methods {
    'app.ping': () => void
}

// Records what keepConnected asks of the socket.
class FakeSocket implements Revivable {
    reconnects = 0
    readonly probes: string[] = []
    readonly pipe = { reconnectNow: (): void => void this.reconnects++ }

    probe(method: string): Promise<boolean> {
        this.probes.push(method)
        return Promise.resolve(true)
    }
}

const becomes = (state: DocumentVisibilityState): void => {
    setVisibility(state)
    dispatchEvent(new Event('visibilitychange'))
}

let stop: () => void

beforeEach(() => {
    jest.useFakeTimers()
})

afterEach(() => {
    stop()
    jest.useRealTimers()
    removeDocument()
})

describe('keepConnected', () => {
    test('a visible page reconnects at once, probes, and keeps probing on the beat', () => {
        setVisibility('visible')
        const socket = new FakeSocket()
        stop = keepConnected<Methods>(socket, 'app.ping')
        expect(socket.reconnects).toBe(1)
        expect(socket.probes).toEqual(['app.ping'])

        jest.advanceTimersByTime(60_000)
        expect(socket.probes).toHaveLength(2)
        jest.advanceTimersByTime(60_000)
        expect(socket.probes).toHaveLength(3)
    })

    test('a hidden page asks nothing until it is shown', () => {
        setVisibility('hidden')
        const socket = new FakeSocket()
        stop = keepConnected<Methods>(socket, 'app.ping')
        jest.advanceTimersByTime(120_000)
        expect(socket.reconnects).toBe(0)
        expect(socket.probes).toEqual([])

        becomes('visible')
        expect(socket.reconnects).toBe(1)
        expect(socket.probes).toEqual(['app.ping'])
    })

    test('hiding the page stops the beat', () => {
        setVisibility('visible')
        const socket = new FakeSocket()
        stop = keepConnected<Methods>(socket, 'app.ping')
        becomes('hidden')
        jest.advanceTimersByTime(120_000)
        expect(socket.probes).toHaveLength(1)
    })

    test('showing the page again restarts the beat rather than adding one', () => {
        setVisibility('visible')
        const socket = new FakeSocket()
        stop = keepConnected<Methods>(socket, 'app.ping')
        becomes('visible')
        expect(socket.probes).toHaveLength(2)
        jest.advanceTimersByTime(60_000)
        expect(socket.probes).toHaveLength(3)
    })

    test('the device coming back reconnects without probing', () => {
        setVisibility('hidden')
        const socket = new FakeSocket()
        stop = keepConnected<Methods>(socket, 'app.ping')
        dispatchEvent(new Event('online'))
        expect(socket.reconnects).toBe(1)
        expect(socket.probes).toEqual([])
    })

    test('the teardown silences every trigger', () => {
        setVisibility('visible')
        const socket = new FakeSocket()
        keepConnected<Methods>(socket, 'app.ping')()
        stop = (): void => {}
        dispatchEvent(new Event('online'))
        becomes('visible')
        jest.advanceTimersByTime(120_000)
        expect(socket.reconnects).toBe(1)
        expect(socket.probes).toHaveLength(1)
    })
})
