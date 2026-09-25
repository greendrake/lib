import type { MethodMap, MethodMapConstraint, Transport } from './types'
import { packRequest } from './protocol'

// Typed facade over a transport: methods come from the app's declared
// MethodMap, so arguments and results are fully inferred — no bracket-notation
// proxies, no casts at call sites.
//    const client = new RpcClient<Methods>(transport)
//    const user = await client.call('user.get', id)
export class RpcClient<M extends MethodMapConstraint<M> = MethodMap> {
    readonly transport: Transport
    // Supplies the caller's UI language tag per request. Evaluated per call,
    // like HttpTransport's headers provider, so a locale switch takes effect on
    // the next call rather than at construction.
    readonly #locale: (() => string | undefined) | undefined

    constructor(transport: Transport, locale?: () => string | undefined) {
        this.transport = transport
        this.#locale = locale
    }

    // The one way to invoke a method; every request carries a nonce for
    // server-side dedup. Omitting the nonce is only worth doing where a
    // response cache needs byte-identical requests to key on, and this client
    // holds no cache — a client composing one owns that rule, derived from the
    // single list of cacheable methods it is configured with.
    call<K extends keyof M & string>(method: K, ...args: Parameters<M[K]>): Promise<Awaited<ReturnType<M[K]>>> {
        return this.#invoke(true, method, args)
    }

    async #invoke<K extends keyof M & string>(nonce: boolean, method: K, args: unknown[]): Promise<Awaited<ReturnType<M[K]>>> {
        return (await this.transport.send(packRequest(method, args, nonce, this.#locale?.()))) as Awaited<ReturnType<M[K]>>
    }
}
