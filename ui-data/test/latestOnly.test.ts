import { describe, expect, test } from 'bun:test'
import { latestOnly } from '../src/latestOnly'

describe('latestOnly', () => {
    test('a call is the latest until the next one starts', () => {
        const start = latestOnly()
        const first = start()
        expect(first()).toBe(true)
        const second = start()
        expect([first(), second()]).toEqual([false, true])
    })

    test('the calls of one source do not supersede those of another', () => {
        const start = latestOnly()
        const other = latestOnly()
        const call = start()
        other()
        expect(call()).toBe(true)
    })
})
