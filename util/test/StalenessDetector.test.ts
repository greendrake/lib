import { describe, expect, test } from 'bun:test'
import { StalenessDetector } from '../src/StalenessDetector'
import { TimeOutPromise } from '../src/timing'

describe('StalenessDetector', () => {
    test('renew keeps fresh; silence goes stale; transitions emit once', async () => {
        const d = new StalenessDetector(20)
        const events: string[] = []
        d.on('fresh', () => events.push('fresh'))
        d.on('stale', () => events.push('stale'))

        expect(d.isFresh).toBe(false)
        d.renew()
        expect(d.isFresh).toBe(true)
        await TimeOutPromise.wait(10)
        d.renew()
        await TimeOutPromise.wait(10)
        expect(d.isFresh).toBe(true)
        await TimeOutPromise.wait(30)
        expect(d.isFresh).toBe(false)
        expect(events).toEqual(['fresh', 'stale'])
        d.dispose()
    })
})
