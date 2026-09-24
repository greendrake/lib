import { Emitter } from './Emitter'

export interface StalenessEvents {
    fresh: []
    stale: []
    [key: string]: unknown[]
}

// Renewable freshness flag for push data streams: renew() on every inbound
// datum keeps `isFresh` true; when no renewal arrives within ttlMs the flag
// drops and 'stale' fires. Events fire only on transitions.
export class StalenessDetector extends Emitter<StalenessEvents> {
    #ttlMs: number
    #timeout: ReturnType<typeof setTimeout> | undefined
    #fresh = false

    constructor(ttlMs: number) {
        super()
        this.#ttlMs = ttlMs
    }

    get isFresh(): boolean {
        return this.#fresh
    }

    renew(): void {
        if (this.#timeout) {
            clearTimeout(this.#timeout)
        }
        if (!this.#fresh) {
            this.#fresh = true
            this.emit('fresh')
        }
        this.#timeout = setTimeout(() => {
            this.#fresh = false
            this.emit('stale')
        }, this.#ttlMs)
    }

    dispose(): void {
        if (this.#timeout) {
            clearTimeout(this.#timeout)
            this.#timeout = undefined
        }
    }
}
