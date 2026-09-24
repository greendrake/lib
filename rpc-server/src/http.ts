import type { Server } from 'bun'
import { identityCodec } from '@greendrake/rpc'
import { bearerToken } from './auth'
import type { Dispatcher } from './dispatcher'
import { ANONYMOUS } from './types'
import type { Codec, IncomingRequest, RpcResponse } from './types'

export interface HttpHandlerOptions {
    // Applied to the request body and the response body. Identity by default;
    // a service whose clients obscure or encrypt their payloads supplies the
    // same codec both ends run.
    codec?: Codec
}

// Where the call came from, as the proxies in front of this service report it.
// A service behind nothing still gets the socket's own address.
export const clientIp = <T>(request: Request, server: Server<T>): string => {
    const forwarded = request.headers.get('x-forwarded-for')
    if (forwarded) {
        // The left-most entry is the original client; the rest are the proxies
        // it passed through.
        return forwarded.replace(/,.*/, '').trim()
    }
    return request.headers.get('x-real-ip') ?? server.requestIP(request)?.address ?? ''
}

// The single-endpoint HTTP transport: one POST carries one call. Returns the
// handler a `Bun.serve` fetch routes its API path to.
export const httpHandler = (dispatcher: Dispatcher, options: HttpHandlerOptions = {}): ((request: Request, ip: string) => Promise<Response>) => {
    const codec = options.codec ?? identityCodec
    // A codec's output is not JSON any more, and calling it JSON misleads
    // everything that sniffs the body — devtools, proxies, CDNs.
    const contentType = options.codec ? 'text/plain; charset=utf-8' : 'application/json'
    const respond = (response: RpcResponse): Response => new Response(codec.encode(JSON.stringify(response)), { headers: { 'content-type': contentType } })

    return async (request, ip) => {
        if (request.method !== 'POST') {
            return new Response(null, { status: 405 })
        }
        const token = bearerToken(request)
        // A token nobody recognises leaves the caller anonymous rather than
        // refused: what that costs them is decided per method, by the rule the
        // method declares.
        const auth = (token && (await dispatcher.auth?.resolveToken(token, request.signal))) || ANONYMOUS

        let parsed: IncomingRequest
        try {
            parsed = JSON.parse(codec.decode(await request.text())) as IncomingRequest
        } catch {
            return respond({ success: false, error: 'INVALID_JSON' })
        }
        // No `type` on an HTTP response: the field exists to tell a WebSocket
        // frame's kind apart, and here there is only one kind.
        return respond(
            await dispatcher.dispatch(parsed, {
                auth,
                clientIp: ip,
                signal: request.signal
            })
        )
    }
}
