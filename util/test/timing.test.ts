import { describe, expect, test } from 'bun:test'
import { createDeferred, type Deferred } from '../src/misc'
import { Debouncer, Delayed, TimeOutPromise, coalesced } from '../src/timing'

interface Held {
    pull: () => Promise<number>
    // How many runs have started.
    started: () => number
    // Ends the run that started `index`-th (from 0) with this value or failure.
    settle: (index: number, outcome: number | Error) => void
}

describe('coalesced', () => {
    // A run per call to `run`, each held until the test settles it, numbered in
    // the order they started.
    const held = (): Held => {
        const runs: Deferred<number>[] = []
        const pull = coalesced(() => {
            const run = createDeferred<number>()
            runs.push(run)
            return run.promise
        })
        return {
            pull,
            started: () => runs.length,
            settle: (index, outcome) => (outcome instanceof Error ? runs[index]!.reject(outcome) : runs[index]!.resolve(outcome))
        }
    }

    test('a pull with nothing running starts a run and settles with it', async () => {
        const { pull, started, settle } = held()
        const answer = pull()
        expect(started()).toBe(1)
        settle(0, 1)
        expect(await answer).toBe(1)
    })

    test('pulls during a run all settle with the one run queued behind it, none with the running one', async () => {
        const { pull, started, settle } = held()
        const first = pull()
        const during = [pull(), pull(), pull()]
        expect(started()).toBe(1)

        settle(0, 1)
        expect(await first).toBe(1)
        expect(started()).toBe(2)
        settle(1, 2)
        expect(await Promise.all(during)).toEqual([2, 2, 2])

        // Idle again: the next pull starts a run of its own.
        const after = pull()
        expect(started()).toBe(3)
        settle(2, 3)
        expect(await after).toBe(3)
    })

    test('a rejected run rejects the pulls it covers and frees the slot for the one queued', async () => {
        const { pull, started, settle } = held()
        const first = pull()
        const queued = pull()
        settle(0, new Error('read failed'))
        await expect(first).rejects.toThrow('read failed')
        expect(started()).toBe(2)
        settle(1, 2)
        expect(await queued).toBe(2)
    })

    test('a run that throws synchronously is a rejection like any other, and frees the slot', async () => {
        let runs = 0
        const pull = coalesced(() => {
            runs++
            if (runs === 1) throw new Error('read failed')
            return runs
        })
        await expect(pull()).rejects.toThrow('read failed')
        expect(await pull()).toBe(2)
    })
})

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
