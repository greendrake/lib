import type { ServiceCommand, ServiceImplementation, ServiceState } from '@greendrake/service-state/server'

// How long a mocked service pretends starting and stopping take. Long enough
// that STARTING and STOPPING are states a person sees, rather than frames.
export const TRANSITION_MS = 3000

// The services mocked, by what they are called on the wire: what a dashboard's
// controls name to reach them, and what their entries in the document are
// keyed by.
export const MOCK_SERVICES = ['web', 'worker']

export interface MockService extends ServiceImplementation {
    // Arm the next `start` or `stop` to fail, so ERROR is reachable without
    // breaking anything: once the transition's delay is up, or at once. The
    // `mock.fail_next` method exposes it.
    failNext(command: ServiceCommand, atOnce: boolean): void
}

// What a real implementation would wrap — a systemd unit, a container, a
// device. Here: a flag, a delay and a switch that makes the next transition
// fail.
export const createMockService = (transitionMs = TRANSITION_MS): MockService => {
    let running = false
    let failing: { command: ServiceCommand; atOnce: boolean } | null = null

    const transition = async (command: ServiceCommand, target: boolean): Promise<void> => {
        const armed = failing?.command === command ? failing : null
        if (armed) {
            failing = null
        }
        if (!armed?.atOnce) {
            await Bun.sleep(transitionMs)
        }
        if (armed) {
            throw new Error(`mock service failed to ${command}`)
        }
        running = target
    }

    return {
        start: () => transition('start', true),
        stop: () => transition('stop', false),
        check: (): Promise<Exclude<ServiceState, 'ERROR'>> => Promise.resolve(running ? 'ON' : 'OFF'),
        failNext: (command, atOnce) => {
            failing = { command, atOnce }
        }
    }
}
