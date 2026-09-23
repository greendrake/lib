import { ApiError } from '@greendrake/rpc-server'
import type { AuthRequirement, EventSink, MethodDef } from '@greendrake/rpc-server'
import { SERVICE_STATE_EVENT, TRANSITIONS } from './main'
import type { ServiceCommand, ServiceState, ServiceStateMethods, ServiceStatus } from './main'

// What a concrete service plugs in. `start` and `stop` resolve once the
// service is up or down; rejecting puts the machine in ERROR with the
// rejection's message. `check` reports what the service is actually doing
// right now, read from the service rather than remembered.
export interface ServiceImplementation {
    start(): Promise<void>
    stop(): Promise<void>
    check(): Promise<ServiceState>
}

export interface ServiceStateMachine {
    status(): ServiceStatus
    // Return as soon as the transition has begun, with the in-flight status.
    // Where it ends up is the machine's next state, pushed to everyone — not
    // this caller's answer, because everyone watching needs it equally and one
    // of them happening to have asked changes nothing.
    start(): ServiceStatus
    stop(): ServiceStatus
    // Re-read the service and adopt what it reports.
    refresh(): Promise<ServiceStatus>
}

// The command a state is in no position to take. One code covers both cases —
// a transition already under way, and a command that would take the service
// where it already is — because the control never offers either, so this is
// what a client that ignored the state gets told.
const BUSY = 'SERVICE_BUSY'

// The service-state machine over a concrete implementation. Reads the service
// before returning, so it never reports a state it has not verified — a
// machine that assumed OFF at boot would have the dashboard show OFF for a
// service that is up.
export const createServiceStateMachine = async (service: ServiceImplementation, sink: EventSink): Promise<ServiceStateMachine> => {
    let current: ServiceStatus = {
        state: 'OFF',
        error: null,
        changed_at: Date.now()
    }
    let inFlight = false

    const set = (state: ServiceState, error: string | null): ServiceStatus => {
        current = {
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

    const run = (command: ServiceCommand): ServiceStatus => {
        const { from, during, to } = TRANSITIONS[command]
        if (inFlight || current.state !== from) {
            throw new ApiError(BUSY)
        }
        inFlight = true
        const answer = set(during, null)
        void service[command]()
            .then(
                () => set(to, null),
                (e: Error) => set('ERROR', e.message)
            )
            .finally(() => {
                inFlight = false
            })
        return answer
    }

    const refresh = async (): Promise<ServiceStatus> => {
        if (inFlight) {
            throw new ApiError(BUSY)
        }
        inFlight = true
        try {
            return adopt(await service.check(), null)
        } catch (e) {
            return adopt('ERROR', (e as Error).message)
        } finally {
            inFlight = false
        }
    }

    const machine: ServiceStateMachine = {
        status: () => current,
        start: () => run('start'),
        stop: () => run('stop'),
        refresh
    }
    await machine.refresh()
    return machine
}

// Derived from the client-facing contract rather than restated beside it: a
// method added there has to be defined here before this compiles.
export type ServiceStateMethodDefs = { [K in keyof ServiceStateMethods]: MethodDef<void, ReturnType<ServiceStateMethods[K]>> }

// The four methods, ready to merge into a dispatch table. Admin-only by
// default: turning a service off is not a read.
export const serviceStateMethods = (machine: ServiceStateMachine, auth: AuthRequirement = 'admin'): ServiceStateMethodDefs => ({
    'service.status': { auth, handler: () => machine.status() },
    'service.start': { auth, handler: () => machine.start() },
    'service.stop': { auth, handler: () => machine.stop() },
    'service.refresh': { auth, handler: () => machine.refresh() }
})

export { SERVICE_STATE_EVENT, SERVICE_STATES, TRANSITIONS } from './main'
export type { ServiceCommand, ServiceState, ServiceStateMethods, ServiceStatePushes, ServiceStatus } from './main'
