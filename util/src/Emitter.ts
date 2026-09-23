export type EventMap = Record<string, unknown[]>

type AnyListener = (...args: unknown[]) => void

// Payload-typed event emitter used by composition (never inheritance of a god
// base class). `on` returns the unsubscribe function so callers — Vue
// components in particular — can detach in one line on teardown.
export class Emitter<E extends EventMap> {
    #listeners = new Map<keyof E, Set<AnyListener>>()
    #onceListeners = new Map<keyof E, Set<AnyListener>>()

    on<K extends keyof E>(event: K, listener: (...args: E[K]) => void): () => void {
        return this.#add(this.#listeners, event, listener)
    }

    once<K extends keyof E>(event: K, listener: (...args: E[K]) => void): () => void {
        return this.#add(this.#onceListeners, event, listener)
    }

    #add<K extends keyof E>(map: Map<keyof E, Set<AnyListener>>, event: K, listener: (...args: E[K]) => void): () => void {
        let set = map.get(event)
        if (!set) {
            set = new Set()
            map.set(event, set)
        }
        set.add(listener as AnyListener)
        return () => this.off(event, listener)
    }

    off<K extends keyof E>(event: K, listener: (...args: E[K]) => void): void {
        this.#listeners.get(event)?.delete(listener as AnyListener)
        this.#onceListeners.get(event)?.delete(listener as AnyListener)
    }

    emit<K extends keyof E>(event: K, ...args: E[K]): void {
        this.#listeners.get(event)?.forEach(l => l(...args))
        const once = this.#onceListeners.get(event)
        if (once) {
            this.#onceListeners.delete(event)
            once.forEach(l => l(...args))
        }
    }
}
