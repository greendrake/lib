import { describe, expect, test } from 'bun:test'
import { Retrier, RetryStopped } from '../src/Retrier'

describe('Retrier', () => {
    test('resolves on first success', async () => {
        const r = new Retrier({ attempt: async () => 42 })
        expect(await r.run()).toBe(42)
    })

    test('retries until success, emitting status', async () => {
        let attempts = 0
        const r = new Retrier({
            attempt: async () => {
                attempts++
                if (attempts < 3) throw new Error('boom')
                return 'ok'
            },
            delaySeconds: 0.01
        })
        const events: string[] = []
        r.on('trying', () => events.push('trying'))
        r.on('waiting', s => events.push(`waiting:${s}`))
        expect(await r.run()).toBe('ok')
        expect(attempts).toBe(3)
        expect(events).toEqual(['trying', 'waiting:1', 'trying', 'waiting:1', 'trying'])
    })

    test('fatal errors rethrow via shouldRetry gate', async () => {
        const fatal = new Error('fatal')
        const r = new Retrier({
            attempt: async () => {
                throw fatal
            },
            shouldRetry: () => false
        })
        await expect(r.run()).rejects.toBe(fatal)
    })

    test('unaccepted results retry', async () => {
        let attempts = 0
        const r = new Retrier({
            attempt: async () => ++attempts,
            accept: result => result >= 2,
            delaySeconds: 0.01
        })
        expect(await r.run()).toBe(2)
    })

    test('stop() rejects the run with RetryStopped', async () => {
        const r = new Retrier({
            attempt: async () => {
                throw new Error('always')
            },
            delaySeconds: 60
        })
        const run = r.run()
        r.stop()
        expect(r.stopped).toBe(true)
        await expect(run).rejects.toBeInstanceOf(RetryStopped)
    })

    test('backoff schedule receives the retry index', async () => {
        const delays: number[] = []
        let attempts = 0
        const r = new Retrier({
            attempt: async () => {
                attempts++
                if (attempts < 3) throw new Error('boom')
                return true
            },
            delaySeconds: i => {
                delays.push(i)
                return 0.01
            }
        })
        await r.run()
        expect(delays).toEqual([0, 1])
    })
})
