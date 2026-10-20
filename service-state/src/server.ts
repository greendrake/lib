import { ApiError } from '@greendrake/rpc-server'
import type { Audience, DocumentPublisher, MethodAccess, MethodDef } from '@greendrake/rpc-server'
import { SERVICE_BUSY, SERVICE_NOT_FOUND, SERVICE_STATE_DOCUMENT, TRANSITIONS } from './main'
import type { ServiceCommand, ServiceCondition, ServiceRef, ServiceState, ServiceStateMethods, ServiceStates, ServiceStatus } from './main'

// What the machine throws for a command its state is in no position to take:
// a transition already under way, or one that would take the service where it
// already is. serviceStateMethods answers it SERVICE_BUSY.
export class ServiceBusyError extends Error {
    constructor(command: ServiceCommand | 'refresh', state: ServiceState) {
        super(`cannot ${command} a service ${state}`)
        this.name = 'ServiceBusyError'
    }
}

// What a concrete service plugs in. `start` and `stop` resolve once the
// service is up or down; rejecting puts the machine in ERROR with the
// rejection's message. `check` reports what the service is actually doing
// right now, read from the service rather than remembered: a service found
// failed is reported by rejecting with why, which the machine reads as ERROR.
// It must settle promptly: a command or a refresh can wait on it, and the next
// re-read is timed from when it settles, so a check that never does leaves the
// state as it last stood.
export interface ServiceImplementation {
    start(): Promise<void>
    stop(): Promise<void>
    check(): Promise<Exclude<ServiceState, 'ERROR'>>
}

export interface ServiceStateMachine {
    // What the service is called on the wire: what a call names to reach it,
    // and what its entry in the document is keyed by.
    readonly name: string
    // Resolve as soon as the transition has begun, or reject a ServiceBusyError.
    // Where it ends up is the document's to say, to everyone — not this caller's
    // answer, because everyone watching needs it equally and one of them
    // happening to have asked changes nothing.
    start(): Promise<void>
    stop(): Promise<void>
    // Re-read the service and adopt what it reports, joining a read already
    // under way — one begun since the last transition — rather than asking
    // the service again. Resolves once the reading has landed.
    refresh(): Promise<void>
    // Stop re-reading the service and listening for watchers, for a process
    // shutting down.
    close(): void
}

// The machines of a backend, published as one document, and the one index of
// them by name. Presence passes through from the publisher: whether anybody is
// there to watch is what every machine's reading turns on.
export interface ServiceStateBoard extends Audience {
    // The machine registered under a name.
    machine(name: string): ServiceStateMachine | undefined
    // For createServiceStateMachine: registers a machine, refusing a second of
    // its name. Its entry in the document is whatever `entry` gives as each
    // publish runs, none where it gives undefined.
    add(machine: ServiceStateMachine, entry: () => ServiceStatus | undefined): void
    // For createServiceStateMachine: publishes every machine's entry as it now
    // stands. What changed goes to every socket; nothing goes when nothing did.
    publish(): void
}

// The board over a publisher — @greendrake/rpc-server's ConnRegistry is one —
// which keeps its document on every socket as SERVICE_STATE_DOCUMENT.
export const serviceStateBoard = (publisher: DocumentPublisher & Audience): ServiceStateBoard => {
    const registered = new Map<string, { machine: ServiceStateMachine; entry: () => ServiceStatus | undefined }>()
    const document = publisher.document(SERVICE_STATE_DOCUMENT, (): ServiceStates => {
        const states: ServiceStates = {}
        for (const [name, { entry }] of registered) {
            const status = entry()
            if (status) {
                states[name] = status
            }
        }
        return states
    })
    return {
        get size() {
            return publisher.size
        },
        onPresence: listener => publisher.onPresence(listener),
        machine: name => registered.get(name)?.machine,
        add: (machine, entry) => {
            if (registered.has(machine.name)) {
                throw new Error(`serviceStateBoard: two machines are named ${machine.name}`)
            }
            registered.set(machine.name, { machine, entry })
            document.refresh()
        },
        publish: () => document.refresh()
    }
}

// The service-state machine over a concrete implementation, published on the
// board as `name`. Nothing tells the machine when a service is changed by
// other hands — a unit stopped from a shell, a schedule that turns it off
// overnight — so it reads the service when somebody needs to know, and only
// then:
//
// - Watched. While the board's audience has anybody in it — a socket open to
//   be told — the machine re-reads whenever its latest reading turns
//   `maxAgeMs` old, so a dashboard left open follows the service. The first to
//   arrive has the service read at once, and so does a machine created while
//   somebody watches.
// - Commanded. A start or stop on a machine whose latest reading is missing or
//   `maxAgeMs` old reads first, and is refused or taken as that reading says.
//
// A reading is published only when somebody watched it being taken, and the
// last to leave takes every reading out of the document: so an opening
// dashboard is shown the service as it is, once it has been read, and never as
// it was. A transition under way, and the ERROR a failed one lands in, are the
// machine's own word rather than a reading: published whatever the audience,
// and not read past — the failed transition's ERROR until somebody refreshes,
// because reading past it would take the why away before anyone saw it. The
// ERROR of a failed check is a reading like any other. A transition that lands
// counts as a reading.
export const createServiceStateMachine = (name: string, service: ServiceImplementation, board: ServiceStateBoard, maxAgeMs: number): ServiceStateMachine => {
    // The service as the machine last knew it. Assumed OFF until the first
    // reading, which is neither published nor acted on: `witnessed` is false and
    // `readAt` names no time, so a command reads first.
    let current: ServiceStatus = { state: 'OFF', changed_at: Date.now() }
    // When the latest reading landed, or a transition last did — which is what
    // `maxAgeMs` is counted from. Never, before the first.
    let readAt = Number.NEGATIVE_INFINITY
    // Whether somebody was watching as the latest reading landed: only then is
    // it published.
    let witnessed = false
    // The machine's own word: a transition under way, and the ERROR a failed
    // one landed in.
    let transitioning = false
    let failed = false
    // Counts the transitions begun. A read that began before the latest one
    // reports what the service was before it, so its answer is dropped rather
    // than adopted over the transition's.
    let transitions = 0
    // The read under way and the transition count it began under. A refresh,
    // re-read or command asked while it is out joins it rather than asking the
    // service again — unless a transition has begun since, which makes it a
    // reading of the past: then a fresh read goes out, and the old one is
    // dropped when it settles.
    let reading: { promise: Promise<void>; under: number } | undefined
    let nextRead: ReturnType<typeof setTimeout> | undefined
    let closed = false

    // A change of condition, stamped; the same state for the same reason keeps
    // its stamp, so a poll against a steady service publishes nothing.
    const become = (next: ServiceCondition): void => {
        const same = next.state === current.state && (next.state !== 'ERROR' || (current.state === 'ERROR' && next.error === current.error))
        if (!same) {
            current = { ...next, changed_at: Date.now() }
        }
    }

    // The next re-read, for when the latest reading turns `maxAgeMs` old —
    // straight away if it already has. Only while somebody is watching; none
    // while a transition is under way, whose landing is the next reading; and
    // none in a failed transition's ERROR, which stands until somebody
    // refreshes.
    const schedule = (): void => {
        clearTimeout(nextRead)
        if (!closed && board.size > 0 && !transitioning && !failed) {
            nextRead = setTimeout(() => void read(), Math.max(0, readAt + maxAgeMs - Date.now()))
        }
    }

    // A check settling or a transition landing: the service as it now stands,
    // which is a reading.
    const reached = (next: ServiceCondition): void => {
        readAt = Date.now()
        witnessed = board.size > 0
        become(next)
        board.publish()
        schedule()
    }

    const read = (): Promise<void> => {
        if (reading?.under === transitions) {
            return reading.promise
        }
        clearTimeout(nextRead)
        const under = transitions
        // A transition begun since the read went out has the last word — its
        // own landing schedules the next read — and the read's answer, from
        // before it, is dropped.
        const land = (next: ServiceCondition): void => {
            if (under === transitions) {
                failed = false
                reached(next)
            }
        }
        const promise = service
            .check()
            .then(
                state => land({ state }),
                (e: Error) => land({ state: 'ERROR', error: e.message })
            )
            .finally(() => {
                if (reading?.promise === promise) {
                    reading = undefined
                }
            })
        reading = { promise, under }
        return promise
    }

    const run = async (command: ServiceCommand): Promise<void> => {
        // The state acted on is verified: the machine's own word, or a reading
        // younger than `maxAgeMs` — taken now where there is none.
        if (!transitioning && !failed && Date.now() - readAt >= maxAgeMs) {
            await read()
        }
        const { from, during, to } = TRANSITIONS[command]
        // A transition under way and a failed one's ERROR are never a `from`.
        if (current.state !== from) {
            throw new ServiceBusyError(command, current.state)
        }
        transitioning = true
        transitions += 1
        clearTimeout(nextRead)
        become({ state: during })
        board.publish()
        // Over the moment it lands, because its landing is a reading and a
        // reading schedules the next only with no transition under way.
        const landed = (next: ServiceCondition, failure: boolean): void => {
            transitioning = false
            failed = failure
            reached(next)
        }
        void service[command]().then(
            () => landed({ state: to }, false),
            (e: Error) => landed({ state: 'ERROR', error: e.message }, true)
        )
    }

    const machine: ServiceStateMachine = {
        name,
        start: () => run('start'),
        stop: () => run('stop'),
        refresh: async () => {
            if (transitioning) {
                throw new ServiceBusyError('refresh', current.state)
            }
            await read()
        },
        close: () => {
            closed = true
            clearTimeout(nextRead)
            stopListening()
        }
    }

    board.add(machine, () => (transitioning || failed || witnessed ? current : undefined))

    // The first watcher has the service read at once: every reading went out
    // of the document with the last one to leave. The machine's own word stays,
    // and is not read past. The last to leave stops the re-reads.
    const stopListening = board.onPresence(present => {
        if (present) {
            if (!transitioning && !failed) {
                void read()
            }
            return
        }
        clearTimeout(nextRead)
        witnessed = false
        board.publish()
    })

    if (board.size > 0) {
        void read()
    }
    return machine
}

// Derived from the client-facing contract rather than restated beside it: a
// method added there has to be defined here before this compiles.
export type ServiceStateMethodDefs<P> = { [K in keyof ServiceStateMethods]: MethodDef<Parameters<ServiceStateMethods[K]>[0], ReturnType<ServiceStateMethods[K]>, P> }

// The argument every method takes, checked for its shape. Whether it names a
// service the board has is the board's to say, as SERVICE_NOT_FOUND. Written
// against Standard Schema directly, so this package carries no validator.
const SERVICE_REF: NonNullable<MethodDef<ServiceRef>['args']> = {
    '~standard': {
        version: 1,
        vendor: 'greendrake',
        validate: value => (typeof value === 'object' && value !== null && 'service' in value && typeof value.service === 'string' ? { value: { service: value.service } } : { issues: [{ message: 'Expected the name of a service', path: ['service'] }] })
    }
}

// The three methods over every machine on the board, ready to merge into a
// dispatch table: one socket serves them all, each call naming its service.
// Who may call them is the application's to say — turning a service off is
// not a read, and only the application knows its principals.
export const serviceStateMethods = <P>(board: ServiceStateBoard, access: MethodAccess<P>): ServiceStateMethodDefs<P> => {
    // Each method is the same three steps — the argument checked, the service
    // it names found, one thing asked of that service's machine — and differs
    // only in what is asked.
    const asking = (ask: (machine: ServiceStateMachine) => Promise<void>): MethodDef<ServiceRef, void, P> => ({
        ...access,
        args: SERVICE_REF,
        handler: async (_ctx, { service }) => {
            const machine = board.machine(service)
            if (!machine) {
                throw new ApiError(SERVICE_NOT_FOUND)
            }
            try {
                await ask(machine)
            } catch (e) {
                throw e instanceof ServiceBusyError ? new ApiError(SERVICE_BUSY) : e
            }
        }
    })
    return {
        'service.start': asking(machine => machine.start()),
        'service.stop': asking(machine => machine.stop()),
        'service.refresh': asking(machine => machine.refresh())
    }
}

export { CONTROLS, SERVICE_BUSY, SERVICE_NOT_FOUND, SERVICE_STATE_DOCUMENT, SERVICE_STATES, TRANSITIONS } from './main'
export type { ServiceCommand, ServiceCondition, ServiceRef, ServiceState, ServiceStateMethods, ServiceStatePushes, ServiceStates, ServiceStatus } from './main'
