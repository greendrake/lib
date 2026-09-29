import { ApiError } from '@greendrake/rpc-server'
import type { Audience, AuthRequirement, EventSink, MethodDef } from '@greendrake/rpc-server'
import { SERVICE_STATE_EVENT, TRANSITIONS } from './main'
import type { ServiceCommand, ServiceRef, ServiceState, ServiceStateMethods, ServiceStatus } from './main'

// What a concrete service plugs in. `start` and `stop` resolve once the
// service is up or down; rejecting puts the machine in ERROR with the
// rejection's message. `check` reports what the service is actually doing
// right now, read from the service rather than remembered, and must settle
// promptly: a status or a refresh can wait on it, and the next re-read is
// timed from when it settles, so a check that never does leaves the state as
// it last stood.
export interface ServiceImplementation {
    start(): Promise<void>
    stop(): Promise<void>
    check(): Promise<ServiceState>
}

export interface ServiceStateMachine {
    // What the service is called on the wire: what a call names to reach it,
    // and what each of its pushes carries.
    readonly name: string
    // What the service is: its last reading, or what its last transition left
    // it as — read again first when that is `maxAgeMs` old (see the factory).
    status(): Promise<ServiceStatus>
    // Return as soon as the transition has begun, with the in-flight status.
    // Where it ends up is the machine's next state, pushed to everyone — not
    // this caller's answer, because everyone watching needs it equally and one
    // of them happening to have asked changes nothing.
    start(): ServiceStatus
    stop(): ServiceStatus
    // Re-read the service and adopt what it reports, joining a read already
    // under way — one begun since the last transition — rather than asking
    // the service again.
    refresh(): Promise<ServiceStatus>
    // Stop re-reading the service and listening for watchers, for a process
    // shutting down.
    close(): void
}

// The command a state is in no position to take. One code covers both cases —
// a transition already under way, and a command that would take the service
// where it already is — because the control never offers either, so this is
// what a client that ignored the state gets told.
const BUSY = 'SERVICE_BUSY'

// A call naming a service the table does not serve. A `_NOT_FOUND` code, so
// clients following rpc-server's convention treat it as not found.
const NOT_FOUND = 'SERVICE_NOT_FOUND'

// The service-state machine over a concrete implementation. Reads the service
// before returning, so it never reports a state it has not verified — a
// machine that assumed OFF at boot would have the dashboard show OFF for a
// service that is up. Nor does it later, when anybody needs to know: nothing
// tells the machine when a service is changed by other hands — a unit stopped
// from a shell, a schedule that turns it off overnight — so a reading
// `maxAgeMs` old is taken again before a status is answered from it, and
// while the sink has an audience — a socket open to be pushed to — the
// machine re-reads on that cadence by itself and pushes what changed, so a
// dashboard left open follows the service. With nobody asking and nobody
// watching, the service is left alone. A transition that lands counts as a
// reading. A transition under way and ERROR are the machine's own word rather
// than a reading, and are not read past — ERROR until somebody asks for a
// refresh, because reading past it would take the why away before anyone saw
// it.
export const createServiceStateMachine = async (name: string, service: ServiceImplementation, sink: EventSink & Audience, maxAgeMs: number): Promise<ServiceStateMachine> => {
    let current: ServiceStatus = {
        service: name,
        state: 'OFF',
        error: null,
        changed_at: Date.now()
    }
    // When the service was last read, or a transition last landed — which is
    // what `maxAgeMs` is counted from. The boot read below is the first.
    let readAt = Date.now()
    let transitioning = false
    // Counts the transitions begun. A read that began before the latest one
    // reports what the service was before it, so its answer is dropped rather
    // than adopted over the transition's.
    let transitions = 0
    // The read under way and the transition count it began under. A refresh
    // or re-read asked while it is out joins it rather than asking the service
    // again — unless a transition has begun since, which makes it a reading of
    // the past: then a fresh read goes out, and the old one is dropped when it
    // settles.
    let reading: { promise: Promise<ServiceStatus>; under: number } | undefined
    let nextRead: ReturnType<typeof setTimeout> | undefined
    let closed = false

    const set = (state: ServiceState, error: string | null): ServiceStatus => {
        current = {
            service: name,
            state,
            error,
            changed_at: Date.now()
        }
        sink.broadcastEvent(SERVICE_STATE_EVENT, current)
        return current
    }

    // What a re-read yields: a push only where something actually changed, so
    // a poll against a steady service is silent.
    const adopt = (state: ServiceState, error: string | null): ServiceStatus => (state === current.state && error === current.error ? current : set(state, error))

    // The next re-read, for when the latest reading turns `maxAgeMs` old —
    // straight away if it already has. Only while somebody is watching; none
    // while a transition is under way, whose landing is the next reading; and
    // none in ERROR, which stands until somebody refreshes.
    const schedule = (): void => {
        clearTimeout(nextRead)
        if (!closed && sink.size > 0 && !transitioning && current.state !== 'ERROR') {
            nextRead = setTimeout(() => void read(), Math.max(0, readAt + maxAgeMs - Date.now()))
        }
    }

    // The first watcher starts the re-reads and the last one to leave stops
    // them.
    const stopListening = sink.onPresence(present => {
        if (present) {
            schedule()
        } else {
            clearTimeout(nextRead)
        }
    })

    // A transition landing or a read settling: the service as it now stands,
    // which is a reading — and a push, wherever it moved. A transition lands
    // away from the state it passed through, so it always pushes.
    const reached = (state: ServiceState, error: string | null): ServiceStatus => {
        readAt = Date.now()
        const status = adopt(state, error)
        schedule()
        return status
    }

    const run = (command: ServiceCommand): ServiceStatus => {
        const { from, during, to } = TRANSITIONS[command]
        if (transitioning || current.state !== from) {
            throw new ApiError(BUSY)
        }
        transitioning = true
        transitions += 1
        clearTimeout(nextRead)
        const answer = set(during, null)
        // Over the moment it lands, because its landing is a reading and a
        // reading schedules the next only with no transition under way.
        const landed = (state: ServiceState, error: string | null): void => {
            transitioning = false
            reached(state, error)
        }
        void service[command]().then(
            () => landed(to, null),
            (e: Error) => landed('ERROR', e.message)
        )
        return answer
    }

    const read = (): Promise<ServiceStatus> => {
        if (reading?.under === transitions) {
            return reading.promise
        }
        clearTimeout(nextRead)
        const under = transitions
        // A transition begun since the read went out has the last word — its
        // own landing schedules the next read — and the read's answer, from
        // before it, is dropped.
        const land = (state: ServiceState, error: string | null): ServiceStatus => (under === transitions ? reached(state, error) : current)
        const promise = service
            .check()
            .then(
                state => land(state, null),
                (e: Error) => land('ERROR', e.message)
            )
            .finally(() => {
                if (reading?.promise === promise) {
                    reading = undefined
                }
            })
        reading = { promise, under }
        return promise
    }

    const refresh = async (): Promise<ServiceStatus> => {
        if (transitioning) {
            throw new ApiError(BUSY)
        }
        return read()
    }

    const machine: ServiceStateMachine = {
        name,
        // Answered from the latest reading while it is fresh. A transition under
        // way and ERROR are answered as they stand, being the machine's own word.
        status: () => (transitioning || current.state === 'ERROR' || Date.now() - readAt < maxAgeMs ? Promise.resolve(current) : read()),
        start: () => run('start'),
        stop: () => run('stop'),
        refresh,
        close: () => {
            closed = true
            clearTimeout(nextRead)
            stopListening()
        }
    }
    await machine.refresh()
    return machine
}

// Derived from the client-facing contract rather than restated beside it: a
// method added there has to be defined here before this compiles.
export type ServiceStateMethodDefs = { [K in keyof ServiceStateMethods]: MethodDef<Parameters<ServiceStateMethods[K]>[0], ReturnType<ServiceStateMethods[K]>> }

// The argument every method takes, checked for its shape. Whether it names a
// service the table serves is the table's to say, as SERVICE_NOT_FOUND. Written
// against Standard Schema directly, so this package carries no validator.
const SERVICE_REF: NonNullable<MethodDef<ServiceRef>['args']> = {
    '~standard': {
        version: 1,
        vendor: 'greendrake',
        validate: value => (typeof value === 'object' && value !== null && 'service' in value && typeof value.service === 'string' ? { value: { service: value.service } } : { issues: [{ message: 'Expected the name of a service', path: ['service'] }] })
    }
}

// The four methods over every machine given, ready to merge into a dispatch
// table: one socket serves them all, each call naming its service. Admin-only
// by default: turning a service off is not a read.
export const serviceStateMethods = (machines: readonly ServiceStateMachine[], auth: AuthRequirement = 'admin'): ServiceStateMethodDefs => {
    const byName = new Map(machines.map(machine => [machine.name, machine]))
    if (byName.size !== machines.length) {
        throw new Error(`serviceStateMethods: two machines share a name among ${machines.map(machine => machine.name).join(', ')}`)
    }
    // Each method is the same three steps — the argument checked, the service
    // it names found, one thing asked of that service's machine — and differs
    // only in what is asked.
    const asking = (ask: (machine: ServiceStateMachine) => ServiceStatus | Promise<ServiceStatus>): MethodDef<ServiceRef, ServiceStatus> => ({
        auth,
        args: SERVICE_REF,
        handler: (_ctx, { service }) => {
            const machine = byName.get(service)
            if (!machine) {
                throw new ApiError(NOT_FOUND)
            }
            return ask(machine)
        }
    })
    return {
        'service.status': asking(machine => machine.status()),
        'service.start': asking(machine => machine.start()),
        'service.stop': asking(machine => machine.stop()),
        'service.refresh': asking(machine => machine.refresh())
    }
}

export { SERVICE_STATE_EVENT, SERVICE_STATES, TRANSITIONS } from './main'
export type { ServiceCommand, ServiceRef, ServiceState, ServiceStateMethods, ServiceStatePushes, ServiceStatus } from './main'
