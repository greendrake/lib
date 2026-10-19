// Exponential backoff for retry loops. The first delay after a reset is zero, so a
// one-off failure recovers immediately; each further failure without a reset doubles
// the wait up to a cap.
export class Backoff {
    readonly #baseMs: number
    readonly #maxMs: number
    #delayMs = 0

    constructor(baseMs: number, maxMs: number) {
        this.#baseMs = baseMs
        this.#maxMs = maxMs
    }

    // How long to wait before the attempt being made now, advancing the sequence.
    next(): number {
        const delayMs = this.#delayMs
        this.#delayMs = delayMs === 0 ? this.#baseMs : Math.min(delayMs * 2, this.#maxMs)
        return delayMs
    }

    reset(): void {
        this.#delayMs = 0
    }
}
