import { safeFetch } from '@greendrake/util'
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
        const text = this.#codec.decode(await response.text())
        return settleResponse(JSON.parse(text) as RpcResponse, request.method)
    }
}
