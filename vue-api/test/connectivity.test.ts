import { beforeEach, describe, expect, test } from 'bun:test'
import { NetworkError } from '@greendrake/util'
import { useConnectivity } from '../src/connectivity'
import { retryWhenOnline } from '../src/retryWhenOnline'
import { FakeSource, setDeviceOnline, withSource } from './support'

beforeEach(() => {
    setDeviceOnline(true)
})

describe('useConnectivity', () => {
    test('starts out neither offline nor confirmed, so the first handshake cannot flash', () => {
        const source = new FakeSource()
        withSource(source)
        const connectivity = useConnectivity()
        // The eager connect at boot has not had its chance yet: the socket is
        // genuinely not connected, and saying "offline" for that would put the
        // offline surfaces on screen at every start.
        expect(source.connected).toBe(false)
        expect(connectivity.offline).toBe(false)
        expect(connectivity.confirmedOffline).toBe(false)
    })

    test('the first disconnect goes offline, the second confirms it', () => {
        const source = new FakeSource()
        withSource(source)
        const connectivity = useConnectivity()

        source.report(false)
        expect(connectivity.offline).toBe(true)
        // One failure is not yet an outage — a rolling deploy looks exactly
        // like this and reconnects on the next attempt.
        expect(connectivity.confirmedOffline).toBe(false)

        source.report(false)
        expect(connectivity.confirmedOffline).toBe(true)
    })

    test('a reconnect clears both flags', () => {
        const source = new FakeSource()
        withSource(source)
        const connectivity = useConnectivity()
        source.report(false)
        source.report(false)
        source.report(true)
        expect(connectivity.offline).toBe(false)
        expect(connectivity.confirmedOffline).toBe(false)
    })

    test('a drop that reconnects never confirms, even if it drops again later', () => {
        const source = new FakeSource()
        withSource(source)
        const connectivity = useConnectivity()
        source.report(false)
        source.report(true)
        source.report(false)
        // Consecutive is the whole point: two failures either side of a
        // success are two separate blips, not one outage.
        expect(connectivity.confirmedOffline).toBe(false)
    })

    test('whenOnline resolves immediately while connected', async () => {
        withSource(new FakeSource())
        await expect(useConnectivity().whenOnline()).resolves.toBeUndefined()
    })

    test('whenOnline settles every waiter on the next connect', async () => {
        const source = new FakeSource()
        withSource(source)
        const connectivity = useConnectivity()
        source.report(false)

        const settled: number[] = []
        const waiters = [0, 1, 2].map(n => connectivity.whenOnline().then(() => settled.push(n)))
        await Promise.resolve()
        expect(settled).toEqual([])

        source.report(true)
        await Promise.all(waiters)
        expect(settled).toEqual([0, 1, 2])
    })

    test('with no source the device signal alone drives both flags', () => {
        setDeviceOnline(false)
        withSource(undefined)
        const connectivity = useConnectivity()
        // An HTTP-only app has no oracle and no attempts to count, so the
        // device's claim is both the report and its confirmation.
        expect(connectivity.offline).toBe(true)
        expect(connectivity.confirmedOffline).toBe(true)
        expect(connectivity.deviceOffline).toBe(true)

        setDeviceOnline(true)
        dispatchEvent(new Event('online'))
        expect(connectivity.offline).toBe(false)
        expect(connectivity.confirmedOffline).toBe(false)
    })

    test('with a source the device signal only picks the wording', () => {
        const source = new FakeSource()
        withSource(source)
        const connectivity = useConnectivity()

        setDeviceOnline(false)
        dispatchEvent(new Event('offline'))
        expect(connectivity.deviceOffline).toBe(true)
        // The device saying it is offline is not evidence that calls cannot
        // succeed — only the transport is.
        expect(connectivity.offline).toBe(false)
    })
})

describe('retryWhenOnline', () => {
    test('re-runs the attempt when the connection returns', async () => {
        const source = new FakeSource()
        withSource(source)
        // Instantiated before the report: the store subscribes when it
        // materialises, so anything reported earlier is not its to see.
        useConnectivity()
        source.report(false)

        let attempts = 0
        const run = retryWhenOnline(() => {
            attempts++
            if (attempts === 1) {
                return Promise.reject(new NetworkError(new Error('down')))
            }
            return Promise.resolve('content')
        })
        await Promise.resolve()
        expect(attempts).toBe(1)

        source.report(true)
        expect(await run).toBe('content')
        expect(attempts).toBe(2)
    })

    test('propagates a non-network failure without waiting', async () => {
        const source = new FakeSource()
        withSource(source)
        source.report(false)
        await expect(retryWhenOnline(() => Promise.reject(new Error('not found')))).rejects.toThrow('not found')
    })

    test('propagates a network failure the oracle does not corroborate', async () => {
        withSource(new FakeSource())
        // Nothing reported a disconnect, so this is the site being unreachable
        // rather than an outage to wait out — the caller must hear about it.
        await expect(retryWhenOnline(() => Promise.reject(new NetworkError(new Error('down'))))).rejects.toBeInstanceOf(NetworkError)
    })
})
