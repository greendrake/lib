import { createApp } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { WebSocketClosedError } from '@greendrake/rpc'
import type { RpcRequest, Transport } from '@greendrake/rpc'
import { configureApiUX, type ConnectionSource } from '../src/uxConfig'

// A transport standing in for the real connectivity oracle: reports are driven
// by hand, and deliberately not deduplicated — repeated `false` is what a run
// of failed reconnect attempts looks like.
export class FakeSource implements ConnectionSource {
    connected = false
    readonly #listeners: ((connected: boolean) => void)[] = []

    onConnectionChange(listener: (connected: boolean) => void): void {
        this.#listeners.push(listener)
    }

    report(connected: boolean): void {
        this.connected = connected
        this.#listeners.forEach(listener => listener(connected))
    }
}

// How a scripted call ends: served, or dropped by a socket that took the
// request with it, or refused before the request could be written at all.
export type Outcome = 'ok' | 'in-flight-drop' | 'never-sent'

const closed = (inFlight: boolean): WebSocketClosedError => {
    const error = new WebSocketClosedError(new CloseEvent('close', { code: 1006, wasClean: false }))
    return inFlight ? error.asInFlight() : error
}

// Answers calls from a script, recording what reached it. An exhausted script
// succeeds, so a test states only the failures it is about.
export class ScriptedTransport implements Transport {
    outcomes: Outcome[] = []
    readonly sent: RpcRequest[] = []

    send(request: RpcRequest): Promise<unknown> {
        this.sent.push(request)
        const outcome = this.outcomes.shift() ?? 'ok'
        if (outcome === 'ok') {
            return Promise.resolve('result')
        }
        return Promise.reject(closed(outcome === 'in-flight-drop'))
    }
}

// Bun's navigator carries no onLine, and the store reads it at construction.
export const setDeviceOnline = (on: boolean): void => {
    Object.defineProperty(navigator, 'onLine', { value: on, configurable: true })
}

// The store reads its source once, when it materialises, so each test wires the
// source it wants before touching useConnectivity().
export const withSource = (source?: ConnectionSource): void => {
    configureApiUX({ connectionSource: source })
    const pinia = createPinia()
    createApp({}).use(pinia)
    setActivePinia(pinia)
}

// Long enough for a call's own promise chain to settle to its next rest state.
export const tick = (): Promise<void> => new Promise(resolve => setTimeout(resolve, 0))
