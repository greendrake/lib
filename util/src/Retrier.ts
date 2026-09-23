import { Emitter } from './Emitter'
import { TimeOutPromise } from './timing'

export class RetryStopped extends Error {
    constructor() {
        super('Retrier stopped')
        this.name = 'RetryStopped'
    }
}

export interface RetrierOptions<T> {
    attempt: () => Promise<T>
    // Result gate: a resolved attempt whose result is not accepted counts as a
    // failure and schedules a retry.
    accept?: (result: T) => boolean
    // Error gate: return false to make the error fatal (rethrown from run()).
    shouldRetry?: (error: unknown) => boolean
    // Seconds to wait between attempts: a constant, or a function of the
    // 0-based retry index for backoff schedules.
    delaySeconds?: number | ((retryIndex: number) => number)
}

export interface RetrierEvents {
    trying: []
    // Emitted once per second while waiting for the next attempt, counting down.
    waiting: [secondsLeft: number]
    [key: string]: unknown[]
}

const DEFAULT_DELAY_SECONDS = 4

// Retry orchestration: run() resolves with the first accepted result, rethrows
// errors its gate declares fatal, and rejects with RetryStopped when stop() is
// called. Status is observable via typed events — rendering (message strings,
// reactive refs) is entirely the consumer's concern.
export class Retrier<T> extends Emitter<RetrierEvents> {
    #options: RetrierOptions<T>
    #stopped = false
    #pendingWait?: TimeOutPromise

    constructor(options: RetrierOptions<T>) {
        super()
        this.#options = options
    }

    get stopped(): boolean {
        return this.#stopped
    }

    async run(): Promise<T> {
        let retryIndex = 0
        while (true) {
            if (this.#stopped) {
                throw new RetryStopped()
            }
            this.emit('trying')
            try {
                const result = await this.#options.attempt()
                if (this.#options.accept && !this.#options.accept(result)) {
                    throw new Error('Unacceptable result')
                }
                return result
            } catch (e) {
                if (this.#stopped) {
                    throw new RetryStopped()
                }
                if (this.#options.shouldRetry && !this.#options.shouldRetry(e)) {
                    throw e
                }
                await this.#wait(retryIndex)
                retryIndex++
            }
        }
    }

    async #wait(retryIndex: number): Promise<void> {
        const { delaySeconds } = this.#options
        const delay = typeof delaySeconds === 'function' ? delaySeconds(retryIndex) : (delaySeconds ?? DEFAULT_DELAY_SECONDS)
        let remainingMs = delay * 1000
        let secondsLeft = Math.ceil(delay)
        while (remainingMs > 0 && !this.#stopped) {
            this.emit('waiting', secondsLeft)
            const chunk = Math.min(1000, remainingMs)
            this.#pendingWait = new TimeOutPromise(chunk)
            try {
                await this.#pendingWait.promise
            } catch {
                // cancelled by stop(); the run() loop re-checks #stopped
            }
            remainingMs -= chunk
            secondsLeft--
        }
    }

    stop(): void {
        this.#stopped = true
        this.#pendingWait?.cancel()
    }
}
