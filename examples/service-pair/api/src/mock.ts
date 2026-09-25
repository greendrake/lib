import type { ServiceCommand, ServiceImplementation, ServiceState } from '@greendrake/service-state/server'

// How long the mocked service pretends starting and stopping take. Long enough
// that STARTING and STOPPING are states a person sees, rather than frames.
export const TRANSITION_MS = 3000

// What the service is called on the wire: what a dashboard's control names to
// reach it, and what its pushes carry.
export const MOCK_SERVICE = 'mock'

export interface MockService extends ServiceImplementation {
    // Arm the next `start` or `stop` to fail, so ERROR is reachable without
    // breaking anything. The `mock.fail_next` method exposes it.
    failNext(command: ServiceCommand): void
}

// What a real implementation would wrap — a systemd unit, a container, a
// device. Here: a flag, a delay and a switch that makes the next transition
// fail.
export const createMockService = (transitionMs = TRANSITION_MS): MockService => {
    let running = false
    let failing: ServiceCommand | null = null

    const transition = async (command: ServiceCommand, target: boolean): Promise<void> => {
        await Bun.sleep(transitionMs)
        if (failing === command) {
            failing = null
            throw new Error(`mock service failed to ${command}`)
        }
        running = target
    }

    return {
        start: () => transition('start', true),
        stop: () => transition('stop', false),
        check: (): Promise<ServiceState> => Promise.resolve(running ? 'ON' : 'OFF'),
        failNext: command => {
            failing = command
        }
    }
}
