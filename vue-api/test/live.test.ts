import { describe, expect, test } from 'bun:test'
import { computed, isReactive } from 'vue'
import { WebSocketPipe, WsTransport, type StateFrame } from '@greendrake/rpc'
import { keepDocument } from '../src/live'

interface Settings {
    mode: string
    limits: Record<string, number>
}

interface DataPushes {
    settings: StateFrame<Settings>
}

describe('keepDocument', () => {
    test('the document is reactive, initial until the socket sends its own, and patched in place after', () => {
        // A transport handed its frames by the test: nothing connects, and the
        // pipe's events are emitted as a socket's would be.
        const pipe = new WebSocketPipe({ url: 'ws://localhost:1/' })
        const transport = new WsTransport<Record<never, never>, DataPushes>({ pipe })
        const frame = (data: unknown): void => void pipe.emit('message', JSON.stringify({ type: 'settings', data }))

        const settings: Settings = keepDocument(transport, 'settings', { mode: 'unknown', limits: {} })
        expect(isReactive(settings)).toBe(true)
        const limited = computed(() => Object.keys(settings.limits).length)
        expect([settings.mode, limited.value]).toEqual(['unknown', 0])

        frame({ doc: { mode: 'eco', limits: { up: 10 } } })
        expect([settings.mode, limited.value]).toEqual(['eco', 1])

        frame({ patch: { limits: { down: 20 } } })
        expect(settings).toEqual({ mode: 'eco', limits: { up: 10, down: 20 } })
        expect(limited.value).toBe(2)
    })
})
