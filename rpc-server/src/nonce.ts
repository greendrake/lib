import type { NonceStore } from './types'

// In-process nonce store: a Map swept on insert, so it needs no timer and no
// teardown. One process only — behind a load balancer the replicas would each
// keep their own set and a retry landing elsewhere would run twice, so a
// multi-instance deployment needs a shared store (Redis `SET NX PX` with the
// same TTL implements this interface in a few lines).
export const memoryNonceStore = (ttlMs: number): NonceStore => {
    // Insertion order is expiry order, the TTL being constant, so the sweep
    // stops at the first entry still alive instead of walking the whole map.
    const expiries = new Map<string, number>()
    return {
        checkAndSet: nonce => {
            const now = Date.now()
            for (const [seen, expiry] of expiries) {
                if (expiry > now) {
                    break
                }
                expiries.delete(seen)
            }
            if (expiries.has(nonce)) {
                return Promise.resolve(false)
            }
            expiries.set(nonce, now + ttlMs)
            return Promise.resolve(true)
        }
    }
}
