export interface CacheConfig {
    // Default TTL in seconds for endpoints enabled with `true`.
    ttl: number
    // Cached endpoints: `true` = default TTL, number = per-endpoint TTL seconds.
    endpoints: Record<string, true | number>
}

interface CacheEntry {
    data: unknown
    expires: number
}

export interface CacheLookup {
    hit: boolean
    data?: unknown
}

// In-memory TTL response cache keyed by scope + method + args. Only for
// stateless, platform-static reads — never per-user data that mutates
// in-session.
export class ResponseCache {
    readonly #config: CacheConfig
    readonly #scope: (() => string | undefined) | undefined
    readonly #entries = new Map<string, CacheEntry>()

    // `scope` names the request property that is not an argument yet decides
    // what comes back — the caller's UI language, which every server-resolved
    // label in a response is written in. Answers from different scopes are
    // different answers and are kept apart, so switching back to a language
    // already read costs no round trip.
    constructor(config: CacheConfig, scope?: () => string | undefined) {
        this.#config = config
        this.#scope = scope
    }

    // Seconds for a cached endpoint, false for an uncached one.
    ttlFor(method: string): number | false {
        const val = this.#config.endpoints[method]
        if (val === undefined) return false
        return val === true ? this.#config.ttl : val
    }

    #key(method: string, args: unknown[]): string {
        return JSON.stringify([this.#scope?.() ?? null, method, ...args])
    }

    get(method: string, args: unknown[]): CacheLookup {
        const entry = this.#entries.get(this.#key(method, args))
        if (entry && Date.now() < entry.expires) {
            return { hit: true, data: entry.data }
        }
        return { hit: false }
    }

    // Store a response for a cached endpoint; no-op for uncached methods so
    // callers can set unconditionally after a read.
    set(method: string, args: unknown[], data: unknown): void {
        const ttl = this.ttlFor(method)
        if (ttl === false) return
        this.#entries.set(this.#key(method, args), { data, expires: Date.now() + ttl * 1000 })
    }

    // Drop cached responses for an endpoint. Called after a mutation so the
    // next read reflects the change rather than a snapshot still inside its
    // TTL. With args, only the entry for that exact args-key is dropped;
    // without args, every entry for the method is dropped (a whole query
    // family). Either way in every scope: what a mutation changed is the data,
    // not the language it would be read back in.
    invalidate(method: string, ...args: unknown[]): void {
        const wanted = args.length ? JSON.stringify(args) : null
        for (const key of this.#entries.keys()) {
            const parts = JSON.parse(key) as unknown[]
            if (parts[1] !== method) {
                continue
            }
            if (wanted === null || JSON.stringify(parts.slice(2)) === wanted) {
                this.#entries.delete(key)
            }
        }
    }

    // Drop everything. For the change that invalidates the whole cache at once
    // rather than one endpoint's worth: the session identity, which every
    // audience-dependent read here was answered under.
    clear(): void {
        this.#entries.clear()
    }

    // Seed the cache with a known-fresh response — a server push already
    // carrying the full DTO a read would return — so the next read serves it
    // without a round-trip. `args` must match the read's args so the same key
    // is hit.
    prime(method: string, args: unknown[], data: unknown): void {
        this.set(method, args, data)
    }
}
