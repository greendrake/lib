import { beforeEach, describe, expect, test } from 'bun:test'
import { ref } from 'vue'
import { ApiError, NotFoundError } from '@greendrake/rpc'
import { NetworkError } from '@greendrake/util'
import { useAppState } from '@greendrake/vue-kit'
import { ApiClient, UnacceptableResultError, bubbled } from '../src/ApiClient'
import { useConnectivity } from '../src/connectivity'
import { classifyException, useExceptionState } from '../src/exceptionState'
import { FakeSource, ScriptedTransport, setDeviceOnline, tick, withSource } from './support'

interface Methods {
    'thing.get': () => string
    'thing.create': () => string
    'thing.find': (query: { q: string }) => string
}

// One offline call, its transport and the oracle that will bring it back.
const offlineCall = (cacheable?: boolean) => {
    const source = new FakeSource()
    withSource(source)
    const connectivity = useConnectivity()
    const exceptionState = useExceptionState()
    const appState = useAppState()
    const transport = new ScriptedTransport()
    const api = new ApiClient<Methods>({
        transport,
        cache: cacheable ? { ttl: 300, endpoints: { 'thing.get': true } } : undefined
    })
    return {
        source,
        connectivity,
        exceptionState,
        appState,
        transport,
        api
    }
}

beforeEach(() => {
    setDeviceOnline(true)
})

describe('ApiClient calls', () => {
    test('send a method its arguments as given, and one that takes none none at all', async () => {
        const { transport, api } = offlineCall()
        await api.call('thing.find', { q: 'a' })
        await api.call('thing.create')
        expect(transport.sent[0]!.arguments).toEqual([{ q: 'a' }])
        expect('arguments' in transport.sent[1]!).toBe(false)
    })

    test('made through with run under its options, and the client’s own under none', async () => {
        const { api } = offlineCall()
        const refusing = api.with({ silent: true, validate: () => false })
        await expect(refusing.call('thing.create')).rejects.toBeInstanceOf(UnacceptableResultError)
        expect(await api.call('thing.create')).toBe('result')
        // The options stay with the caller, call after call.
        await expect(refusing.call('thing.create')).rejects.toBeInstanceOf(UnacceptableResultError)
    })
})

describe('ApiClient offline parking', () => {
    test('a call refused before it was written resumes on reconnect, raising no toast', async () => {
        const { source, exceptionState, transport, api } = offlineCall()
        transport.outcomes = ['never-sent']
        source.report(false)

        const call = api.call('thing.create')
        await tick()
        expect(transport.sent.length).toBe(1)
        // Nothing to ask the user: the call is waiting, not failed.
        expect(exceptionState.exception).toBe(false)

        source.report(true)
        expect(await call).toBe('result')
        expect(transport.sent.length).toBe(2)
        expect(exceptionState.exception).toBe(false)
    })

    test('a parked call is re-issued once per reconnection, not once per failed attempt', async () => {
        const { source, transport, api } = offlineCall()
        transport.outcomes = ['never-sent']
        source.report(false)

        const call = api.call('thing.create')
        await tick()
        // Further failed attempts by the transport are not this call's cue —
        // only connectivity actually returning is.
        source.report(false)
        source.report(false)
        await tick()
        expect(transport.sent.length).toBe(1)

        source.report(true)
        await call
        expect(transport.sent.length).toBe(2)
    })

    test('parking releases the global loading slot', async () => {
        const { source, appState, transport, api } = offlineCall()
        transport.outcomes = ['never-sent']
        source.report(false)

        const call = api.call('thing.create')
        await tick()
        // Holding it for the whole outage would pin the spinner and wedge
        // isFirstLoading, which flips only when the counter empties.
        expect(appState.isLoading).toBe(false)
        expect(appState.isFirstLoading).toBe(false)

        source.report(true)
        await call
        expect(appState.isLoading).toBe(false)
    })

    test('a scoped pending ref stays busy for the whole park', async () => {
        const { source, transport, api } = offlineCall()
        transport.outcomes = ['never-sent']
        source.report(false)

        const pending = ref(false)
        const call = api.with({ pending }).call('thing.create')
        await tick()
        // The page stays interactive under the offline banner, so this busy
        // flag is what keeps the submitting control disabled — without it a
        // form would queue duplicates that all fire at once on reconnect.
        expect(pending.value).toBe(true)

        source.report(true)
        await call
        expect(pending.value).toBe(false)
    })

    test('a nonce-carrying call dropped in flight keeps the toast', async () => {
        const { source, exceptionState, transport, api } = offlineCall()
        transport.outcomes = ['in-flight-drop']
        source.report(false)

        const call = api.call('thing.create')
        void call.catch(() => undefined)
        await tick()
        // It may have executed server-side with only its response lost, and a
        // retry would carry a fresh nonce — so the server's dedup cannot cover
        // it and re-sending has to stay the user's decision.
        expect(exceptionState.exception).toBe('network')
        expect(transport.sent.length).toBe(1)
        expect(transport.sent[0]!.nonce).toBeString()
    })

    test('a nonce-less read dropped in flight parks anyway', async () => {
        const { source, exceptionState, transport, api } = offlineCall(true)
        transport.outcomes = ['in-flight-drop']
        source.report(false)

        const call = api.call('thing.get')
        await tick()
        // Re-running a read cannot double-apply anything, so there is nothing
        // to consult the user about.
        expect(transport.sent[0]!.nonce).toBeUndefined()
        expect(exceptionState.exception).toBe(false)

        source.report(true)
        expect(await call).toBe('result')
        expect(transport.sent.length).toBe(2)
    })

    test('a network failure the oracle does not corroborate still toasts', async () => {
        const { exceptionState, transport, api } = offlineCall()
        transport.outcomes = ['never-sent']
        // No disconnect reported: the socket is up and this is the call itself
        // failing, which the user must hear about rather than wait out.
        const call = api.call('thing.create')
        void call.catch(() => undefined)
        await tick()
        expect(exceptionState.exception).toBe('network')
    })
})

// Refusing the cache for one call. The flag moves two things and deliberately
// leaves a third alone — the nonce, and with it everything nonce-lessness buys
// a read.
describe('per-call cache refusal', () => {
    test('stores nothing, so the next reader still goes to the wire', async () => {
        const { transport, api } = offlineCall(true)

        expect(await api.with({ cache: false }).call('thing.get')).toBe('result')
        expect(transport.sent.length).toBe(1)
        // Refusing the cache must not amount to refreshing it for whoever
        // reads next — that would hand them an answer they did not ask to be
        // reusable, from a caller that wanted it fresh for itself.
        expect(await api.call('thing.get')).toBe('result')
        expect(transport.sent.length).toBe(2)
    })

    test('reads past an answer already stored', async () => {
        const { transport, api } = offlineCall(true)

        expect(await api.call('thing.get')).toBe('result')
        expect(await api.call('thing.get')).toBe('result')
        // Served from the cache the first call filled.
        expect(transport.sent.length).toBe(1)

        expect(await api.with({ cache: false }).call('thing.get')).toBe('result')
        expect(transport.sent.length).toBe(2)
    })

    test('stays nonce-less, so it still parks when dropped in flight', async () => {
        const { source, exceptionState, transport, api } = offlineCall(true)
        transport.outcomes = ['in-flight-drop']
        source.report(false)

        const call = api.with({ cache: false }).call('thing.get')
        await tick()
        // Freshness is the caller's business; whether re-running the call can
        // double-apply anything is the method's, and that has not changed.
        expect(transport.sent[0]!.nonce).toBeUndefined()
        expect(exceptionState.exception).toBe(false)

        source.report(true)
        expect(await call).toBe('result')
    })

    test('cannot opt an unlisted method into the cache', async () => {
        const { transport, api } = offlineCall(true)
        await api.with({ cache: false }).call('thing.create')
        await api.call('thing.create')
        expect(transport.sent.length).toBe(2)
        expect(transport.sent[0]!.nonce).toBeString()
    })
})

describe('error handlers', () => {
    const handled = (outcome: Error) => {
        const { transport } = offlineCall()
        transport.outcomes = [outcome]
        const seen: ApiError[] = []
        const api = new ApiClient<Methods>({ transport, errorHandlers: { REFUSED: e => seen.push(e), THING_NOT_FOUND: e => seen.push(e) } })
        void api.call('thing.create').catch(() => undefined)
        return seen
    }

    test.each([
        ['a code of its own', new ApiError('REFUSED', { why: 'no' })],
        ['a not-found, by its code', new NotFoundError('THING_NOT_FOUND')]
    ])('take %s, the error itself in hand', async (_name, error) => {
        const seen = handled(error)
        await tick()
        expect(seen).toEqual([error])
        expect(useExceptionState().exception).toBe(false)
    })

    test('take only what the server answered with: an error whose message reads like a code is not one', async () => {
        const seen = handled(new NetworkError('REFUSED'))
        await tick()
        expect(seen).toEqual([])
        expect(useExceptionState().exception).toBe('network')
    })
})

describe('the toast', () => {
    test.each([
        ['a not-found is only acknowledged', new NotFoundError(), 'notfound'],
        ['a socket gone is a network failure', new NetworkError('gone'), 'network'],
        ['any other answer may be tried again', new ApiError('REFUSED'), 'other']
    ] as const)('%s', (_name, error, kind) => {
        expect(classifyException(error)).toBe(kind)
    })
})

describe('bubbled', () => {
    test('throws each code’s error back, as given or as turned', () => {
        const refused = new ApiError('REFUSED')
        expect(Object.keys(bubbled(['REFUSED', 'GONE']))).toEqual(['REFUSED', 'GONE'])
        expect(() => bubbled(['REFUSED']).REFUSED!(refused)).toThrow(refused)
        expect(() => bubbled(['REFUSED'], e => new Error(`told ${e.code}`)).REFUSED!(refused)).toThrow('told REFUSED')
    })
})
