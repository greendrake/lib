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
    // false disables CORS entirely — for a service reached only by other
    // services, where no browser is involved.
    cors?: CorsOptions | false
    // Application teardown, run once the listener has stopped accepting:
    // finish in-flight work, close pools, flush.
    drain?: () => Promise<void> | void
}

export interface RunningService<T> {
    readonly server: Server<T>
    // Set from the moment shutdown begins: the probes report it, and an
    // application can read it to stop taking on new work of its own.
    readonly draining: boolean
    // The shutdown sequence, as the signal handlers run it. 'forced' means the
    // deadline elapsed first — the caller decides what that is worth, which is
    // what lets a test drive this without the process exiting.
    stop(): Promise<'clean' | 'forced'>
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

    const stop = async (): Promise<'clean' | 'forced'> => {
        draining = true
        const { deadlineMs, lameDuckMs } = options.shutdown
        let timer: ReturnType<typeof setTimeout> | undefined
        const expiry = new Promise<'forced'>(resolve => {
            timer = setTimeout(() => resolve('forced'), deadlineMs)
        })
        const sequence = async (): Promise<'clean'> => {
            await Bun.sleep(lameDuckMs)
            await server.stop()
            await options.drain?.()
            return 'clean'
        }
        try {
            return await Promise.race([sequence(), expiry])
        } finally {
            clearTimeout(timer)
        }
    }

    const onSignal = (): void => {
        void stop().then(outcome => {
            if (outcome === 'forced') {
                console.error(`${options.name}: shutdown deadline (${options.shutdown.deadlineMs}ms) exceeded, forcing exit`)
                process.exit(1)
            }
            console.log(`${options.name} stopped`)
            process.exit(0)
        })
    }
    process.on('SIGINT', onSignal)
    process.on('SIGTERM', onSignal)

    return {
        server,
        get draining() {
            return draining
        },
        stop
    }
}
