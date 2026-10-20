import { afterEach, describe, expect, test } from 'bun:test'
import { effectScope, ref, watch } from 'vue'
import { WebSocketPipe, WsTransport } from '@greendrake/rpc'
import { ConnRegistry, createDispatcher, websocketHandler } from '@greendrake/rpc-server'
import type { ServiceStatePushes } from '@greendrake/service-state'
import { createServiceStateMachine, serviceStateBoard, serviceStateMethods } from '@greendrake/service-state/server'
import type { ServiceStateMachine } from '@greendrake/service-state/server'
import { keepServiceStates } from '../src/keeper'

// What each test started, stopped after it.
const stops: (() => void)[] = []
afterEach(() => stops.splice(0).forEach(stop => stop()))

// A backend serving two services over a socket — web, which is ON, and worker,
// which is OFF — and a page's keeper over a socket to it.
const keeping = () => {
    const registry = new ConnRegistry()
    const board = serviceStateBoard(registry)
    const machines: ServiceStateMachine[] = Object.entries({ web: 'ON', worker: 'OFF' } as const).map(([name, state]) =>
        createServiceStateMachine(
            name,
            {
                start: () => Promise.resolve(),
                stop: () => Promise.resolve(),
                check: () => Promise.resolve(state)
            },
            board,
            60_000
        )
    )
    const ws = websocketHandler(createDispatcher(serviceStateMethods(board, { watch: { auth: 'none' }, command: { auth: 'none' } })), registry)
    const server = Bun.serve({
        port: 0,
        websocket: ws.handlers,
        fetch: (request, srv) => ws.upgrade(request, srv)
    })
    const pipe = new WebSocketPipe({ url: `ws://localhost:${server.port}/` })
    const scope = effectScope()
    stops.push(() => {
        scope.stop()
        pipe.dispose()
        machines.forEach(machine => machine.close())
        void server.stop(true)
    })
    return { keeper: keepServiceStates(new WsTransport<Record<never, never>, ServiceStatePushes>({ pipe })), scope }
}

// Resolves once what holds is true, as Vue sees it change.
const until = (holds: () => boolean): Promise<void> =>
    new Promise(resolve => {
        if (holds()) {
            resolve()
            return
        }
        const stop = watch(holds, held => {
            if (held) {
                stop()
                resolve()
            }
        })
    })

describe('keepServiceStates', () => {
    test('watching a service opens the socket, and shows its status once greeted', async () => {
        const { keeper, scope } = keeping()
        scope.run(() => keeper.watch('web'))
        await until(() => keeper.status('web')?.state === 'ON')
        expect(keeper.status('worker')).toBeUndefined()
    })

    test('a getter that changes moves the watch: the first service is released, the second watched', async () => {
        const { keeper, scope } = keeping()
        const selected = ref('web')
        scope.run(() => keeper.watch(() => selected.value))
        await until(() => keeper.status('web')?.state === 'ON')
        selected.value = 'worker'
        await until(() => keeper.status('worker')?.state === 'OFF')
        await until(() => keeper.status('web') === undefined)
    })

    test("a refused watch is the service's failure", async () => {
        const { keeper, scope } = keeping()
        scope.run(() => keeper.watch('db'))
        await until(() => keeper.failure('db') !== undefined)
        expect(keeper.failure('db')?.message).toBe('SERVICE_NOT_FOUND')
        expect(keeper.status('db')).toBeUndefined()
    })
})
