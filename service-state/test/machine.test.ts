import { afterEach, beforeEach, describe, expect, jest, test } from 'bun:test'
import { documentOf, mergePatchBetween, type JsonObject } from '@greendrake/rpc'
import { ApiError, createDispatcher, keyAuthResolver } from '@greendrake/rpc-server'
import type { Audience, Document, DocumentOptions, DocumentPublisher, HandlerContext, MethodAccess, Topics } from '@greendrake/rpc-server'
import { CONTROLS, SERVICE_STATES, ServiceBusyError, TRANSITIONS, createServiceStateMachine, serviceStateBoard, serviceStateDocument, serviceStateMethods } from '../src/server'
import type { ServiceDocument, ServiceImplementation, ServiceState, ServiceStateBoard, ServiceStateMachine } from '../src/server'

type Publisher = DocumentPublisher & {
    // A socket joining and leaving the service's topic, as service.watch and
    // service.unwatch have it.
    watch: (name: string) => void
    unwatch: (name: string) => void
    // The joining and leaving a call's context hands a method, on the same
    // topics.
    topics: Topics
    // The service's document as last published: what a socket starting to
    // watch it is greeted with.
    doc: (name: string) => ServiceDocument
    // What a socket watching all along would have shown for the service,
    // document by document: its state, or null while it has no status — the
    // sequence an operator's screen would go through rather than only where it
    // ended up.
    seen: (name: string) => (ServiceState | null)[]
}

// A publisher as a registry is one, each document kept by the members of its
// topic: published only where something changed, and an audience a test opens
// and closes, as sockets join and leave.
const publisher = (): Publisher => {
    const kept = new Map<string, { doc: JsonObject; published: JsonObject[] }>()
    const members = new Map<string, number>()
    const listeners = new Map<string, Set<(present: boolean) => void>>()
    const join = (topic: string): void => {
        const joined = (members.get(topic) ?? 0) + 1
        members.set(topic, joined)
        if (joined === 1) listeners.get(topic)?.forEach(listener => listener(true))
    }
    const leave = (topic: string): void => {
        const left = (members.get(topic) ?? 0) - 1
        members.set(topic, left)
        if (left === 0) listeners.get(topic)?.forEach(listener => listener(false))
    }
    // The board keeps every document on a topic of its own name.
    function document(type: string, produce: () => object): Document
    function document(type: string, produce: () => object, options: DocumentOptions): Document & Audience
    function document(type: string, produce: () => object, options?: DocumentOptions): Document & Audience {
        expect(options?.topic).toBe(type)
        const entry = { doc: documentOf(type, produce()), published: [] as JsonObject[] }
        kept.set(type, entry)
        return {
            refresh: () => {
                const next = documentOf(type, produce())
                if (mergePatchBetween(entry.doc, next)) {
                    entry.doc = next
                    entry.published.push(next)
                }
            },
            get size() {
                return members.get(type) ?? 0
            },
            onPresence: listener => {
                const set = listeners.get(type) ?? new Set()
                listeners.set(type, set.add(listener))
                return () => {
                    set.delete(listener)
                }
            }
        }
    }
    // A service's document is its ServiceDocument, as the machine produced it.
    const docOf = (doc: JsonObject): ServiceDocument => doc as ServiceDocument
    return {
        document,
        watch: name => join(serviceStateDocument(name)),
        unwatch: name => leave(serviceStateDocument(name)),
        topics: { join, leave },
        doc: name => docOf(kept.get(serviceStateDocument(name))?.doc ?? {}),
        seen: name => (kept.get(serviceStateDocument(name))?.published ?? []).map(doc => docOf(doc).status?.state ?? null).filter((state, i, all) => i === 0 || state !== all[i - 1])
    }
}

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

// A board over a publisher on which a socket already watches the web service.
const watchedBoard = (): { p: Publisher; board: ServiceStateBoard } => {
    const p = publisher()
    p.watch('web')
    return { p, board: serviceStateBoard(p) }
}

// The web service's machine made on a board somebody watches it on, its first
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

// What a handler is called with, from root: over a socket on the publisher's
// topics, or over HTTP, which has none.
const ctxOver = (topics: Topics | null): HandlerContext<string> => ({
    principal: 'root',
    clientIp: '',
    locale: '',
    signal: AbortSignal.any([]),
    topics
})

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
    test('every machine has a document of its own, and watching one reads it alone', async () => {
        const { p, board } = watchedBoard()
        const [web, worker] = [controllable(), controllable()]
        web.observed = 'ON'
        make('web', web, board)
        make('worker', worker, board)
        await settled()
        expect(p.doc('web')).toEqual({ status: { state: 'ON', changed_at: expect.any(Number) } })
        expect([web.reads, worker.reads]).toEqual([1, 0])
        expect(p.doc('worker')).toEqual({})
        p.watch('worker')
        await settled()
        expect(p.doc('worker')).toEqual({ status: { state: 'OFF', changed_at: expect.any(Number) } })
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
        expect(p.doc('web')).toEqual({})
        await settled()
        expect(p.doc('web').status?.state).toBe('ON')
    })

    test('starting goes through STARTING and lands ON', async () => {
        const machine = await ready(service, board)
        await machine.start()
        expect(p.doc('web').status?.state).toBe('STARTING')
        service.settle()
        await settled()
        expect(p.seen('web')).toEqual(['OFF', 'STARTING', 'ON'])
    })

    test('stopping goes through STOPPING and lands OFF', async () => {
        service.observed = 'ON'
        const machine = await ready(service, board)
        await machine.stop()
        service.settle()
        await settled()
        expect(p.seen('web')).toEqual(['ON', 'STOPPING', 'OFF'])
    })

    test('a transition that fails lands in ERROR carrying why', async () => {
        const machine = await ready(service, board)
        await machine.start()
        service.settle(new Error('port already bound'))
        await settled()
        expect(p.doc('web').status).toEqual({
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
        expect(p.seen('web')).toEqual(['OFF', 'STARTING', 'ERROR'])
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
        const published = p.seen('web').length
        await machine.refresh()
        expect(p.seen('web')).toHaveLength(published)
        service.observed = 'ON'
        await machine.refresh()
        expect(p.doc('web').status?.state).toBe('ON')
    })

    test('a refresh that cannot read the service is itself an ERROR', async () => {
        const machine = await ready(service, board)
        unreadable(service)
        await machine.refresh()
        expect(p.doc('web').status).toEqual({
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
        expect(p.doc('web').status).toEqual({ state: 'OFF', changed_at: expect.any(Number) })
    })

    test('a transition begun while a read is out goes ahead, and the read is not adopted over it', async () => {
        const machine = await ready(service, board)
        const held = Promise.withResolvers<Exclude<ServiceState, 'ERROR'>>()
        service.check = () => held.promise
        const refreshed = machine.refresh()
        await machine.start()
        held.resolve('OFF')
        await refreshed
        expect(p.doc('web').status?.state).toBe('STARTING')
        service.settle()
        await settled()
        expect(p.seen('web')).toEqual(['OFF', 'STARTING', 'ON'])
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
        expect(p.doc('web').status?.state).toBe('ON')
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
        expect(p.seen('web')).toEqual([])
    })

    test('a reading taken with nobody watching is not published: a refresh, a landing, a failed check', async () => {
        const service = controllable()
        const machine = make('web', service, board)
        await machine.refresh()
        expect(p.doc('web')).toEqual({})
        // The transition under way is the machine's own word, and is published.
        await machine.start()
        expect(p.doc('web').status?.state).toBe('STARTING')
        service.settle()
        await settled()
        expect(p.doc('web')).toEqual({})
        unreadable(service)
        await machine.refresh()
        expect(p.doc('web')).toEqual({})
    })

    test('the first to watch a service is greeted with no reading, and has it read at once', async () => {
        const [web, worker] = [controllable(), controllable()]
        await make('web', web, board).refresh()
        await make('worker', worker, board).refresh()
        p.watch('web')
        expect(p.doc('web')).toEqual({})
        expect([web.reads, worker.reads]).toEqual([2, 1])
        await settled()
        expect(p.doc('web')).toEqual({ status: { state: 'OFF', changed_at: expect.any(Number) } })
        expect(p.doc('worker')).toEqual({})
    })

    test("the last to stop watching takes the reading out, a failed check's ERROR among them, and the machine's own word stays", async () => {
        const [idle, broken, failing, busy] = [controllable(), controllable(), controllable(), controllable()]
        const names = ['idle', 'broken', 'failing', 'busy']
        names.forEach(name => p.watch(name))
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
        expect(names.map(name => p.doc(name).status?.state)).toEqual(['OFF', 'ERROR', 'ERROR', 'STARTING'])
        names.forEach(name => p.unwatch(name))
        expect(names.map(name => p.doc(name).status?.state)).toEqual([undefined, undefined, 'ERROR', 'STARTING'])
        // Begun with somebody watching, landed with nobody: a reading like any
        // other, which nobody saw taken.
        busy.settle()
        await settled()
        expect(p.doc('busy')).toEqual({})
    })

    test("a machine in its own word is not read when its first watcher arrives, and a transition's landing then counts as its reading", async () => {
        const [failing, busy] = [controllable(), controllable()]
        const failingMachine = make('failing', failing, board)
        const busyMachine = make('busy', busy, board)
        await failingMachine.start()
        failing.settle(new Error('port already bound'))
        await busyMachine.start()
        await settled()
        p.watch('failing')
        p.watch('busy')
        await elapse(3 * AGE_MS)
        expect([failing.reads, busy.reads]).toEqual([1, 1])
        busy.settle()
        await settled()
        expect(p.doc('busy').status?.state).toBe('ON')
        await elapse(AGE_MS - 1)
        expect(busy.reads).toBe(1)
        await elapse(1)
        expect(busy.reads).toBe(2)
    })

    test('close stops listening for watchers', async () => {
        const service = controllable()
        make('web', service, board).close()
        p.watch('web')
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
        expect(p.seen('web')).toEqual(['OFF', 'ON'])
    })

    test('every reading schedules the next, and a steady service publishes nothing new', async () => {
        await ready(service, board)
        const published = p.seen('web').length
        await elapse(AGE_MS)
        await elapse(AGE_MS)
        expect(service.reads).toBe(3)
        expect(p.seen('web')).toHaveLength(published)
    })

    test('a state the service was read in is read again like any other, STARTING included', async () => {
        service.observed = 'STARTING'
        await ready(service, board)
        service.observed = 'ON'
        await elapse(AGE_MS)
        expect(p.seen('web')).toEqual(['STARTING', 'ON'])
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
        expect(p.doc('web').status).toEqual({
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
        expect(p.doc('web').status).toEqual({
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
        expect(p.seen('web')).toEqual(['OFF', 'ERROR', 'ON'])
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
        expect(p.doc('web').status?.state).toBe('OFF')
        leftover.resolve('ON')
        await refreshed
        expect(p.doc('web').status?.state).toBe('OFF')
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

    test('the last to stop watching stops the re-reads', async () => {
        await ready(service, board)
        p.unwatch('web')
        await elapse(3 * AGE_MS)
        expect(service.reads).toBe(1)
    })
})

describe('the methods', () => {
    let p: Publisher
    let board: ServiceStateBoard

    beforeEach(() => {
        p = publisher()
        board = serviceStateBoard(p)
    })

    const access = { watch: ROOT_ONLY, command: ROOT_ONLY }

    test("watch and unwatch join and leave the service's topic: it is read while watched, and its document goes to the watcher", async () => {
        const service = controllable()
        make('web', service, board)
        const methods = serviceStateMethods(board, access)
        const web = { service: 'web' }
        expect(await methods['service.watch'].handler(ctxOver(p.topics), web)).toBeUndefined()
        await settled()
        expect(service.reads).toBe(1)
        expect(p.doc('web').status?.state).toBe('OFF')
        await methods['service.unwatch'].handler(ctxOver(p.topics), web)
        expect(p.doc('web')).toEqual({})
    })

    test('watch and unwatch need a WebSocket', async () => {
        make('web', controllable(), board)
        const methods = serviceStateMethods(board, access)
        for (const name of ['service.watch', 'service.unwatch'] as const) {
            await expect(methods[name].handler(ctxOver(null), { service: 'web' })).rejects.toThrow(new ApiError('WEBSOCKET_REQUIRED'))
        }
    })

    test("the commands, under the application's policy, each acting on the service it names", async () => {
        const service = controllable()
        p.watch('web')
        await ready(service, board)
        const methods = serviceStateMethods(board, access)
        expect(Object.keys(methods)).toEqual(['service.watch', 'service.unwatch', 'service.start', 'service.stop', 'service.refresh'])
        for (const def of Object.values(methods)) {
            expect(def.auth).toBe('required')
            expect(def.access?.('someone')).toBe('ADMIN_REQUIRED')
        }

        const web = { service: 'web' }
        expect(await methods['service.start'].handler(ctxOver(null), web)).toBeUndefined()
        expect(p.doc('web').status?.state).toBe('STARTING')
        service.settle()
        await settled()
        await methods['service.stop'].handler(ctxOver(null), web)
        service.settle()
        await settled()
        expect(await methods['service.refresh'].handler(ctxOver(null), web)).toBeUndefined()
        expect(p.seen('web')).toEqual(['OFF', 'STARTING', 'ON', 'STOPPING', 'OFF'])
    })

    test('watching and commanding are each under their own access: a caller command refuses may still watch', async () => {
        make('web', controllable(), board)
        const methods = serviceStateMethods(board, { watch: { auth: 'none' }, command: ROOT_ONLY })
        const dispatcher = createDispatcher(methods, { auth: keyAuthResolver('key', 'root') })
        const someone = { ...ctxOver(p.topics), principal: 'someone' }
        expect(await dispatcher.dispatch({ method: 'service.watch', arguments: [{ service: 'web' }] }, someone)).toMatchObject({ success: true })
        expect(await dispatcher.dispatch({ method: 'service.start', arguments: [{ service: 'web' }] }, someone)).toMatchObject({ success: false, error: 'ADMIN_REQUIRED' })
    })

    test('one board over several services, each call reaching only the one it names', async () => {
        const [web, worker] = [controllable(), controllable()]
        p.watch('web')
        p.watch('worker')
        make('web', web, board)
        make('worker', worker, board)
        await settled()
        await serviceStateMethods(board, access)['service.start'].handler(ctxOver(null), { service: 'worker' })
        expect([p.doc('web').status?.state, p.doc('worker').status?.state]).toEqual(['OFF', 'STARTING'])
        worker.settle()
        await settled()
    })

    test('a command the state cannot take is SERVICE_BUSY on the wire', async () => {
        const service = controllable()
        p.watch('web')
        await ready(service, board)
        const methods = serviceStateMethods(board, access)
        await methods['service.start'].handler(ctxOver(null), { service: 'web' })
        await expect(methods['service.start'].handler(ctxOver(null), { service: 'web' })).rejects.toThrow(new ApiError('SERVICE_BUSY'))
        service.settle()
        await settled()
    })

    test('every method refuses a service the board does not have', async () => {
        make('web', controllable(), board)
        const methods = serviceStateMethods(board, access)
        for (const def of Object.values(methods)) {
            await expect(def.handler(ctxOver(p.topics), { service: 'db' })).rejects.toThrow(new ApiError('SERVICE_NOT_FOUND'))
        }
    })

    test('a call that names no service is refused before any machine is reached', async () => {
        make('web', controllable(), board)
        const dispatcher = createDispatcher(serviceStateMethods(board, access), { auth: keyAuthResolver('key', 'root') })
        expect(await dispatcher.dispatch({ method: 'service.refresh', arguments: [{}] }, ctxOver(null))).toMatchObject({
            success: false,
            error: 'INVALID_ARGUMENT',
            error_details: [{ field: 'service' }]
        })
    })

    test('a read-only console may leave the commands open', () => {
        const methods = serviceStateMethods(board, { watch: { auth: 'none' }, command: { auth: 'none' } })
        expect(methods['service.refresh'].auth).toBe('none')
        expect(methods['service.refresh'].access).toBeUndefined()
    })
})
