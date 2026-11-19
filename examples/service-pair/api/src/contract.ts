import type { ServiceCommand, ServiceRef, ServiceStateMethods } from '@greendrake/service-state'

// What a client of this API is typed by, and what its dispatch table is typed
// by: the shared service-state contract, and the methods beyond it. A module
// of its own, importing neither side's code, so a client's type-check reads
// this and none of what the handlers import.
export interface ApiMethods extends ServiceStateMethods {
    // Makes the service's next transition fail, once its delay is up or at once:
    // a way to reach ERROR without breaking anything.
    'mock.fail_next': (args: { service: string; command: ServiceCommand; at_once: boolean }) => void
    // How many times the service has been checked.
    'mock.reads': (ref: ServiceRef) => number
}
