import { HttpError, NetworkError, safeFetch } from '@greendrake/util'
import { identityCodec } from './types'
import type { Codec, HeadersProvider, RpcRequest, RpcResponse, Transport } from './types'
import { settleResponse } from './protocol'

export interface HttpTransportOptions {
    url: string
    codec?: Codec
    headers?: HeadersProvider
    // Merged into every request — e.g. { keepalive: true } for fire-and-forget
    // calls that must survive page unload.
    requestInit?: Partial<RequestInit>
}

export class HttpTransport implements Transport {
    readonly #options: HttpTransportOptions
    readonly #codec: Codec

    constructor(options: HttpTransportOptions) {
        this.#options = options
        this.#codec = options.codec ?? identityCodec
    }

    async send(request: RpcRequest): Promise<unknown> {
        const response = await safeFetch(this.#options.url, {
            ...this.#options.requestInit,
            method: 'POST',
            headers: this.#options.headers?.() ?? {},
            body: this.#codec.encode(JSON.stringify(request))
        })
        // Every server of the format answers an envelope with a 200: anything else
        // is the answer of something between, a proxy's 502 say.
        if (!response.ok) {
            throw new HttpError(response.status, response.statusText)
        }
        let body: string
        try {
            body = await response.text()
        } catch (e) {
            throw new NetworkError(e)
        }
        return settleResponse(JSON.parse(this.#codec.decode(body)) as RpcResponse, request.method)
    }
}
