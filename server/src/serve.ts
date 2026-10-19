import type { Server, WebSocketHandler } from 'bun'
import { withCors, type CorsOptions } from './cors'
import { healthz, readyz, type DependencyCheck } from './probes'

// Bun's request handler, including the `undefined` a handler returns once it
// has upgraded the request to a WebSocket.
export type FetchHandler<T = undefined> = (request: Request, server: Server<T>) => Response | undefined | Promise<Response | undefined>

export interface ShutdownOptions {
    // The whole sequence's budget. Past it the process exits non-zero: task
    // brokers and HTTP clients recover from that (re-delivery, retry), whereas
    // a binary that will not die has to be killed by something else.
    deadlineMs: number
    // How long to keep serving after the probes start reporting unready, so
    // whatever routes traffic notices before the sockets stop accepting. Zero
    // for anything nothing routes to.
    lameDuckMs: number
}

export interface ServeOptions<T> {
    // Appears in the startup and forced-exit log lines, so a host running
    // several binaries says which one.
    name: string
    port: number
    // The application's routes. Probes and CORS are answered before it.
    fetch: FetchHandler<T>
    websocket?: WebSocketHandler<T>
    checks?: DependencyCheck[]
    probeTimeoutMs: number
    shutdown: ShutdownOptions
    // Which browser pages may call the service; false for one no browser
    // reaches, which answers none of them.
    cors: CorsOptions | false
    // Application teardown, run once the listener has stopped accepting:
    // finish in-flight work, close pools, flush.
    drain?: () => Promise<void> | void
}

export interface RunningService<T> {
    readonly server: Server<T>
    // Set from the moment shutdown begins: the probes report it, and an
    // application can read it to stop taking on new work of its own.
    readonly draining: boolean
    // The shutdown sequence, as the signal handlers run it; calling it again
    // joins the one under way. 'forced' means the deadline elapsed first — the
    // caller decides what that is worth, which is what lets a test drive this
    // without the process exiting.
    stop(): Promise<'clean' | 'forced'>
}

interface Stoppable {
    name: string
    deadlineMs: number
    stop(): Promise<'clean' | 'forced'>
}

// The services this process runs that have not finished stopping.
const live = new Set<Stoppable>()

// SIGINT/SIGTERM stop every live service at once, then exit: non-zero when any
// was cut off at its deadline. One pair of handlers for the process, there
// while any service is: a process running several, or a test suite starting
// one per test, does not pile them up.
const shutDown = (): void => {
    void Promise.all(
        [...live].map(async service => {
            const outcome = await service.stop()
            if (outcome === 'forced') {
                console.error(`${service.name}: shutdown deadline (${service.deadlineMs}ms) exceeded, forcing exit`)
            } else {
                console.log(`${service.name} stopped`)
            }
            return outcome
        })
    ).then(outcomes => process.exit(outcomes.includes('forced') ? 1 : 0))
}

const SIGNALS = ['SIGINT', 'SIGTERM'] as const

const track = (service: Stoppable): void => {
    if (live.size === 0) {
        SIGNALS.forEach(signal => process.on(signal, shutDown))
    }
    live.add(service)
}

const untrack = (service: Stoppable): void => {
    live.delete(service)
    if (live.size === 0) {
        SIGNALS.forEach(signal => process.off(signal, shutDown))
    }
}

// Runs a service binary: bind the port, answer the probes, and shut down on
// SIGINT/SIGTERM the same way every time — unready first, then stop accepting,
// then drain, all inside one deadline.
export const serve = <T>(options: ServeOptions<T>): RunningService<T> => {
    let draining = false
    const app: FetchHandler<T> = (request, server) => {
        const { pathname } = new URL(request.url)
        if (pathname === '/healthz') {
            return healthz(draining)
        }
        if (pathname === '/readyz') {
            return readyz(options.checks ?? [], options.probeTimeoutMs, draining)
        }
        return options.fetch(request, server)
    }
    const fetch = options.cors === false ? app : withCors(app, options.cors)
    // Bun's options are two distinct shapes, and an explicit
    // `websocket: undefined` fits neither. The shapes differ in their fetch
    // signature too: only the WebSocket one allows a handler to answer
    // nothing, which is how a successful upgrade is reported.
    const server = options.websocket
        ? Bun.serve<T, never>({
              port: options.port,
              fetch,
              websocket: options.websocket
          })
        : Bun.serve<T, never>({
              port: options.port,
              fetch: async (request, srv) => {
                  const response = await fetch(request, srv)
                  if (!response) {
                      throw new Error(`${options.name}: no response for ${request.url}, and no WebSocket handler configured that could have upgraded it`)
                  }
                  return response
              }
          })
    console.log(`${options.name} listening on ${server.url.href}`)

    const { deadlineMs, lameDuckMs } = options.shutdown
    let stopping: Promise<'clean' | 'forced'> | undefined
    const sequence = async (): Promise<'clean' | 'forced'> => {
        draining = true
        let timer: ReturnType<typeof setTimeout> | undefined
        const expiry = new Promise<'forced'>(resolve => {
            timer = setTimeout(() => resolve('forced'), deadlineMs)
        })
        const steps = async (): Promise<'clean'> => {
            await Bun.sleep(lameDuckMs)
            await server.stop()
            await options.drain?.()
            return 'clean'
        }
        try {
            return await Promise.race([steps(), expiry])
        } finally {
            clearTimeout(timer)
            untrack(stoppable)
        }
    }
    const stoppable: Stoppable = {
        name: options.name,
        deadlineMs,
        stop: () => (stopping ??= sequence())
    }
    track(stoppable)

    return {
        server,
        get draining() {
            return draining
        },
        stop: stoppable.stop
    }
}
