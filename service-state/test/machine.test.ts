import { afterEach, beforeEach, describe, expect, jest, test } from 'bun:test'
import { documentOf, mergePatchBetween, type JsonObject } from '@greendrake/rpc'
import { ApiError, createDispatcher, keyAuthResolver } from '@greendrake/rpc-server'
import type { Audience, Document, DocumentPublisher, HandlerContext, MethodAccess } from '@greendrake/rpc-server'
import { CONTROLS, SERVICE_STATES, SERVICE_STATE_DOCUMENT, ServiceBusyError, TRANSITIONS, createServiceStateMachine, serviceStateBoard, serviceStateMethods } from '../src/server'
import type { ServiceImplementation, ServiceState, ServiceStateBoard, ServiceStateMachine, ServiceStates } from '../src/server'

type Publisher = DocumentPublisher &
    Audience & {
        // The document as last published: what an opening socket is greeted with.
        doc: () => ServiceStates
        // Every document published since the board was made, so a test asserts
        // the sequence an operator's screen would go through rather than only
        // where it ended up.
        published: ServiceStates[]
        watch: () => void
        leave: () => void
    }

// A publisher as a registry is one: the document it keeps, published only where
// something changed, and an audience a test opens and closes, as sockets come
// and go.
const publisher = (): Publisher => {
    let doc: JsonObject = {}
    const published: ServiceStates[] = []
    const listeners = new Set<(present: boolean) => void>()
    let watchers = 0
    return {
        // The board's document is its ServiceStates, as it produced them.
        doc: () => doc as ServiceStates,
        published,
        document: (type, produce): Document => {
            expect(type).toBe(SERVICE_STATE_DOCUMENT)
            doc = documentOf(type, produce())
            return {
                refresh: () => {
                    const next = documentOf(type, produce())
                    if (mergePatchBetween(doc, next)) {
                        doc = next
                        published.push(next as ServiceStates)
                    }
                }
            }
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

// What a socket watching all along would have shown for a service, document by
// document: its state, or null while it has no entry.
const seen = (p: Publisher, name: string): (ServiceState | null)[] => p.published.map(doc => doc[name]?.state ?? null).filter((state, i, all) => i === 0 || state !== all[i - 1])

// A service whose transitions are resolved by hand, so a test can hold one
// open and look at the machine while it is mid-flight. `reads` counts the
// times it was checked.
const controllable = (): ServiceImplementation & { settle: (outcome?: Error) => void; observed: Exclude<ServiceState, 'ERROR'>; reads: number } => {
    let release: (() => void) | undefined
    let fail: ((e: Error) => void) | undefined
    const transition = (to: Exclude<ServiceState, 'ERROR'>): Promise<void> =>
        new Promise((resolve, reject) => {
            release = () => {
                service.observed = to
                resolve()
            }
            fail = reject
        })
    const service = {
        observed: 'OFF' as Exclude<ServiceState, 'ERROR'>,
        reads: 0,
        start: () => transition('ON'),
        stop: () => transition('OFF'),
        check: (): Promise<Exclude<ServiceState, 'ERROR'>> => {
            service.reads += 1
            return Promise.resolve(service.observed)
        },
        settle: (outcome?: Error) => (outcome ? fail!(outcome) : release!())
    }
    return service
}

// A check that fails, as one does on a service it cannot read.
const unreadable = (service: ReturnType<typeof controllable>): void => {
    service.check = () => {
        service.reads += 1
        return Promise.reject(new Error('no such unit'))
    }
}

// Lets the machine's promise chains run to their end. Each is a few microtasks
// deep — a check or transition settling, its handler, its `finally`, the
// command waiting on it — and the microtask queue drains in order whatever the
// clock, so this needs no timer: under bun's fake timers, a timer-based wait
// (even setImmediate) can hang.
const settled = async (): Promise<void> => {
    for (let tick = 0; tick < 20; tick++) {
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
const make = (name: string, service: ServiceImplementation, board: ServiceStateBoard): ServiceStateMachine => {
    const machine = createServiceStateMachine(name, service, board, AGE_MS)
    made.push(machine)
    return machine
}

// A board over a publisher somebody is already watching.
const watchedBoard = (): { p: Publisher; board: ServiceStateBoard } => {
    const p = publisher()
    p.watch()
    return { p, board: serviceStateBoard(p) }
}

// The web service's machine made on a board somebody watches, its first
// reading landed.
const ready = async (service: ServiceImplementation, board: ServiceStateBoard): Promise<ServiceStateMachine> => {
    const machine = make('web', service, board)
    await settled()
    return machine
}

afterEach(() => {
    for (const machine of made.splice(0)) {
        machine.close()
    }
    jest.useRealTimers()
})

// The application's policy over its principals, user ids here: root alone may
// work the services.
const ROOT_ONLY: MethodAccess<string> = {
    auth: 'required',
    access: caller => (caller === 'root' ? true : 'ADMIN_REQUIRED')
}

// What a handler is called with, from root.
const ctx: HandlerContext<string> = {
    principal: 'root',
    clientIp: '',
    locale: '',
    signal: AbortSignal.any([]),
    topics: null
}

describe('the contract', () => {
    test("a state's control offers each transition from it, a refresh out of ERROR, and nothing mid-transition", () => {
        for (const [command, { from, during }] of Object.entries(TRANSITIONS)) {
            expect(CONTROLS[from]).toBe(command as keyof typeof TRANSITIONS)
            expect(CONTROLS[during]).toBeNull()
        }
        expect(CONTROLS.ERROR).toBe('refresh')
        expect(Object.keys(CONTROLS).sort()).toEqual([...SERVICE_STATES].sort())
    })
})

describe('the board', () => {
    test('every machine is published in one document, under its name', async () => {
        const { p, board } = watchedBoard()
        const [web, worker] = [controllable(), controllable()]
        web.observed = 'ON'
        make('web', web, board)
        make('worker', worker, board)
        await settled()
        expect(p.doc()).toEqual({ web: { state: 'ON', changed_at: expect.any(Number) }, worker: { state: 'OFF', changed_at: expect.any(Number) } })
        expect(board.machine('worker')?.name).toBe('worker')
        expect(board.machine('db')).toBeUndefined()
    })

    test('a second machine of a name is refused', () => {
        const board = serviceStateBoard(publisher())
        make('web', controllable(), board)
        expect(() => make('web', controllable(), board)).toThrow('two machines are named web')
    })
})

describe('the machine, watched', () => {
    let service: ReturnType<typeof controllable>
    let p: Publisher
    let board: ServiceStateBoard

    beforeEach(() => {
        service = controllable()
        ;({ p, board } = watchedBoard())
    })

    test('it reads the service at once, and is published only once it has', async () => {
        service.observed = 'ON'
        make('web', service, board)
        expect(service.reads).toBe(1)
        expect(p.doc()).toEqual({})
        await settled()
        expect(p.doc().web?.state).toBe('ON')
    })

    test('starting goes through STARTING and lands ON', async () => {
        const machine = await ready(service, board)
        await machine.start()
        expect(p.doc().web?.state).toBe('STARTING')
        service.settle()
        await settled()
        expect(seen(p, 'web')).toEqual(['OFF', 'STARTING', 'ON'])
    })

    test('stopping goes through STOPPING and lands OFF', async () => {
        service.observed = 'ON'
        const machine = await ready(service, board)
        await machine.stop()
        service.settle()
        await settled()
        expect(seen(p, 'web')).toEqual(['ON', 'STOPPING', 'OFF'])
    })

    test('a transition that fails lands in ERROR carrying why', async () => {
        const machine = await ready(service, board)
        await machine.start()
        service.settle(new Error('port already bound'))
        await settled()
        expect(p.doc().web).toEqual({
            state: 'ERROR',
            error: 'port already bound',
            changed_at: expect.any(Number)
        })
    })

    test('a start that fails at once lands in ERROR, after STARTING', async () => {
        service.start = () => Promise.reject(new Error('port already bound'))
        const machine = await ready(service, board)
        await machine.start()
        await settled()
        expect(seen(p, 'web')).toEqual(['OFF', 'STARTING', 'ERROR'])
    })

    test('nothing is accepted while a transition is in flight', async () => {
        const machine = await ready(service, board)
        await machine.start()
        await expect(machine.stop()).rejects.toThrow(new ServiceBusyError('stop', 'STARTING'))
        await expect(machine.start()).rejects.toThrow(new ServiceBusyError('start', 'STARTING'))
        await expect(machine.refresh()).rejects.toThrow(new ServiceBusyError('refresh', 'STARTING'))
        service.settle()
        await settled()
    })

    test('a command the state has nowhere to go with is refused', async () => {
        service.observed = 'ON'
        const machine = await ready(service, board)
        await expect(machine.start()).rejects.toThrow(new ServiceBusyError('start', 'ON'))
    })

    test('a refresh adopts what the service reports, and publishes only a change', async () => {
        const machine = await ready(service, board)
        const published = p.published.length
        await machine.refresh()
        expect(p.published.length).toBe(published)
        service.observed = 'ON'
        await machine.refresh()
        expect(p.doc().web?.state).toBe('ON')
    })

    test('a refresh that cannot read the service is itself an ERROR', async () => {
        const machine = await ready(service, board)
        unreadable(service)
        await machine.refresh()
        expect(p.doc().web).toEqual({
            state: 'ERROR',
            error: 'no such unit',
            changed_at: expect.any(Number)
        })
    })

    test("a refresh out of a failed transition's ERROR takes the reading, and the why goes with it", async () => {
        const machine = await ready(service, board)
        await machine.start()
        service.settle(new Error('port already bound'))
        await settled()
        await machine.refresh()
        expect(p.doc().web).toEqual({ state: 'OFF', changed_at: expect.any(Number) })
    })

    test('a transition begun while a read is out goes ahead, and the read is not adopted over it', async () => {
        const machine = await ready(service, board)
        const held = Promise.withResolvers<Exclude<ServiceState, 'ERROR'>>()
        service.check = () => held.promise
        const refreshed = machine.refresh()
        await machine.start()
        held.resolve('OFF')
        await refreshed
        expect(p.doc().web?.state).toBe('STARTING')
        service.settle()
        await settled()
        expect(seen(p, 'web')).toEqual(['OFF', 'STARTING', 'ON'])
    })

    test('a refresh asked while a read is out joins it', async () => {
        const machine = await ready(service, board)
        const held = Promise.withResolvers<Exclude<ServiceState, 'ERROR'>>()
        let reads = 0
        service.check = () => {
            reads += 1
            return held.promise
        }
        const asked = [machine.refresh(), machine.refresh()]
        held.resolve('ON')
        await Promise.all(asked)
        expect(reads).toBe(1)
        expect(p.doc().web?.state).toBe('ON')
    })
})

describe('commands act on a verified state', () => {
    let service: ReturnType<typeof controllable>
    let board: ServiceStateBoard

    beforeEach(() => {
        jest.useFakeTimers()
        service = controllable()
        board = serviceStateBoard(publisher())
    })

    test('a command on a machine never read reads first, and is refused or taken as the reading says', async () => {
        service.observed = 'ON'
        const machine = make('web', service, board)
        expect(service.reads).toBe(0)
        await expect(machine.start()).rejects.toThrow(new ServiceBusyError('start', 'ON'))
        expect(service.reads).toBe(1)
        // The reading is fresh, so the next command acts on it.
        await machine.stop()
        expect(service.reads).toBe(1)
        service.settle()
        await settled()
    })

    test('a reading the age old is taken again before a command', async () => {
        const machine = make('web', service, board)
        await machine.refresh()
        service.observed = 'ON'
        await elapse(AGE_MS)
        await expect(machine.start()).rejects.toThrow(new ServiceBusyError('start', 'ON'))
        expect(service.reads).toBe(2)
    })

    test("a command mid-transition, or in a failed transition's ERROR, is refused without a read, however old the reading", async () => {
        const machine = make('web', service, board)
        await machine.refresh()
        await machine.start()
        await elapse(3 * AGE_MS)
        await expect(machine.stop()).rejects.toThrow(new ServiceBusyError('stop', 'STARTING'))
        service.settle(new Error('port already bound'))
        await settled()
        await elapse(3 * AGE_MS)
        await expect(machine.start()).rejects.toThrow(new ServiceBusyError('start', 'ERROR'))
        expect(service.reads).toBe(1)
    })
})

describe('publishing only what somebody watched being taken', () => {
    let p: Publisher
    let board: ServiceStateBoard

    beforeEach(() => {
        jest.useFakeTimers()
        p = publisher()
        board = serviceStateBoard(p)
    })

    test('with nobody watching and nobody commanding, the service is left alone, and nothing is published', async () => {
        const service = controllable()
        make('web', service, board)
        await elapse(3 * AGE_MS)
        expect(service.reads).toBe(0)
        expect(p.published).toEqual([])
    })

    test('a reading taken with nobody watching is not published: a refresh, a landing, a failed check', async () => {
        const service = controllable()
        const machine = make('web', service, board)
        await machine.refresh()
        expect(p.doc()).toEqual({})
        // The transition under way is the machine's own word, and is published.
        await machine.start()
        expect(p.doc().web?.state).toBe('STARTING')
        service.settle()
        await settled()
        expect(p.doc()).toEqual({})
        unreadable(service)
        await machine.refresh()
        expect(p.doc()).toEqual({})
    })

    test('the first to arrive is greeted with no reading, and has every machine without an entry read at once', async () => {
        const [web, worker] = [controllable(), controllable()]
        worker.observed = 'ON'
        await make('web', web, board).refresh()
        await make('worker', worker, board).refresh()
        p.watch()
        expect(p.doc()).toEqual({})
        expect([web.reads, worker.reads]).toEqual([2, 2])
        await settled()
        expect(p.doc()).toEqual({ web: { state: 'OFF', changed_at: expect.any(Number) }, worker: { state: 'ON', changed_at: expect.any(Number) } })
    })

    test("the last to leave takes every reading out, a failed check's ERROR among them, and keeps the machine's own word", async () => {
        p.watch()
        const [idle, broken, failing, busy] = [controllable(), controllable(), controllable(), controllable()]
        unreadable(broken)
        make('idle', idle, board)
        make('broken', broken, board)
        const failingMachine = make('failing', failing, board)
        const busyMachine = make('busy', busy, board)
        await settled()
        await failingMachine.start()
        failing.settle(new Error('port already bound'))
        await busyMachine.start()
        await settled()
        expect(Object.keys(p.doc()).sort()).toEqual(['broken', 'busy', 'failing', 'idle'])
        p.leave()
        expect(p.doc()).toEqual({
            failing: {
                state: 'ERROR',
                error: 'port already bound',
                changed_at: expect.any(Number)
            },
            busy: { state: 'STARTING', changed_at: expect.any(Number) }
        })
        // Begun with somebody watching, landed with nobody: a reading like any
        // other, which nobody saw taken.
        busy.settle()
        await settled()
        expect(p.doc().busy).toBeUndefined()
    })

    test("a machine in its own word is not read when the first watcher arrives, and a transition's landing then counts as its reading", async () => {
        const [failing, busy] = [controllable(), controllable()]
        const failingMachine = make('failing', failing, board)
        const busyMachine = make('busy', busy, board)
        await failingMachine.start()
        failing.settle(new Error('port already bound'))
        await busyMachine.start()
        await settled()
        p.watch()
        await elapse(3 * AGE_MS)
        expect([failing.reads, busy.reads]).toEqual([1, 1])
        busy.settle()
        await settled()
        expect(p.doc().busy?.state).toBe('ON')
        await elapse(AGE_MS - 1)
        expect(busy.reads).toBe(1)
        await elapse(1)
        expect(busy.reads).toBe(2)
    })

    test('close stops listening for watchers', async () => {
        const service = controllable()
        make('web', service, board).close()
        p.watch()
        await elapse(3 * AGE_MS)
        expect(service.reads).toBe(0)
    })
})

describe('keeping the state current while somebody watches', () => {
    let service: ReturnType<typeof controllable>
    let p: Publisher
    let board: ServiceStateBoard

    beforeEach(() => {
        jest.useFakeTimers()
        service = controllable()
        ;({ p, board } = watchedBoard())
    })

    test('a reading the age old is taken again, and what changed is published', async () => {
        await ready(service, board)
        service.observed = 'ON'
        await elapse(AGE_MS - 1)
        expect(service.reads).toBe(1)
        await elapse(1)
        expect(service.reads).toBe(2)
        expect(seen(p, 'web')).toEqual(['OFF', 'ON'])
    })

    test('every reading schedules the next, and a steady service publishes nothing new', async () => {
        await ready(service, board)
        const published = p.published.length
        await elapse(AGE_MS)
        await elapse(AGE_MS)
        expect(service.reads).toBe(3)
        expect(p.published.length).toBe(published)
    })

    test('a state the service was read in is read again like any other, STARTING included', async () => {
        service.observed = 'STARTING'
        await ready(service, board)
        service.observed = 'ON'
        await elapse(AGE_MS)
        expect(seen(p, 'web')).toEqual(['STARTING', 'ON'])
    })

    test('a refresh is a reading: the next re-read is timed from it', async () => {
        const machine = await ready(service, board)
        await elapse(AGE_MS - 1)
        await machine.refresh()
        await elapse(1)
        expect(service.reads).toBe(2)
        await elapse(AGE_MS - 1)
        expect(service.reads).toBe(3)
    })

    test('nothing is read while a transition is under way, and one that lands counts as a reading', async () => {
        const machine = await ready(service, board)
        await elapse(AGE_MS - 1)
        await machine.start()
        await elapse(3 * AGE_MS)
        expect(service.reads).toBe(1)
        service.settle()
        await settled()
        await elapse(AGE_MS - 1)
        expect(service.reads).toBe(1)
        await elapse(1)
        expect(service.reads).toBe(2)
    })

    test("a failed transition's ERROR is not read past: the re-reads stop until somebody refreshes", async () => {
        const machine = await ready(service, board)
        await machine.start()
        service.settle(new Error('port already bound'))
        await settled()
        await elapse(3 * AGE_MS)
        expect(service.reads).toBe(1)
        expect(p.doc().web).toEqual({
            state: 'ERROR',
            error: 'port already bound',
            changed_at: expect.any(Number)
        })
        await machine.refresh()
        await elapse(AGE_MS)
        expect(service.reads).toBe(3)
    })

    test("a failed check's ERROR is a reading like any other: the re-reads carry on past it", async () => {
        await ready(service, board)
        unreadable(service)
        await elapse(AGE_MS)
        expect(p.doc().web).toEqual({
            state: 'ERROR',
            error: 'no such unit',
            changed_at: expect.any(Number)
        })
        service.check = () => {
            service.reads += 1
            return Promise.resolve('ON')
        }
        await elapse(AGE_MS)
        expect(service.reads).toBe(3)
        expect(seen(p, 'web')).toEqual(['OFF', 'ERROR', 'ON'])
    })

    test('a command asked while a re-read is out joins it, and acts on what it finds', async () => {
        const machine = await ready(service, board)
        const held = Promise.withResolvers<Exclude<ServiceState, 'ERROR'>>()
        let reads = 0
        service.check = () => {
            reads += 1
            return held.promise
        }
        jest.advanceTimersByTime(AGE_MS)
        const started = machine.start()
        held.resolve('ON')
        await expect(started).rejects.toThrow(new ServiceBusyError('start', 'ON'))
        expect(reads).toBe(1)
    })

    test('a read left over from before a transition is not joined, so the re-reads carry on past it', async () => {
        const machine = await ready(service, board)
        const leftover = Promise.withResolvers<Exclude<ServiceState, 'ERROR'>>()
        let reads = 0
        service.check = () => {
            reads += 1
            return reads === 1 ? leftover.promise : Promise.resolve(service.observed)
        }
        const refreshed = machine.refresh()
        await machine.start()
        service.settle()
        await settled()
        service.observed = 'OFF'
        await elapse(AGE_MS)
        expect(reads).toBe(2)
        expect(p.doc().web?.state).toBe('OFF')
        leftover.resolve('ON')
        await refreshed
        expect(p.doc().web?.state).toBe('OFF')
        await elapse(AGE_MS)
        expect(reads).toBe(3)
    })

    test('close stops the re-reads, a read already out included', async () => {
        const machine = await ready(service, board)
        const held = Promise.withResolvers<Exclude<ServiceState, 'ERROR'>>()
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

    test('the last to leave stops the re-reads', async () => {
        await ready(service, board)
        p.leave()
        await elapse(3 * AGE_MS)
        expect(service.reads).toBe(1)
    })
})

describe('the methods', () => {
    let p: Publisher
    let board: ServiceStateBoard

    beforeEach(() => {
        ;({ p, board } = watchedBoard())
    })

    test("all three, under the application's policy, each acting on the service it names", async () => {
        const service = controllable()
        await ready(service, board)
        const methods = serviceStateMethods(board, ROOT_ONLY)
        expect(Object.keys(methods)).toEqual(['service.start', 'service.stop', 'service.refresh'])
        for (const def of Object.values(methods)) {
            expect(def.auth).toBe('required')
            expect(def.access?.('someone')).toBe('ADMIN_REQUIRED')
        }

        const web = { service: 'web' }
        expect(await methods['service.start'].handler(ctx, web)).toBeUndefined()
        expect(p.doc().web?.state).toBe('STARTING')
        service.settle()
        await settled()
        await methods['service.stop'].handler(ctx, web)
        service.settle()
        await settled()
        expect(await methods['service.refresh'].handler(ctx, web)).toBeUndefined()
        expect(seen(p, 'web')).toEqual(['OFF', 'STARTING', 'ON', 'STOPPING', 'OFF'])
    })

    test('one board over several services, each call reaching only the one it names', async () => {
        const [web, worker] = [controllable(), controllable()]
        make('web', web, board)
        make('worker', worker, board)
        await settled()
        await serviceStateMethods(board, ROOT_ONLY)['service.start'].handler(ctx, { service: 'worker' })
        expect([p.doc().web?.state, p.doc().worker?.state]).toEqual(['OFF', 'STARTING'])
        worker.settle()
        await settled()
    })

    test('a command the state cannot take is SERVICE_BUSY on the wire', async () => {
        const service = controllable()
        await ready(service, board)
        const methods = serviceStateMethods(board, ROOT_ONLY)
        await methods['service.start'].handler(ctx, { service: 'web' })
        await expect(methods['service.start'].handler(ctx, { service: 'web' })).rejects.toThrow(new ApiError('SERVICE_BUSY'))
        service.settle()
        await settled()
    })

    test('a service the board does not have is not found', async () => {
        make('web', controllable(), board)
        await expect(serviceStateMethods(board, ROOT_ONLY)['service.refresh'].handler(ctx, { service: 'db' })).rejects.toThrow(new ApiError('SERVICE_NOT_FOUND'))
    })

    test('a call that names no service is refused before any machine is reached', async () => {
        make('web', controllable(), board)
        const dispatcher = createDispatcher(serviceStateMethods(board, ROOT_ONLY), { auth: keyAuthResolver('key', 'root') })
        expect(await dispatcher.dispatch({ method: 'service.refresh', arguments: [{}] }, ctx)).toMatchObject({
            success: false,
            error: 'INVALID_ARGUMENT',
            error_details: [{ field: 'service' }]
        })
    })

    test('a read-only console may leave them open', () => {
        const methods = serviceStateMethods(board, { auth: 'none' })
        expect(methods['service.refresh'].auth).toBe('none')
        expect(methods['service.refresh'].access).toBeUndefined()
    })
})
