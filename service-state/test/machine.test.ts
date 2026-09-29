import { afterEach, beforeEach, describe, expect, jest, test } from 'bun:test'
import { ApiError, createDispatcher } from '@greendrake/rpc-server'
import type { Audience, EventSink } from '@greendrake/rpc-server'
import { SERVICE_STATE_EVENT, createServiceStateMachine, serviceStateMethods } from '../src/server'
import type { ServiceImplementation, ServiceState, ServiceStateMachine, ServiceStatus } from '../src/server'

type Sink = EventSink & Audience & { states: () => ServiceState[]; pushes: ServiceStatus[]; watch: () => void; leave: () => void }

// Every push the machine sends, so a test asserts the sequence an operator's
// screen would go through rather than only where it ended up — and an
// audience a test opens and closes, as sockets come and go on a registry.
const sink = (): Sink => {
    const pushes: ServiceStatus[] = []
    const listeners = new Set<(present: boolean) => void>()
    let watchers = 0
    return {
        pushes,
        states: () => pushes.map(status => status.state),
        emitToUser: () => {
            throw new Error('the machine addresses nobody: a service has one state, and everyone watching shares it')
        },
        broadcastEvent: (event, data) => {
            expect(event).toBe(SERVICE_STATE_EVENT)
            pushes.push(data as ServiceStatus)
        },
        get size() {
            return watchers
        },
        onPresence: listener => {
            listeners.add(listener)
            return () => {
                listeners.delete(listener)
            }
        },
        watch: () => {
            watchers += 1
            if (watchers === 1) listeners.forEach(listener => listener(true))
        },
        leave: () => {
            watchers -= 1
            if (watchers === 0) listeners.forEach(listener => listener(false))
        }
    }
}

// A service whose transitions are resolved by hand, so a test can hold one
// open and look at the machine while it is mid-flight. `reads` counts the
// times it was checked.
const controllable = (): ServiceImplementation & { settle: (outcome?: Error) => void; observed: ServiceState; reads: number } => {
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
        reads: 0,
        start: () => transition('ON'),
        stop: () => transition('OFF'),
        check: () => {
            service.reads += 1
            return Promise.resolve(service.observed)
        },
        settle: (outcome?: Error) => (outcome ? fail!(outcome) : release!())
    }
    return service
}

// Lets the machine's promise chains run to their end. Each is a few microtasks
// deep — a check or transition settling, its handler, its `finally` — and the
// microtask queue drains in order whatever the clock, so this needs no timer:
// under bun's fake timers, a timer-based wait (even setImmediate) can hang.
const settled = async (): Promise<void> => {
    for (let tick = 0; tick < 10; tick++) {
        await Promise.resolve()
    }
}

// Moves the fake clock, then lets whatever read that set off settle.
const elapse = async (ms: number): Promise<void> => {
    jest.advanceTimersByTime(ms)
    await settled()
}

// How old a reading may get before the machine takes another. Past anything a
// test takes on the real clock, so only the tests on the fake one see a
// re-read.
const AGE_MS = 60_000

// Every machine a test makes, closed after it so no re-read outlives the test
// — and the real clock back, for the tests that swapped in the fake one.
const made: ServiceStateMachine[] = []
const make = async (name: string, service: ServiceImplementation, events: EventSink & Audience): Promise<ServiceStateMachine> => {
    const machine = await createServiceStateMachine(name, service, events, AGE_MS)
    made.push(machine)
    return machine
}

afterEach(() => {
    for (const machine of made.splice(0)) {
        machine.close()
    }
    jest.useRealTimers()
})

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
        const machine = await make('web', service, events)
        expect((await machine.status()).state).toBe('ON')
        expect(events.states()).toEqual(['ON'])
    })

    test('a boot that finds the service where it assumed pushes nothing', async () => {
        const machine = await make('web', service, events)
        expect((await machine.status()).state).toBe('OFF')
        expect(events.states()).toEqual([])
    })

    test('every status names its service, the pushes included', async () => {
        const machine = await make('web', service, events)
        expect((await machine.status()).service).toBe('web')
        machine.start()
        service.settle()
        await settled()
        expect(events.pushes.map(status => status.service)).toEqual(['web', 'web'])
    })

    test('starting goes through STARTING and lands ON', async () => {
        const machine = await make('web', service, events)
        expect(machine.start().state).toBe('STARTING')
        expect((await machine.status()).state).toBe('STARTING')
        service.settle()
        await settled()
        expect((await machine.status()).state).toBe('ON')
        expect(events.states()).toEqual(['STARTING', 'ON'])
    })

    test('stopping goes through STOPPING and lands OFF', async () => {
        service.observed = 'ON'
        const machine = await make('web', service, events)
        expect(machine.stop().state).toBe('STOPPING')
        service.settle()
        await settled()
        expect((await machine.status()).state).toBe('OFF')
        expect(events.states()).toEqual(['ON', 'STOPPING', 'OFF'])
    })

    test('a transition that fails lands in ERROR carrying why', async () => {
        const machine = await make('web', service, events)
        machine.start()
        service.settle(new Error('port already bound'))
        await settled()
        expect(await machine.status()).toMatchObject({ state: 'ERROR', error: 'port already bound' })
    })

    test('nothing is accepted while a transition is in flight', async () => {
        const machine = await make('web', service, events)
        machine.start()
        expect(() => machine.stop()).toThrow(new ApiError('SERVICE_BUSY'))
        expect(() => machine.start()).toThrow(new ApiError('SERVICE_BUSY'))
        await expect(machine.refresh()).rejects.toThrow(new ApiError('SERVICE_BUSY'))
        service.settle()
        await settled()
    })

    test('a command the state has nowhere to go with is refused', async () => {
        service.observed = 'ON'
        const machine = await make('web', service, events)
        expect(() => machine.start()).toThrow(new ApiError('SERVICE_BUSY'))
    })

    test('a refresh adopts what the service reports, and only pushes a change', async () => {
        const machine = await make('web', service, events)
        expect((await machine.refresh()).state).toBe('OFF')
        expect(events.states()).toEqual([])

        service.observed = 'ON'
        expect((await machine.refresh()).state).toBe('ON')
        expect(events.states()).toEqual(['ON'])
    })

    test('a refresh that cannot read the service is itself an ERROR', async () => {
        const machine = await make('web', service, events)
        service.check = () => Promise.reject(new Error('no such unit'))
        expect(await machine.refresh()).toMatchObject({ state: 'ERROR', error: 'no such unit' })
    })

    test('a refresh out of ERROR clears the message', async () => {
        const machine = await make('web', service, events)
        machine.start()
        service.settle(new Error('port already bound'))
        await settled()
        expect(await machine.refresh()).toMatchObject({ state: 'OFF', error: null })
    })

    test('a transition begun while a read is out goes ahead, and the read is not adopted over it', async () => {
        const machine = await make('web', service, events)
        const held = Promise.withResolvers<ServiceState>()
        service.check = () => held.promise
        const refreshed = machine.refresh()
        expect(machine.start().state).toBe('STARTING')
        held.resolve('OFF')
        expect((await refreshed).state).toBe('STARTING')
        service.settle()
        await settled()
        expect(events.states()).toEqual(['STARTING', 'ON'])
    })

    test('a refresh asked while a read is out joins it', async () => {
        const machine = await make('web', service, events)
        const held = Promise.withResolvers<ServiceState>()
        let reads = 0
        service.check = () => {
            reads += 1
            return held.promise
        }
        const asked = [machine.refresh(), machine.refresh()]
        held.resolve('ON')
        expect((await Promise.all(asked)).map(status => status.state)).toEqual(['ON', 'ON'])
        expect(reads).toBe(1)
    })
})

describe('keeping the state current while somebody watches', () => {
    let service: ReturnType<typeof controllable>
    let events: ReturnType<typeof sink>

    beforeEach(() => {
        jest.useFakeTimers()
        service = controllable()
        events = sink()
        events.watch()
    })

    test('a reading the age old is taken again, and what changed is pushed to everyone watching', async () => {
        const machine = await make('web', service, events)
        service.observed = 'ON'
        await elapse(AGE_MS - 1)
        expect(service.reads).toBe(1)
        await elapse(1)
        expect(service.reads).toBe(2)
        expect((await machine.status()).state).toBe('ON')
        expect(events.states()).toEqual(['ON'])
    })

    test('every reading schedules the next, and a steady service pushes nothing', async () => {
        await make('web', service, events)
        await elapse(AGE_MS)
        await elapse(AGE_MS)
        expect(service.reads).toBe(3)
        expect(events.states()).toEqual([])
    })

    test('a state the service was read in is read again like any other, STARTING included', async () => {
        service.observed = 'STARTING'
        await make('web', service, events)
        service.observed = 'ON'
        await elapse(AGE_MS)
        expect(events.states()).toEqual(['STARTING', 'ON'])
    })

    test('a refresh is a reading: the next re-read is timed from it', async () => {
        const machine = await make('web', service, events)
        await elapse(AGE_MS - 1)
        await machine.refresh()
        await elapse(1)
        expect(service.reads).toBe(2)
        await elapse(AGE_MS - 1)
        expect(service.reads).toBe(3)
    })

    test('nothing is read while a transition is under way, and one that lands counts as a reading', async () => {
        const machine = await make('web', service, events)
        await elapse(AGE_MS - 1)
        machine.start()
        await elapse(3 * AGE_MS)
        expect(service.reads).toBe(1)
        service.settle()
        await settled()
        await elapse(AGE_MS - 1)
        expect(service.reads).toBe(1)
        await elapse(1)
        expect(service.reads).toBe(2)
    })

    test('ERROR is not read past: the re-reads stop until somebody refreshes', async () => {
        const machine = await make('web', service, events)
        machine.start()
        service.settle(new Error('port already bound'))
        await settled()
        await elapse(3 * AGE_MS)
        expect(service.reads).toBe(1)
        expect(await machine.status()).toMatchObject({ state: 'ERROR', error: 'port already bound' })
        await machine.refresh()
        await elapse(AGE_MS)
        expect(service.reads).toBe(3)
    })

    test('a re-read that cannot read the service lands in ERROR, and stops there', async () => {
        const machine = await make('web', service, events)
        service.check = () => {
            service.reads += 1
            return Promise.reject(new Error('no such unit'))
        }
        await elapse(AGE_MS)
        expect(await machine.status()).toMatchObject({ state: 'ERROR', error: 'no such unit' })
        await elapse(3 * AGE_MS)
        expect(service.reads).toBe(2)
    })

    test('a transition begun while a re-read is out goes ahead, and the re-read is not adopted over it', async () => {
        const machine = await make('web', service, events)
        const held = Promise.withResolvers<ServiceState>()
        service.check = () => held.promise
        jest.advanceTimersByTime(AGE_MS)
        expect(machine.start().state).toBe('STARTING')
        held.resolve('OFF')
        await settled()
        expect((await machine.status()).state).toBe('STARTING')
        service.settle()
        await settled()
        expect(events.states()).toEqual(['STARTING', 'ON'])
    })

    test('a read left over from before a transition is not joined, so the re-reads carry on past it', async () => {
        const machine = await make('web', service, events)
        const leftover = Promise.withResolvers<ServiceState>()
        let reads = 0
        service.check = () => {
            reads += 1
            return reads === 1 ? leftover.promise : Promise.resolve(service.observed)
        }
        const refreshed = machine.refresh()
        machine.start()
        service.settle()
        await settled()
        service.observed = 'OFF'
        await elapse(AGE_MS)
        expect(reads).toBe(2)
        expect((await machine.status()).state).toBe('OFF')
        leftover.resolve('ON')
        expect((await refreshed).state).toBe('OFF')
        await elapse(AGE_MS)
        expect(reads).toBe(3)
    })

    test('close stops the re-reads, a read already out included', async () => {
        const machine = await make('web', service, events)
        const held = Promise.withResolvers<ServiceState>()
        let reads = 0
        service.check = () => {
            reads += 1
            return held.promise
        }
        jest.advanceTimersByTime(AGE_MS)
        machine.close()
        held.resolve('OFF')
        await elapse(3 * AGE_MS)
        expect(reads).toBe(1)
    })
})

describe('reading the service only when somebody needs it', () => {
    let service: ReturnType<typeof controllable>
    let events: ReturnType<typeof sink>

    beforeEach(() => {
        jest.useFakeTimers()
        service = controllable()
        events = sink()
    })

    test('with nobody asking and nobody watching, the service is left alone', async () => {
        await make('web', service, events)
        await elapse(3 * AGE_MS)
        expect(service.reads).toBe(1)
    })

    test('a status asked of a reading the age old reads first, and answers what it found', async () => {
        const machine = await make('web', service, events)
        service.observed = 'ON'
        await elapse(AGE_MS)
        expect(service.reads).toBe(1)
        expect((await machine.status()).state).toBe('ON')
        expect(service.reads).toBe(2)
        expect(events.states()).toEqual(['ON'])
    })

    test('a status asked of a younger reading answers from it', async () => {
        const machine = await make('web', service, events)
        service.observed = 'ON'
        await elapse(AGE_MS - 1)
        expect((await machine.status()).state).toBe('OFF')
        expect(service.reads).toBe(1)
    })

    test('the first to watch a stale reading has it taken again at once, and the last to leave stops the re-reads', async () => {
        await make('web', service, events)
        await elapse(2 * AGE_MS)
        events.watch()
        await elapse(0)
        expect(service.reads).toBe(2)
        events.watch()
        await elapse(AGE_MS)
        expect(service.reads).toBe(3)
        events.leave()
        await elapse(AGE_MS)
        expect(service.reads).toBe(4)
        events.leave()
        await elapse(3 * AGE_MS)
        expect(service.reads).toBe(4)
    })

    test('watching again takes the cadence up from the last reading', async () => {
        await make('web', service, events)
        events.watch()
        await elapse(AGE_MS / 2)
        events.leave()
        await elapse(AGE_MS / 4)
        events.watch()
        await elapse(AGE_MS / 4 - 1)
        expect(service.reads).toBe(1)
        await elapse(1)
        expect(service.reads).toBe(2)
    })

    test('a watcher arriving mid-transition waits for it to land before anything is read', async () => {
        const machine = await make('web', service, events)
        await elapse(2 * AGE_MS)
        machine.start()
        events.watch()
        await elapse(3 * AGE_MS)
        expect(service.reads).toBe(1)
        service.settle()
        await settled()
        await elapse(AGE_MS - 1)
        expect(service.reads).toBe(1)
        await elapse(1)
        expect(service.reads).toBe(2)
    })

    test('close stops listening for watchers', async () => {
        const machine = await make('web', service, events)
        await elapse(2 * AGE_MS)
        machine.close()
        events.watch()
        await elapse(3 * AGE_MS)
        expect(service.reads).toBe(1)
    })
})

describe('the methods', () => {
    test('all four, admin-only by default, each answering the status of the service it names', async () => {
        const service = controllable()
        const methods = serviceStateMethods([await make('web', service, sink())])
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
        const methods = serviceStateMethods([await make('web', web, events), await make('worker', worker, events)])

        expect(await methods['service.start'].handler(ctx, { service: 'worker' })).toMatchObject({ service: 'worker', state: 'STARTING' })
        expect(await methods['service.status'].handler(ctx, { service: 'web' })).toMatchObject({ service: 'web', state: 'OFF' })
        expect(events.pushes.map(status => [status.service, status.state])).toEqual([['worker', 'STARTING']])
        worker.settle()
        await settled()
    })

    test('a service the table does not serve is not found', async () => {
        const methods = serviceStateMethods([await make('web', controllable(), sink())])
        expect(() => methods['service.status'].handler(ctx, { service: 'db' })).toThrow(new ApiError('SERVICE_NOT_FOUND'))
    })

    test('two machines under one name are refused as the table is built', async () => {
        const [first, second] = [await make('web', controllable(), sink()), await make('web', controllable(), sink())]
        expect(() => serviceStateMethods([first, second])).toThrow('two machines share a name')
    })

    test('a call that names no service is refused before any machine is reached', async () => {
        const dispatcher = createDispatcher(serviceStateMethods([await make('web', controllable(), sink())]))
        expect(await dispatcher.dispatch({ method: 'service.status', arguments: [{}] }, ctx)).toMatchObject({
            success: false,
            error: 'INVALID_ARGUMENT',
            error_details: [{ field: 'service' }]
        })
    })

    test('the auth requirement can be relaxed for a read-only console', async () => {
        const machine = await make('web', controllable(), sink())
        expect(serviceStateMethods([machine], 'user')['service.status'].auth).toBe('user')
    })
})
