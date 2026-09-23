// The contract a service-state pair shares: the states, the status both ends
// pass around, the method and push names, and the transition table. Pure types
// and data — no runtime dependency at all, so a frontend importing it pulls in
// nothing. The machine behind it is the `./server` entry.

export const SERVICE_STATES = ['OFF', 'STARTING', 'ON', 'STOPPING', 'ERROR'] as const

export type ServiceState = (typeof SERVICE_STATES)[number]

// Wire shape: snake_case, as the envelope's own fields are.
export interface ServiceStatus {
    state: ServiceState
    // What went wrong, in ERROR. Null in every other state — a stale message
    // outliving the failure it describes is worse than none.
    error: string | null
    // When the service entered this state, epoch milliseconds.
    changed_at: number
}

// Each command: the state it is accepted from, the state the service passes
// through while it runs, and where it lands. The machine refuses anything this
// table does not cover, and the control derives its button from it — so adding
// a command is one row, not an edit in two packages.
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

export const SERVICE_STATE_EVENT = 'service.state'

// The methods a service-state backend serves. A dashboard hands this to its
// ApiClient; the server derives its definitions from it.
export interface ServiceStateMethods {
    'service.status': () => ServiceStatus
    'service.start': () => ServiceStatus
    'service.stop': () => ServiceStatus
    'service.refresh': () => ServiceStatus
}

// Every state change reaches every connected socket: a service has one state,
// and everyone watching it is watching the same thing.
export type ServiceStatePushes = { [SERVICE_STATE_EVENT]: ServiceStatus }
