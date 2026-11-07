import { afterAll, beforeAll } from 'bun:test'
import type { Fetch } from './net'

// A request a recording fetch was handed.
export interface RecordedRequest {
    url: string
    method: string
    headers: Headers
    body: RequestInit['body']
}

// A Fetch standing in for a server: it records each request, in order, and
// answers whatever `answer` makes of it — a Response, or a rejection by
// throwing.
export const recordingFetch = (answer: (request: RecordedRequest) => Response | Promise<Response>): { fetch: Fetch; requests: RecordedRequest[] } => {
    const requests: RecordedRequest[] = []
    const fetch: Fetch = async (url, init) => {
        const request = {
            url,
            method: init.method ?? 'GET',
            headers: new Headers(init.headers),
            body: init.body
        }
        requests.push(request)
        return answer(request)
    }
    return { fetch, requests }
}

// A Storage backed by a Map — bun has neither localStorage nor sessionStorage
// — and the Map, for a test to inspect and clear.
export const memoryStorage = (): { storage: Storage; store: Map<string, string> } => {
    const store = new Map<string, string>()
    const storage: Storage = {
        get length() {
            return store.size
        },
        key: (index: number) => [...store.keys()][index] ?? null,
        getItem: (key: string) => store.get(key) ?? null,
        setItem: (key: string, value: string) => {
            store.set(key, value)
        },
        removeItem: (key: string) => {
            store.delete(key)
        },
        clear: () => store.clear()
    }
    return { storage, store }
}

// bun test runs a run's test files in one process, so they share the global
// object: a global one file puts in is in place for every file after it. The
// calling file — or describe block — gets the globals `values` returns for its
// own tests: computed and put in before the first, and after the last each
// name's own property put back, or the name removed where it had none. Both
// are hooks, so a file a filter leaves no tests to run touches neither, where
// a global put in at module scope would be in place for a run that skips the
// file, and never be put back.
export const stubGlobals = (values: () => Record<string, unknown>): void => {
    const previous = new Map<string, PropertyDescriptor | undefined>()
    beforeAll(() => {
        for (const [name, value] of Object.entries(values())) {
            previous.set(name, Object.getOwnPropertyDescriptor(globalThis, name))
            Object.defineProperty(globalThis, name, { value, configurable: true })
        }
    })
    afterAll(() => {
        for (const [name, descriptor] of previous) {
            if (descriptor) {
                Object.defineProperty(globalThis, name, descriptor)
            } else {
                Reflect.deleteProperty(globalThis, name)
            }
        }
    })
}
