// The contract a service-state pair shares: the states, the status both ends
// pass around, the method and document names, and the transition table. Pure
// types and data — no runtime dependency at all, so a frontend importing it
// pulls in nothing. The machine behind it is the `./server` entry.
import type { StateFrame } from '@greendrake/rpc'

export const SERVICE_STATES = ['OFF', 'STARTING', 'ON', 'STOPPING', 'ERROR'] as const

export type ServiceState = (typeof SERVICE_STATES)[number]

// Which service a call is about. Every method takes it, so one socket serves
// any number of services side by side.
export interface ServiceRef {
    service: string
}

// Where a service stands: in ERROR, with what went wrong; in any other state,
// without — a stale message outliving the failure it describes is worse than
// none.
export type ServiceCondition = { state: 'ERROR'; error: string } | { state: Exclude<ServiceState, 'ERROR'> }

// Wire shape: snake_case, as the envelope's own fields are.
export type ServiceStatus = ServiceCondition & {
    // When the service entered this state, epoch milliseconds.
    changed_at: number
}

// Every service's status, by the name its machine was registered under: the
// document a backend publishes. A service has no entry until it has been read
// with somebody watching (see the machine), and a socket that reconnects is
// greeted afresh, so every lookup may find none.
export type ServiceStates = Partial<Record<string, ServiceStatus>>

// Each command: the state it is accepted from, the state the service passes
// through while it runs, and where it lands. The machine refuses anything this
// table does not cover.
export const TRANSITIONS = {
    start: {
        from: 'OFF',
        during: 'STARTING',
        to: 'ON'
    },
    stop: {
        from: 'ON',
        during: 'STOPPING',
        to: 'OFF'
    }
} as const satisfies Record<string, { from: ServiceState; during: ServiceState; to: ServiceState }>

export type ServiceCommand = keyof typeof TRANSITIONS

// The command a state's control offers: each transition from the state it is
// accepted from, a refresh out of ERROR, and nothing while a transition is
// under way — there is no interfering with it.
export const CONTROLS = {
    OFF: 'start',
    STARTING: null,
    ON: 'stop',
    STOPPING: null,
    ERROR: 'refresh'
} as const satisfies Record<ServiceState, ServiceCommand | 'refresh' | null>

// The codes a call is refused with. BUSY covers both a transition already under
// way and a command that would take the service where it already is: a control
// never offers either, so this is what a client that ignored the state gets
// told. NOT_FOUND answers a call naming a service the backend does not serve.
export const SERVICE_BUSY = 'SERVICE_BUSY'
export const SERVICE_NOT_FOUND = 'SERVICE_NOT_FOUND'

export const SERVICE_STATE_DOCUMENT = 'service.state'

// The methods a service-state backend serves. A dashboard hands this to its
// typed client; the server derives its definitions from it. They answer
// nothing: where the service went is the document's to say, on the one ordered
// stream every socket keeps, so no answer can arrive out of turn with it.
export interface ServiceStateMethods {
    'service.start': (ref: ServiceRef) => void
    'service.stop': (ref: ServiceRef) => void
    'service.refresh': (ref: ServiceRef) => void
}

// Every service's status reaches every connected socket as one document, sent
// whole as the socket opens and patched on every change after: a service has
// one state, and everyone watching it is watching the same thing. A data push,
// so a client's transport carries it in its data slot:
// `WsTransport<Record<never, never>, ServiceStatePushes>`.
export type ServiceStatePushes = { [SERVICE_STATE_DOCUMENT]: StateFrame<ServiceStates> }
