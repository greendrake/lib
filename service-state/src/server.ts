import { ApiError, topicsOf } from '@greendrake/rpc-server'
import type { Audience, Document, DocumentPublisher, HandlerContext, MethodAccess, MethodDef } from '@greendrake/rpc-server'
import { SERVICE_BUSY, SERVICE_NOT_FOUND, TRANSITIONS, serviceStateDocument } from './main'
import type { ServiceCommand, ServiceCondition, ServiceDocument, ServiceRef, ServiceState, ServiceStateMethods, ServiceStatus } from './main'

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
    // and what its document and topic are named for (serviceStateDocument).
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

// The machines of a backend, the one index of them by name, and each one's
// document on a publisher.
export interface ServiceStateBoard {
    // The machine registered under a name.
    machine(name: string): ServiceStateMachine | undefined
    // For createServiceStateMachine: registers a machine, refusing a second of
    // its name, and keeps its document — what `produce` gives on each refresh —
    // on the sockets in its own topic, both named serviceStateDocument(name).
    // The handle is the machine's audience too: the sockets watching it.
    register(machine: ServiceStateMachine, produce: () => ServiceDocument): Document & Audience
}

// The board over a publisher — @greendrake/rpc-server's ConnRegistry is one.
export const serviceStateBoard = (publisher: DocumentPublisher): ServiceStateBoard => {
    const machines = new Map<string, ServiceStateMachine>()
    return {
        machine: name => machines.get(name),
        register: (machine, produce) => {
            if (machines.has(machine.name)) {
                throw new Error(`serviceStateBoard: two machines are named ${machine.name}`)
            }
            machines.set(machine.name, machine)
            const type = serviceStateDocument(machine.name)
            return publisher.document(type, produce, { topic: type })
        }
    }
}

// The service-state machine over a concrete implementation, registered on the
// board as `name`, its document kept by the sockets watching it. Nothing tells
// the machine when a service is changed by other hands — a unit stopped from a
// shell, a schedule that turns it off overnight — so it reads the service when
// somebody needs to know, and only then:
//
// - Watched. While anybody watches the service — a socket in its topic — the
//   machine re-reads whenever its latest reading turns `maxAgeMs` old, so a
//   dashboard left open follows the service. The first to watch has the
//   service read at once where the document holds no reading, and so does a
//   machine created while somebody watches. Watching another service on the
//   board reads nothing of this one.
// - Commanded. A start or stop on a machine whose latest reading is missing or
//   `maxAgeMs` old reads first, and is refused or taken as that reading says.
//
// The document holds the latest reading while anybody watches the service,
// and once nobody does, until the reading turns `maxAgeMs` old: so the first
// to watch is greeted with a reading younger than `maxAgeMs`, the next re-read
// timed from when it was taken — or with none, and the service read at once.
// A reading taken with nobody watching, by a command or a refresh, is kept the
// same way. A transition under way, and the ERROR a failed one lands in, are the
// machine's own word rather than a reading: published whatever the audience,
// and not read past — the failed transition's ERROR until somebody refreshes,
// because reading past it would take the why away before anyone saw it. The
// ERROR of a failed check is a reading like any other. A transition that lands
// counts as a reading.
export const createServiceStateMachine = (name: string, service: ServiceImplementation, board: ServiceStateBoard, maxAgeMs: number): ServiceStateMachine => {
    // The service as the machine last knew it. Assumed OFF until the first
    // reading, which is neither published nor acted on: `kept` is false and
    // `readAt` names no time, so a command reads first.
    let current: ServiceStatus = { state: 'OFF', changed_at: Date.now() }
    // When the latest reading landed, or a transition last did — which is what
    // `maxAgeMs` is counted from. Never, before the first.
    let readAt = Number.NEGATIVE_INFINITY
    // Whether the latest reading is in the document: from its landing until it
    // turns `maxAgeMs` old with nobody watching.
    let kept = false
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
    // For when the latest reading turns `maxAgeMs` old.
    let ageing: ReturnType<typeof setTimeout> | undefined
    let closed = false

    // A change of condition, stamped; the same state for the same reason keeps
    // its stamp, so a poll against a steady service publishes nothing.
    const become = (next: ServiceCondition): void => {
        const same = next.state === current.state && (next.state !== 'ERROR' || (current.state === 'ERROR' && next.error === current.error))
        if (!same) {
            current = { ...next, changed_at: Date.now() }
        }
    }

    // The latest reading turning `maxAgeMs` old, which is one moment whoever
    // watches; what it calls for is decided as it comes. With somebody watching,
    // the service is re-read — joining a read already out — but not while a
    // transition is under way, whose landing is the next reading, nor in a
    // failed transition's ERROR, which stands until somebody refreshes. With
    // nobody, the reading leaves the document.
    const aged = (): void => {
        if (topic.size > 0) {
            if (!transitioning && !failed) {
                void read()
            }
        } else if (kept) {
            kept = false
            topic.refresh()
        }
    }

    // Times `aged` for the latest reading, on every reading and every change of
    // audience. A reading already past its age is acted on now rather than on
    // a timer's tick, before which a join would be greeted with it.
    const age = (): void => {
        clearTimeout(ageing)
        if (closed) {
            return
        }
        const left = readAt + maxAgeMs - Date.now()
        if (left > 0) {
            ageing = setTimeout(aged, left)
        } else {
            aged()
        }
    }

    // A check settling or a transition landing: the service as it now stands,
    // which is a reading.
    const reached = (next: ServiceCondition): void => {
        readAt = Date.now()
        kept = true
        become(next)
        topic.refresh()
        age()
    }

    const read = (): Promise<void> => {
        if (reading?.under === transitions) {
            return reading.promise
        }
        const under = transitions
        // A transition begun since the read went out has the last word — its
        // own landing is the next reading — and the read's answer, from before
        // it, is dropped.
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
        become({ state: during })
        topic.refresh()
        // Out of the transition before its landing counts as a reading, so the
        // reading is aged in the state the landing leaves.
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
            clearTimeout(ageing)
            stopListening()
        }
    }

    // The machine's topic: its document, and the sockets watching it.
    const topic = board.register(machine, () => (transitioning || failed || kept ? { status: current } : {}))

    // Each change of audience ages the reading afresh. The first watcher has
    // the service read at once where no reading is kept — one past its age, or
    // none — and otherwise the re-read comes as the kept one turns `maxAgeMs`
    // old. The last to stop watching takes a reading past its age out at once,
    // and leaves a younger one in until it ages.
    const stopListening = topic.onPresence(age)

    // A machine created while somebody watches has no reading, and reads.
    age()
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

// Who may call the methods, in the application's own principals. Seeing a
// service is a read and commanding it is not, so each has its own: `watch`
// covers watching and unwatching, `command` starting, stopping and refreshing.
// A host that lets every socket see the state, whoever it is bound as, opens
// `watch` and gates `command`.
export interface ServiceStateAccess<P> {
    watch: MethodAccess<P>
    command: MethodAccess<P>
}

// The five methods over every machine on the board, ready to merge into a
// dispatch table: one socket serves them all, each call naming its service.
export const serviceStateMethods = <P>(board: ServiceStateBoard, access: ServiceStateAccess<P>): ServiceStateMethodDefs<P> => {
    // Each method is the same three steps — the argument checked, the service
    // it names found, one thing asked of that service's machine — and differs
    // only in what is asked, and under which access.
    const asking = (allowed: MethodAccess<P>, ask: (machine: ServiceStateMachine, ctx: HandlerContext<P>) => Promise<void> | void): MethodDef<ServiceRef, void, P> => ({
        ...allowed,
        args: SERVICE_REF,
        handler: async (ctx, { service }) => {
            const machine = board.machine(service)
            if (!machine) {
                throw new ApiError(SERVICE_NOT_FOUND)
            }
            try {
                await ask(machine, ctx)
            } catch (e) {
                throw e instanceof ServiceBusyError ? new ApiError(SERVICE_BUSY) : e
            }
        }
    })
    return {
        'service.watch': asking(access.watch, (machine, ctx) => topicsOf(ctx).join(serviceStateDocument(machine.name))),
        'service.unwatch': asking(access.watch, (machine, ctx) => topicsOf(ctx).leave(serviceStateDocument(machine.name))),
        'service.start': asking(access.command, machine => machine.start()),
        'service.stop': asking(access.command, machine => machine.stop()),
        'service.refresh': asking(access.command, machine => machine.refresh())
    }
}

export { CONTROLS, SERVICE_BUSY, SERVICE_NOT_FOUND, SERVICE_STATES, TRANSITIONS, serviceStateDocument } from './main'
export type { ServiceCommand, ServiceCondition, ServiceDocument, ServiceRef, ServiceState, ServiceStateDocumentType, ServiceStateMethods, ServiceStatePushes, ServiceStatus } from './main'
