import { createDeferred, type Deferred } from './misc'

export class TimeOutPromise {
    #timeout: ReturnType<typeof setTimeout>
    #reject: (reason?: unknown) => void
    isResolved = false
    promise: Promise<void>

    constructor(ms: number) {
        this.#reject = () => {}
        this.#timeout = setTimeout(() => {}, 0)
        this.promise = new Promise<void>((resolve, reject) => {
            this.#reject = reject
            this.#timeout = setTimeout(() => {
                resolve()
                this.isResolved = true
            }, ms)
        })
    }

    cancel(): void {
        if (!this.isResolved) {
            clearTimeout(this.#timeout)
            this.#reject()
        }
    }

    static async wait(ms: number): Promise<void> {
        await new this(ms).promise
    }
}

// Counter/proceed debouncing: `fire()` waits the delay and returns a `proceed`
// predicate that is true only if no later fire (or cancel) superseded this one.
//    const proceed = await debouncer.fire()
//    if (proceed()) { ...commit... }
export class Debouncer {
    #ms: number
    #counter = 0

    constructor(ms: number) {
        this.#ms = ms
    }

    // ms overrides the constructed default for this call only — lets a caller
    // vary the delay per fire (e.g. a shorter debounce when the action is cheap).
    async fire(ms?: number): Promise<() => boolean> {
        this.#counter++
        const c = this.#counter
        const delay = ms ?? this.#ms
        if (delay) {
            await TimeOutPromise.wait(delay)
        }
        return () => c === this.#counter
    }

    // Invalidate any in-flight fire(): bumping the counter makes every pending
    // proceed() return false, so an action debounced before cancel() never commits.
    cancel(): void {
        this.#counter++
    }
}

// One run at a time, and at most one more behind it however many times it is
// pulled meanwhile — for work whose every request asks the same question (read
// this again, rebuild that), so the answer to the last request is the answer to
// all of them. A pull settles with the outcome of the run that covers it: the
// one it started, or the one queued behind a run already going. Never the run
// in progress, which may have begun before whatever prompted the pull.
//    const refresh = coalesced(() => load())
//    store.on('changed', () => void refresh().catch(report))
// The outcome is the puller's: a pull nobody handles rejects unhandled like any
// other promise. Whatever the outcome, the slot is freed.
export const coalesced = <T>(run: () => T | Promise<T>): (() => Promise<T>) => {
    let running = false
    // The run queued behind the current one, as every pull covered by it holds it.
    let queued: Deferred<T> | null = null

    // The slot is passed on before the finished run's pulls are settled, so
    // whoever they wake finds the queued run already under way rather than a
    // slot about to change hands.
    const finish = (): void => {
        running = false
        const next = queued
        queued = null
        if (next) start(next)
    }

    const start = (covering: Deferred<T>): void => {
        running = true
        // `new Promise(resolve => resolve(run()))` rather than
        // Promise.resolve(run()): it puts the call inside the promise, so a run
        // that throws synchronously is a rejection like any other instead of
        // escaping with the slot still held, which would wedge every later pull.
        void new Promise<T>(resolve => resolve(run())).then(
            value => {
                finish()
                covering.resolve(value)
            },
            (error: unknown) => {
                finish()
                covering.reject(error)
            }
        )
    }

    return () => {
        if (running) {
            queued ??= createDeferred<T>()
            return queued.promise
        }
        const covering = createDeferred<T>()
        start(covering)
        return covering.promise
    }
}

// Named-timeout registry: scheduling under an existing name replaces the
// pending timeout. Instance-scoped — each consumer creates its own registry,
// so equal names in unrelated consumers cannot collide.
export class Delayed {
    #timeouts = new Map<string, ReturnType<typeof setTimeout>>()

    go(name: string, fn: () => void, ms: number): void {
        this.cancel(name)
        this.#timeouts.set(
            name,
            setTimeout(() => {
                this.#timeouts.delete(name)
                fn()
            }, ms)
        )
    }

    cancel(name: string): void {
        const timeout = this.#timeouts.get(name)
        if (timeout !== undefined) {
            clearTimeout(timeout)
            this.#timeouts.delete(name)
        }
    }

    cancelAll(): void {
        this.#timeouts.forEach(t => clearTimeout(t))
        this.#timeouts.clear()
    }
}
