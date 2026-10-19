import { admitsOrigin, type OriginPolicy } from '@greendrake/util'
import type { FetchHandler } from './serve'

// What a browser may send. The list is the union of what a JSON API over fetch
// needs; a service whose clients send more names them in `allowHeaders`.
const REQUEST_HEADERS = ['Accept', 'Authorization', 'Cache-Control', 'Content-Type', 'Origin', 'X-Requested-With']

const METHODS = ['GET', 'POST', 'PUT', 'DELETE', 'OPTIONS']

export interface CorsOptions {
    // The origins whose pages may read the service's responses. A request from
    // one is answered with its own origin ('*' answers '*'); one from any other
    // gets no CORS headers, and its browser keeps the response from the page.
    origin: OriginPolicy
    // Lets a page send its cookies and HTTP authentication. Offered to any
    // origin it would hand every site the user visits their session, so with
    // '*' it is refused as the service is built.
    credentials?: boolean
    // Added to the defaults above.
    allowHeaders?: string[]
    // Response headers a browser client may read. Everything else is hidden
    // from it whatever the server sends.
    exposeHeaders?: string[]
}

// CORS at the outermost layer, so a preflight is answered before it reaches
// the application — and before any middleware hung off it that would charge a
// rate limit or a captcha for a request the browser made on its own.
export const withCors = <T>(fetch: FetchHandler<T>, options: CorsOptions): FetchHandler<T> => {
    if (options.origin === '*' && options.credentials) {
        throw new Error('withCors: credentials offered to any origin')
    }
    const policy: Record<string, string> = {
        'Access-Control-Allow-Headers': [...REQUEST_HEADERS, ...(options.allowHeaders ?? [])].join(', '),
        'Access-Control-Allow-Methods': METHODS.join(', ')
    }
    if (options.credentials) {
        policy['Access-Control-Allow-Credentials'] = 'true'
    }
    if (options.exposeHeaders?.length) {
        policy['Access-Control-Expose-Headers'] = options.exposeHeaders.join(', ')
    }

    // The policy, for a request from an origin it admits.
    const corsHeaders = (request: Request): Record<string, string> => {
        const origin = request.headers.get('origin')
        if (origin === null || !admitsOrigin(options.origin, origin)) {
            return {}
        }
        return { 'Access-Control-Allow-Origin': options.origin === '*' ? '*' : origin, ...policy }
    }

    // Every response varies by Origin, so no cache serves one origin's answer
    // to another.
    return async (request, server) => {
        if (request.method === 'OPTIONS') {
            return new Response(null, { status: 204, headers: { Vary: 'Origin', ...corsHeaders(request) } })
        }
        const response = await fetch(request, server)
        // Nothing to decorate: the request was upgraded to a WebSocket, whose
        // handshake CORS does not govern.
        if (!response) {
            return undefined
        }
        response.headers.append('Vary', 'Origin')
        Object.entries(corsHeaders(request)).forEach(([name, value]) => response.headers.set(name, value))
        return response
    }
}
