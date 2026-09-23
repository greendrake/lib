import type { FetchHandler } from './serve'

// What a browser may send. The list is the union of what a JSON API over fetch
// needs; a service whose clients send more names them in `allowHeaders`.
const REQUEST_HEADERS = ['Accept', 'Authorization', 'Cache-Control', 'Content-Type', 'Origin', 'X-Requested-With']

const METHODS = ['GET', 'POST', 'PUT', 'DELETE', 'OPTIONS']

export interface CorsOptions {
    // `*` by default. These APIs authorize by token rather than by cookie, so
    // the browser's origin is not what controls access — locking it down is a
    // defence in depth a deployment may still want.
    origin?: string
    // Added to the defaults above.
    allowHeaders?: string[]
    // Response headers a browser client may read. Everything else is hidden
    // from it whatever the server sends.
    exposeHeaders?: string[]
}

// CORS at the outermost layer, so a preflight is answered before it reaches
// the application — and before any middleware hung off it that would charge a
// rate limit or a captcha for a request the browser made on its own.
export const withCors = <T>(fetch: FetchHandler<T>, options: CorsOptions = {}): FetchHandler<T> => {
    const origin = options.origin ?? '*'
    const headers: Record<string, string> = {
        'Access-Control-Allow-Origin': origin,
        'Access-Control-Allow-Headers': [...REQUEST_HEADERS, ...(options.allowHeaders ?? [])].join(', '),
        'Access-Control-Allow-Methods': METHODS.join(', ')
    }
    // A browser refuses credentials against a wildcard origin, so claiming to
    // allow them there is not merely useless but misleading.
    if (origin !== '*') {
        headers['Access-Control-Allow-Credentials'] = 'true'
    }
    if (options.exposeHeaders?.length) {
        headers['Access-Control-Expose-Headers'] = options.exposeHeaders.join(', ')
    }

    return async (request, server) => {
        if (request.method === 'OPTIONS') {
            return new Response(null, { status: 204, headers })
        }
        const response = await fetch(request, server)
        // Nothing to decorate: the request was upgraded to a WebSocket, whose
        // handshake CORS does not govern.
        if (!response) {
            return undefined
        }
        Object.entries(headers).forEach(([name, value]) => response.headers.set(name, value))
        return response
    }
}
