import { mock } from 'bun:test'
import { createApp, type VNode } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { WebSocketClosedError } from '@greendrake/rpc'
import type { RpcRequest, Transport } from '@greendrake/rpc'
import * as ui from '@greendrake/ui'
import { stubGlobals } from '@greendrake/util/testing'
import type { ExceptionKind } from '../src/exceptionState'
import { configureApiUX, type ConnectionSource } from '../src/uxConfig'

// What an exception toast was given: its kind, and what its buttons call.
export interface ToastButtons {
    kind: ExceptionKind
    retry: () => Promise<void> | undefined
    cancel: () => void
}

// The toasts raised, as their buttons and whether they are still up: bun has
// no DOM to render them into.
const raised: { buttons: ToastButtons; up: boolean }[] = []
mock.module('@greendrake/ui', () => ({
    ...ui,
    showToast: (options: ui.ToastOptions): ui.ToastHandle => {
        const toast = { buttons: (options.content as VNode).props as ToastButtons, up: true }
        raised.push(toast)
        return {
            destroy: () => {
                toast.up = false
            }
        }
    }
}))

// The toasts up, oldest first.
export const toastsUp = (): ToastButtons[] => raised.filter(t => t.up).map(t => t.buttons)

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
// request with it, or refused before the request could be written at all, or
// failed with the error given.
export type Outcome = 'ok' | 'in-flight-drop' | 'never-sent' | Error

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
        return Promise.reject(outcome instanceof Error ? outcome : closed(outcome === 'in-flight-drop'))
    }
}

// Bun's navigator carries no onLine, and the store reads it at construction:
// the calling test file's navigator is the stand-in returned, whose onLine its
// tests set.
export const stubNavigator = (): { onLine: boolean } => {
    const navigator = { onLine: true }
    stubGlobals(() => ({ navigator }))
    return navigator
}

// The store reads its source once, when it materialises, so each test wires the
// source it wants before touching useConnectivity(); it starts on fresh stores,
// no toast raised.
export const withSource = (source?: ConnectionSource): void => {
    raised.length = 0
    configureApiUX({ connectionSource: source })
    const pinia = createPinia()
    createApp({}).use(pinia)
    setActivePinia(pinia)
}

// Long enough for a call's own promise chain to settle to its next rest state.
export const tick = (): Promise<void> => new Promise(resolve => setTimeout(resolve, 0))
