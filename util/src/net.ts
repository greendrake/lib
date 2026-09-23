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

export const safeFetch = async (...args: Parameters<typeof fetch>): Promise<Response> => {
    try {
        return await fetch(...args)
    } catch (e) {
        throw new NetworkError(e)
    }
}

// Typed JSON GET/…: NetworkError on transport failure, HttpError on non-2xx.
export const fetchJson = async <T>(url: string, init?: RequestInit): Promise<T> => {
    const response = await safeFetch(url, init)
    if (!response.ok) {
        throw new HttpError(response.status, response.statusText)
    }
    return (await response.json()) as T
}
