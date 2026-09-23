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
