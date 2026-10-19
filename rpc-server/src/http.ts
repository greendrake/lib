import type { Server } from 'bun'
import { identityCodec } from '@greendrake/rpc'
import { bearerToken } from './auth'
import { failure, type Dispatcher } from './dispatcher'
import type { Codec, IncomingRequest, RpcResponse } from './types'

export interface HttpHandlerOptions {
    // Applied to the request body and the response body. Identity by default;
    // a service whose clients obscure or encrypt their payloads supplies the
    // same codec both ends run.
    codec?: Codec
    // The credential a request carries: its bearer token unless given.
    credential?: (request: Request) => string
}

// The address of the socket a request arrived on. Null from Bun for one that
// came over no network socket (a unix one), which has no address to give.
export const peerAddress = <T>(request: Request, server: Server<T>): string => server.requestIP(request)?.address ?? ''

// The single-endpoint HTTP transport: one POST carries one call. Returns the
// handler a `Bun.serve` fetch routes its API path to.
export const httpHandler = <P>(dispatcher: Dispatcher<P>, options: HttpHandlerOptions = {}): (<T>(request: Request, server: Server<T>) => Promise<Response>) => {
    const codec = options.codec ?? identityCodec
    const credential = options.credential ?? bearerToken
    // A codec's output is not JSON any more, and calling it JSON misleads
    // everything that sniffs the body — devtools, proxies, CDNs.
    const contentType = options.codec ? 'text/plain; charset=utf-8' : 'application/json'
    const respond = (response: RpcResponse): Response => new Response(codec.encode(JSON.stringify(response)), { headers: { 'content-type': contentType } })

    return async (request, server) => {
        if (request.method !== 'POST') {
            return new Response(null, { status: 405 })
        }
        const token = credential(request)
        // A token nobody recognises leaves the caller anonymous rather than
        // refused: what that costs them is decided per method, by the rule the
        // method declares.
        const principal = token && dispatcher.auth ? await dispatcher.auth.resolveToken(token, request.signal) : null

        let parsed: IncomingRequest
        try {
            parsed = JSON.parse(codec.decode(await request.text())) as IncomingRequest
        } catch {
            return respond(failure('INVALID_JSON'))
        }
        // No `type` on an HTTP response: the field exists to tell a WebSocket
        // frame's kind apart, and here there is only one kind.
        return respond(
            await dispatcher.dispatch(parsed, {
                principal,
                clientIp: dispatcher.clientIp(request, peerAddress(request, server)),
                signal: request.signal,
                // One request, one response, and no connection left to push to.
                topics: null
            })
        )
    }
}
