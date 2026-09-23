import { isRef, type Ref } from 'vue'
import { createDeferred, NetworkError, TimeOutPromise } from '@greendrake/util'
import { ResponseCache, WebSocketClosedError, codeOf, packRequest } from '@greendrake/rpc'
import type { CacheConfig, MethodMap, MethodMapConstraint, Transport } from '@greendrake/rpc'
import { useAppState } from '@greendrake/vue-kit'
import { useConnectivity } from './connectivity'
import { useExceptionState } from './exceptionState'

export class UnacceptableResultError extends Error {
    constructor() {
        super('Unacceptable result')
        this.name = 'UnacceptableResultError'
    }
}

export interface CallOptions {
    // Loading indication: undefined → the global spinner counter; a Ref →
    // that scoped busy flag; false → none (a button label is already
    // communicating progress).
    pending?: Ref<boolean> | false
    // Reject results the server calls success but the caller cannot accept.
    // Returning an Error throws it; returning false throws
    // UnacceptableResultError. Feeds the same retry pipeline as failures.
    validate?: (result: unknown) => boolean | Error
    // Bypass loading indication AND the error-UX pipeline: errors reject
    // straight to the caller. For background calls (token refresh) and callers
    // owning their own error presentation.
    silent?: boolean
    // Refuse the response cache for this call: read past whatever is stored,
    // and do not store the answer either. For the read whose whole promise is
    // freshness on a method that is worth caching everywhere else — the same
    // query serving a "what's here right now" surface and a search box. It
    // says nothing about the method: an unlisted one cannot be opted in, and a
    // listed one keeps its nonce-lessness (see `call`). See DATA_CACHING.md §4.
    cache?: false
}

export interface ApiClientOptions {
    transport: Transport
    // Which methods may be answered from an in-browser response cache, and for
    // how long. THE list drives two things at once — see `call` below: a listed
    // method is served/stored from the cache and goes out nonce-less; every
    // other method carries a nonce and is never cached. Only list stateless,
    // platform-static reads — never per-user data that mutates in-session.
    cache?: CacheConfig
    // Per-error-code interception, keyed by code (the error's message). A
    // handler that re-throws bubbles the error to the caller (bypassing the
    // toast); a handler that returns parks the call forever — its flow is
    // abandoned, superseded by the handler's side effect (logout, redirect).
    errorHandlers?: Record<string, (e: Error) => void>
    // The caller's UI language tag, evaluated per request. Server-resolved
    // labels (taxonomy names, breadcrumbs, facet chips) come back in it, so a
    // locale switch must take effect on the next call rather than at
    // construction — and it scopes the response cache, which would otherwise
    // answer a call made in one language with a reply written in another.
    locale?: () => string | undefined
}

// The Vue-facing API client: typed calls with loading-state wiring, response
// caching, and the blocking-toast retry pipeline.
export class ApiClient<M extends MethodMapConstraint<M> = MethodMap> {
    readonly transport: Transport
    readonly #cache?: ResponseCache
    readonly #errorHandlers?: Record<string, (e: Error) => void>
    // Dev-only per-method artificial delays (ms), keyed by method name. See
    // setDebugDelay / #debugDelay.
    readonly #debugDelays = new Map<string, number>()
    readonly #locale?: () => string | undefined

    constructor(options: ApiClientOptions) {
        this.transport = options.transport
        // The locale scopes the cache as well as stamping the request: a
        // response carries server-resolved labels in the language it was asked
        // in, so it may only answer a later call asking in that same one.
        this.#cache = options.cache && new ResponseCache(options.cache, options.locale)
        this.#errorHandlers = options.errorHandlers
        this.#locale = options.locale
    }

    // DEV DEBUGGING ONLY. Register an artificial delay (ms) for a method, held
    // while the call is pending — so the pending/loading state (spinner, busy
    // ref, disabled controls) can be inspected in devtools before the response
    // lands. Set it from the console, e.g. `api.setDebugDelay('search.listings',
    // 3000)`; pass 0 to clear. The delay is applied only in dev builds (see
    // #debugDelay), so a stray registration is inert in production. A `read`
    // served from cache is instant and bypasses the delay (no pending to see);
    // opt out of the cache to delay it.
    setDebugDelay(method: keyof M & string, ms: number): void {
        if (ms > 0) {
            this.#debugDelays.set(method, ms)
        } else {
            this.#debugDelays.delete(method)
        }
    }

    // Node-safe DEV probe (mirrors rpc/protocol.ts): import.meta.env only exists
    // under Vite, and is statically replaced so the delay is dead-code-stripped
    // from production builds.
    async #debugDelay(method: string): Promise<void> {
        const ms = this.#debugDelays.get(method)
        if (ms && (import.meta as ImportMeta & { env?: { DEV?: boolean } }).env?.DEV) {
            await TimeOutPromise.wait(ms)
        }
    }

    // The one way to invoke a method. Whether the request carries a nonce is
    // not a per-call-site choice: it follows from the cache config, which is
    // the single declaration of "this answer may be reused".
    //
    //   listed in cache.endpoints → served from / stored in the cache, sent
    //                               WITHOUT a nonce
    //   not listed                → straight to the wire WITH a nonce
    //
    // Why that pairing, and why it is safe to let one list decide both: a
    // nonce's only server-side effect is `SETNX` dedup (the server's dispatch
    // rejects a repeat with DUPLICATE_REQUEST), and a fresh random nonce is
    // minted per call, so on a read it buys nothing and costs one Redis write.
    // Sending it is therefore harmless but pointless — which makes "cacheable"
    // the only axis worth declaring. The protocol's rule that nonce-bearing
    // requests are never cached is preserved by construction.
    //
    // The trade being made: a method that is idempotent but deliberately
    // uncached — left off the list, not merely read past it by a call site —
    // now pays that one Redis write per call. Revisit the split — declaring idempotency per method,
    // separate from cacheability — if those writes ever start to matter, or if
    // server-side response caching is built (there is none today, so nonce-less
    // buys nothing extra), or if `Request.Abort` (which cancels by nonce) gets
    // wired up on a cacheable method.
    async call<K extends keyof M & string>(method: K, args: Parameters<M[K]>, options?: CallOptions): Promise<Awaited<ReturnType<M[K]>>> {
        const cacheable = this.#cache?.ttlFor(method) !== false && this.#cache !== undefined
        // Refusing the cache is about this answer, not about the method: it
        // suppresses the lookup and the store, and deliberately does NOT touch
        // the nonce. The protocol's rule runs one way — a nonce-bearing request
        // is never cached — so a cacheable method read fresh stays nonce-less
        // and keeps what nonce-lessness buys it, which is not only the Redis
        // write it saves but offline parking: a nonce-less read dropped in
        // flight waits out the outage instead of asking the user to retry it.
        const useCache = cacheable && options?.cache !== false
        if (useCache) {
            const cached = this.#cache!.get(method, args)
            if (cached.hit) {
                return cached.data as Awaited<ReturnType<M[K]>>
            }
        }
        return this.#run(!cacheable, useCache, method, args, options)
    }

    invalidateCache(method: keyof M & string, ...args: unknown[]): void {
        this.#cache?.invalidate(method, ...args)
    }

    // Drop every cached response. What the session identity changing costs: a
    // cached read was answered for whoever was signed in when it was made, and
    // the audience-dependent ones (what a listing shows, what a search returns,
    // what an account owns) do not survive that change even when their TTL has
    // not run out.
    clearCache(): void {
        this.#cache?.clear()
    }

    // Seed the cache from a server push already carrying the full DTO a read
    // would return. `args` must match the read's args to hit the same key.
    primeCache(method: keyof M & string, args: unknown[], data: unknown): void {
        this.#cache?.prime(method, args, data)
    }

    async #run<K extends keyof M & string>(nonce: boolean, store: boolean, method: K, args: Parameters<M[K]>, options?: CallOptions): Promise<Awaited<ReturnType<M[K]>>> {
        const send = async (): Promise<Awaited<ReturnType<M[K]>>> => {
            const result = await this.transport.send(packRequest(method, args, nonce, this.#locale?.()))
            if (options?.validate) {
                const verdict = options.validate(result)
                if (verdict instanceof Error) {
                    throw verdict
                }
                if (!verdict) {
                    throw new UnacceptableResultError()
                }
            }
            // Stores exactly the responses the config asked to be reusable,
            // minus any the caller read fresh: those must not be left behind
            // for the next reader either, or refusing the cache would amount to
            // refreshing it. set() is a no-op for anything unlisted.
            if (store) {
                this.#cache?.set(method, args, result)
            }
            return result as Awaited<ReturnType<M[K]>>
        }

        if (options?.silent) {
            return send()
        }

        const appState = useAppState()
        const exceptionState = useExceptionState()
        const connectivity = useConnectivity()
        const pending = options?.pending
        const switchLoading = (on: boolean): void => {
            if (isRef(pending)) {
                pending.value = on
            } else if (pending === undefined) {
                appState.setLoading(on)
            }
        }

        let settleRetryAttempt: (() => void) | undefined
        // Whether THIS call raised the exception currently on screen. A
        // success only clears its own error toast — never one a concurrent
        // call is showing (e.g. a background list refresh must not dismiss a
        // navigation's "not found" toast it had nothing to do with).
        let raisedException = false
        // Drop this call's own toast once it no longer needs one: on success,
        // and on parking to wait out an outage (which takes the failure off
        // the user's hands entirely). Clearing the flag with it keeps the
        // claim honest across a park — whatever appears on screen afterwards
        // belongs to someone else until this call raises again.
        const clearOwnException = (): void => {
            if (raisedException && exceptionState.exception) {
                exceptionState.fail(false)
            }
            raisedException = false
        }
        while (true) {
            switchLoading(true)
            try {
                // Held inside the pending window so the delay prolongs the
                // observable loading state (dev only; see #debugDelay).
                await this.#debugDelay(method)
                const result = await send()
                switchLoading(false)
                clearOwnException()
                return result
            } catch (e) {
                settleRetryAttempt?.()
                // Offline: wait for the connection instead of asking the user
                // to. Each attempt re-runs send(), which mints a fresh nonce
                // (see packRequest), so the server's SETNX dedup cannot span a
                // retry — parking is only safe where re-issuing cannot
                // double-apply. That holds when the request demonstrably never
                // left this machine (the socket was already down, so the
                // transport rejected before writing), and when it carries no
                // nonce at all (a cacheable read; see call). A nonce-carrying
                // request that was on the wire when the socket went may have
                // executed with only its response lost, so whether to re-send
                // it stays the user's decision and keeps the toast.
                const neverSent = e instanceof WebSocketClosedError && !e.inFlight
                if (e instanceof NetworkError && connectivity.offline && (!nonce || neverSent)) {
                    // Park OUTSIDE the loading window: holding the global
                    // counter for the whole outage would pin the spinner and
                    // wedge isFirstLoading (which flips when the counter
                    // empties). A scoped pending ref is the opposite case and
                    // stays on — the page remains interactive under the
                    // offline banner, and that busy flag is what keeps its
                    // control disabled so a form cannot queue duplicate
                    // submissions that all fire at once on reconnect.
                    if (pending === undefined) {
                        appState.setLoading(false)
                    }
                    clearOwnException()
                    await connectivity.whenOnline()
                    continue
                }
                switchLoading(false)
                // Keyed by backend code, which for a *_NOT_FOUND lives on the
                // error rather than in its message — see codeOf.
                const handler = this.#errorHandlers?.[codeOf(e as Error)]
                if (handler) {
                    // Pass the original error so a handler that re-throws (the
                    // bubble pattern) preserves its payload (ApiError.details).
                    handler(e as Error)
                    // A non-throwing handler's side effect (logout, redirect)
                    // supersedes this call's flow: park it forever rather than
                    // lie with an undefined result.
                    return new Promise<never>(() => {})
                }
                const decision = createDeferred<void>()
                const retryAttempt = createDeferred<void>()
                settleRetryAttempt = retryAttempt.resolve
                exceptionState.fail(e as Error, {
                    // The toast's "Trying..." state lasts until the retried
                    // attempt settles (tracked via retryAttempt).
                    retry: () => {
                        decision.resolve()
                        return retryAttempt.promise
                    },
                    cancel: decision.reject,
                    // Identifies the failed RPC in error reports. Args are
                    // deliberately omitted — they can carry credentials/PII.
                    reportContext: { apiMethod: method }
                })
                raisedException = true
                try {
                    await decision.promise
                } catch {
                    throw e
                }
            }
        }
    }
}
