import type { ServerWebSocket } from 'bun'
import type { RpcRequest } from '../src/main'

export interface WsServerHarness {
    url: string
    // Credentials the server has been asked to bind, in order.
    authCalls: string[]
    // Push a frame to every connected socket.
    broadcast: (payload: Record<string, unknown>) => void
    // Cut every socket without a close handshake, as a lost link does.
    killAllSockets: () => void
    socketCount: () => number
    // Snapshot of the live sockets, for a test that must keep speaking from
    // the ones it captured after the client has disowned them.
    sockets: () => ServerWebSocket<unknown>[]
    stop: () => void
}

// Test WS server: responds to frames by id, records auth calls,
// supports push broadcast and server-side socket kills. One per test file —
// each gets its own port, so files neither share state nor race each other's
// teardown.
export const startWsServer = (): WsServerHarness => {
    const sockets = new Set<ServerWebSocket<unknown>>()
    const authCalls: string[] = []
    const server = Bun.serve({
        port: 0,
        fetch(req, srv) {
            if (srv.upgrade(req)) return
            return new Response('not ws', { status: 400 })
        },
        websocket: {
            open(ws) {
                sockets.add(ws)
            },
            close(ws) {
                sockets.delete(ws)
            },
            message(ws, raw) {
                const frame = JSON.parse(String(raw)) as RpcRequest & { id: string }
                const reply = (payload: Record<string, unknown>): void => {
                    ws.send(JSON.stringify({ id: frame.id, ...payload }))
                }
                switch (frame.method) {
                    case 'auth.token': {
                        // A 'bad-' prefixed credential is refused, so a test can
                        // drive the rejected-bind path.
                        const token = frame.arguments![0] as string
                        authCalls.push(token)
                        reply(token.startsWith('bad-') ? { success: false, error: 'UNAUTHENTICATED' } : { success: true, data: { user: 'u1' } })
                        break
                    }
                    case 'auth.logout':
                    case 'ping':
                        reply({ success: true })
                        break
                    case 'add':
                        reply({ success: true, data: (frame.arguments![0] as number) + (frame.arguments![1] as number) })
                        break
                    case 'hang':
                        // never reply — used to test in-flight rejection on close
                        break
                    default:
                        reply({ success: false, error: 'UNKNOWN_METHOD' })
                }
            }
        }
    })

    return {
        url: `ws://localhost:${server.port}/`,
        authCalls,
        broadcast: payload => sockets.forEach(ws => ws.send(JSON.stringify(payload))),
        killAllSockets: () => sockets.forEach(ws => ws.terminate()),
        socketCount: () => sockets.size,
        sockets: () => [...sockets],
        // Not awaited by callers: with WebSocket close handshakes in flight the
        // stop promise can outlive an afterAll timeout, and the process exits
        // right after anyway.
        stop: () => void server.stop(true)
    }
}

export const nextTick = (): Promise<void> => new Promise(resolve => setTimeout(resolve, 20))
