// Thrown when a request fails at the network/transport layer — DNS failure,
// connection refused, TLS/CORS rejection, or the device being offline. fetch()
// rejects only in these cases; a non-2xx HTTP status resolves normally, so
// this never masks a server error response. The wrapped browser error is kept
// as `cause` so its message ("Failed to fetch" / "Load failed") and stack
// remain available for diagnostics. Transport-level failures elsewhere (e.g.
// an abnormal WebSocket close) extend this class so consumers classify all of
// them with one instanceof check.
export class NetworkError extends Error {
    constructor(cause: unknown) {
        super(cause instanceof Error ? cause.message : String(cause), { cause })
        this.name = 'NetworkError'
    }
}

export class HttpError extends Error {
    readonly status: number
    readonly statusText: string

    constructor(status: number, statusText: string) {
        super(`${status} ${statusText}`)
        this.name = 'HttpError'
        this.status = status
        this.statusText = statusText
    }
}

// A response that is not ok, from a request that is named with it: the method,
// the path with its query, and the whole body the server answered — parsed when
// the response says it is JSON, its text otherwise, null when empty. Never a
// field picked out of it: what an API puts its reason in is the reader's to
// know.
export class ApiResponseError extends HttpError {
    readonly method: string
    readonly path: string
    readonly body: unknown

    constructor(method: string, path: string, status: number, statusText: string, body: unknown) {
        super(status, statusText)
        this.name = 'ApiResponseError'
        this.message = `${method} ${path} → ${status}\n${JSON.stringify(body, null, 2)}`
        this.method = method
        this.path = path
        this.body = body
    }
}

// The one call a client makes to reach the network. Declared rather than taken
// as `typeof fetch`: this source is type-checked under each consumer's globals,
// where `typeof fetch` is whatever that runtime adds to it (Bun's carries
// `fetch.preconnect`), and a test double would have to fake all of it.
export type Fetch = (url: string, init: RequestInit) => Promise<Response>

// What an API client built on `request` is constructed with: the fetch it calls
// and the clock it reads, in milliseconds as Date.now gives them.
export interface ClientOptions {
    fetch: Fetch
    now: () => number
}

// A request's init, or one whose body is a value to send as JSON.
export type ApiRequestInit = RequestInit | (Omit<RequestInit, 'body'> & { json: unknown })

const JSON_MEDIA_TYPE = /^application\/(?:[\w.-]+\+)?json$/i

// One round trip to an API. `fetch` is called once and outside any try, so its
// rejection — no response at all — reaches the caller as it is; a status that
// is not ok throws ApiResponseError; an ok one answers its body parsed as JSON,
// null when empty.
export const request = async <T>(fetch: Fetch, url: string, init: ApiRequestInit = {}): Promise<T> => {
    let sent: RequestInit = init
    if ('json' in init) {
        const { json, ...rest } = init
        const headers = new Headers(rest.headers)
        headers.set('Content-Type', 'application/json')
        sent = {
            ...rest,
            headers,
            body: JSON.stringify(json)
        }
    }
    const response = await fetch(url, sent)
    const text = await response.text()
    if (response.ok) {
        return (text ? JSON.parse(text) : null) as T
    }
    const mediaType = response.headers.get('Content-Type')?.split(';')[0]?.trim() ?? ''
    const body: unknown = !text ? null : JSON_MEDIA_TYPE.test(mediaType) ? JSON.parse(text) : text
    // A relative URL, as a browser takes one, is a path already.
    let path = url
    if (URL.canParse(url)) {
        const { pathname, search } = new URL(url)
        path = `${pathname}${search}`
    }
    throw new ApiResponseError(init.method ?? 'GET', path, response.status, response.statusText, body)
}

// `request`, carrying `Authorization: Bearer` with whatever `token` answers at
// the time of each call — a token minted per request, or one held until it
// nearly expires.
export const bearerRequest =
    (fetch: Fetch, token: () => string | Promise<string>) =>
    async <T>(url: string, init: ApiRequestInit = {}): Promise<T> => {
        const headers = new Headers(init.headers)
        headers.set('Authorization', `Bearer ${await token()}`)
        return request<T>(fetch, url, { ...init, headers })
    }

export const safeFetch = async (...args: Parameters<typeof fetch>): Promise<Response> => {
    try {
        return await fetch(...args)
    } catch (e) {
        throw new NetworkError(e)
    }
}

// Which browser origins a server admits: every one ('*'), those listed as
// browsers send them ('https://app.example.com', 'app://localhost'), or
// those a predicate admits.
export type OriginPolicy = '*' | readonly string[] | ((origin: string) => boolean)

export const admitsOrigin = (policy: OriginPolicy, origin: string): boolean => {
    if (policy === '*') {
        return true
    }
    return typeof policy === 'function' ? policy(origin) : policy.includes(origin)
}

// Typed JSON GET/…: NetworkError on transport failure, ApiResponseError — an
// HttpError — on non-2xx.
export const fetchJson = <T>(url: string, init?: RequestInit): Promise<T> => request<T>(safeFetch, url, init)
