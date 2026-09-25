import { beforeEach, describe, expect, test } from 'bun:test'
import { ApiError, createDispatcher } from '@greendrake/rpc-server'
import type { EventSink } from '@greendrake/rpc-server'
import { SERVICE_STATE_EVENT, createServiceStateMachine, serviceStateMethods } from '../src/server'
import type { ServiceImplementation, ServiceState, ServiceStatus } from '../src/server'

// Every push the machine sends, so a test asserts the sequence an operator's
// screen would go through rather than only where it ended up.
const sink = (): EventSink & { states: () => ServiceState[]; pushes: ServiceStatus[] } => {
    const pushes: ServiceStatus[] = []
    return {
        pushes,
        states: () => pushes.map(status => status.state),
        emitToUser: () => {
            throw new Error('the machine addresses nobody: a service has one state, and everyone watching shares it')
        },
        broadcastEvent: (event, data) => {
            expect(event).toBe(SERVICE_STATE_EVENT)
            pushes.push(data as ServiceStatus)
        }
    }
}

// A service whose transitions are resolved by hand, so a test can hold one
// open and look at the machine while it is mid-flight.
const controllable = (): ServiceImplementation & { settle: (outcome?: Error) => void; observed: ServiceState } => {
    let release: (() => void) | undefined
    let fail: ((e: Error) => void) | undefined
    const transition = (to: ServiceState): Promise<void> =>
        new Promise((resolve, reject) => {
            release = () => {
                service.observed = to
                resolve()
            }
            fail = reject
        })
    const service = {
        observed: 'OFF' as ServiceState,
        start: () => transition('ON'),
        stop: () => transition('OFF'),
        check: () => Promise.resolve(service.observed),
        settle: (outcome?: Error) => (outcome ? fail!(outcome) : release!())
    }
    return service
}

// The machine settles its transition on a microtask; this lets it.
const settled = (): Promise<void> => Bun.sleep(0)

// What a handler is called with, from an admin.
const ctx = {
    auth: {
        userId: 'root',
        isAdmin: true,
        phantom: false
    },
    clientIp: '',
    locale: '',
    signal: AbortSignal.any([])
}

describe('the machine', () => {
    let service: ReturnType<typeof controllable>
    let events: ReturnType<typeof sink>

    beforeEach(() => {
        service = controllable()
        events = sink()
    })

    test('it reads the service before reporting anything', async () => {
        service.observed = 'ON'
        const machine = await createServiceStateMachine('web', service, events)
        expect(machine.status().state).toBe('ON')
        expect(events.states()).toEqual(['ON'])
    })

    test('a boot that finds the service where it assumed pushes nothing', async () => {
        const machine = await createServiceStateMachine('web', service, events)
        expect(machine.status().state).toBe('OFF')
        expect(events.states()).toEqual([])
    })

    test('every status names its service, the pushes included', async () => {
        const machine = await createServiceStateMachine('web', service, events)
        expect(machine.status().service).toBe('web')
        machine.start()
        service.settle()
        await settled()
        expect(events.pushes.map(status => status.service)).toEqual(['web', 'web'])
    })

    test('starting goes through STARTING and lands ON', async () => {
        const machine = await createServiceStateMachine('web', service, events)
        expect(machine.start().state).toBe('STARTING')
        expect(machine.status().state).toBe('STARTING')
        service.settle()
        await settled()
        expect(machine.status().state).toBe('ON')
        expect(events.states()).toEqual(['STARTING', 'ON'])
    })

    test('stopping goes through STOPPING and lands OFF', async () => {
        service.observed = 'ON'
        const machine = await createServiceStateMachine('web', service, events)
        expect(machine.stop().state).toBe('STOPPING')
        service.settle()
        await settled()
        expect(machine.status().state).toBe('OFF')
        expect(events.states()).toEqual(['ON', 'STOPPING', 'OFF'])
    })

    test('a transition that fails lands in ERROR carrying why', async () => {
        const machine = await createServiceStateMachine('web', service, events)
        machine.start()
        service.settle(new Error('port already bound'))
        await settled()
        expect(machine.status()).toMatchObject({ state: 'ERROR', error: 'port already bound' })
    })

    test('nothing is accepted while a transition is in flight', async () => {
        const machine = await createServiceStateMachine('web', service, events)
        machine.start()
        expect(() => machine.stop()).toThrow(new ApiError('SERVICE_BUSY'))
        expect(() => machine.start()).toThrow(new ApiError('SERVICE_BUSY'))
        await expect(machine.refresh()).rejects.toThrow(new ApiError('SERVICE_BUSY'))
        service.settle()
        await settled()
    })

    test('a command the state has nowhere to go with is refused', async () => {
        service.observed = 'ON'
        const machine = await createServiceStateMachine('web', service, events)
        expect(() => machine.start()).toThrow(new ApiError('SERVICE_BUSY'))
    })

    test('a refresh adopts what the service reports, and only pushes a change', async () => {
        const machine = await createServiceStateMachine('web', service, events)
        expect((await machine.refresh()).state).toBe('OFF')
        expect(events.states()).toEqual([])

        service.observed = 'ON'
        expect((await machine.refresh()).state).toBe('ON')
        expect(events.states()).toEqual(['ON'])
    })

    test('a refresh that cannot read the service is itself an ERROR', async () => {
        const machine = await createServiceStateMachine('web', service, events)
        service.check = () => Promise.reject(new Error('no such unit'))
        expect(await machine.refresh()).toMatchObject({ state: 'ERROR', error: 'no such unit' })
    })

    test('a refresh out of ERROR clears the message', async () => {
        const machine = await createServiceStateMachine('web', service, events)
        machine.start()
        service.settle(new Error('port already bound'))
        await settled()
        expect(await machine.refresh()).toMatchObject({ state: 'OFF', error: null })
    })
})

describe('the methods', () => {
    test('all four, admin-only by default, each answering the status of the service it names', async () => {
        const service = controllable()
        const methods = serviceStateMethods([await createServiceStateMachine('web', service, sink())])
        expect(Object.keys(methods)).toEqual(['service.status', 'service.start', 'service.stop', 'service.refresh'])
        expect(Object.values(methods).every(def => def.auth === 'admin')).toBe(true)

        const web = { service: 'web' }
        expect(await methods['service.status'].handler(ctx, web)).toMatchObject({ service: 'web', state: 'OFF' })
        expect(await methods['service.start'].handler(ctx, web)).toMatchObject({ state: 'STARTING' })
        service.settle()
        await settled()
        expect(await methods['service.stop'].handler(ctx, web)).toMatchObject({ state: 'STOPPING' })
        service.settle()
        await settled()
        expect(await methods['service.refresh'].handler(ctx, web)).toMatchObject({ state: 'OFF' })
    })

    test('one table over several services, each call reaching only the one it names', async () => {
        const events = sink()
        const [web, worker] = [controllable(), controllable()]
        const methods = serviceStateMethods([await createServiceStateMachine('web', web, events), await createServiceStateMachine('worker', worker, events)])

        expect(await methods['service.start'].handler(ctx, { service: 'worker' })).toMatchObject({ service: 'worker', state: 'STARTING' })
        expect(await methods['service.status'].handler(ctx, { service: 'web' })).toMatchObject({ service: 'web', state: 'OFF' })
        expect(events.pushes.map(status => [status.service, status.state])).toEqual([['worker', 'STARTING']])
        worker.settle()
        await settled()
    })

    test('a service the table does not serve is not found', async () => {
        const methods = serviceStateMethods([await createServiceStateMachine('web', controllable(), sink())])
        expect(() => methods['service.status'].handler(ctx, { service: 'db' })).toThrow(new ApiError('SERVICE_NOT_FOUND'))
    })

    test('two machines under one name are refused as the table is built', async () => {
        const [first, second] = [await createServiceStateMachine('web', controllable(), sink()), await createServiceStateMachine('web', controllable(), sink())]
        expect(() => serviceStateMethods([first, second])).toThrow('two machines share a name')
    })

    test('a call that names no service is refused before any machine is reached', async () => {
        const dispatcher = createDispatcher(serviceStateMethods([await createServiceStateMachine('web', controllable(), sink())]))
        expect(await dispatcher.dispatch({ method: 'service.status', arguments: [{}] }, ctx)).toMatchObject({
            success: false,
            error: 'INVALID_ARGUMENT',
            error_details: [{ field: 'service' }]
        })
    })

    test('the auth requirement can be relaxed for a read-only console', async () => {
        const machine = await createServiceStateMachine('web', controllable(), sink())
        expect(serviceStateMethods([machine], 'user')['service.status'].auth).toBe('user')
    })
})
