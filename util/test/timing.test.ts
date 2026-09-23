import { describe, expect, test } from 'bun:test'
import { Debouncer, Delayed, TimeOutPromise } from '../src/timing'

describe('TimeOutPromise', () => {
    test('resolves after the delay', async () => {
        const p = new TimeOutPromise(5)
        await p.promise
        expect(p.isResolved).toBe(true)
    })

    test('cancel rejects a pending wait', async () => {
        const p = new TimeOutPromise(1000)
        queueMicrotask(() => p.cancel())
        await expect(p.promise).rejects.toBeUndefined()
    })
})

describe('Debouncer', () => {
    test('only the last fire proceeds', async () => {
        const d = new Debouncer(10)
        const [first, second] = await Promise.all([d.fire(), d.fire()])
        expect(first()).toBe(false)
        expect(second()).toBe(true)
    })

    test('cancel invalidates pending fires', async () => {
        const d = new Debouncer(10)
        const firePromise = d.fire()
        d.cancel()
        const proceed = await firePromise
        expect(proceed()).toBe(false)
    })
})

describe('Delayed', () => {
    test('same name replaces the pending timeout', async () => {
        const delayed = new Delayed()
        const calls: string[] = []
        delayed.go('k', () => calls.push('first'), 10)
        delayed.go('k', () => calls.push('second'), 10)
        await TimeOutPromise.wait(30)
        expect(calls).toEqual(['second'])
    })

    test('cancel prevents execution', async () => {
        const delayed = new Delayed()
        let ran = false
        delayed.go('k', () => {
            ran = true
        }, 10)
        delayed.cancel('k')
        await TimeOutPromise.wait(30)
        expect(ran).toBe(false)
    })
})
