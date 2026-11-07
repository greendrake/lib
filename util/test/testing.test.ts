import { afterAll, beforeAll, describe, expect, test } from 'bun:test'
import { stubGlobals } from '../src/testing'

// Two names: one the global object holds a getter for — which no assignment
// could put back — and one it holds nothing for.
const HELD = '__stubGlobalsHeld'
const ABSENT = '__stubGlobalsAbsent'
const held: PropertyDescriptor = {
    get: () => 'held',
    enumerable: true,
    configurable: true
}

beforeAll(() => {
    Object.defineProperty(globalThis, HELD, held)
})

afterAll(() => {
    Reflect.deleteProperty(globalThis, HELD)
})

let computed = 0

describe('stubGlobals', () => {
    stubGlobals(() => {
        computed++
        return { [HELD]: 'stubbed', [ABSENT]: 'stubbed too' }
    })
    const computedAtCall = computed

    test('computes the values when it puts them in, not when called', () => {
        expect(computedAtCall).toBe(0)
        expect(computed).toBe(1)
    })

    test('puts each value in for the tests of its scope', () => {
        expect(Reflect.get(globalThis, HELD)).toBe('stubbed')
        expect(Reflect.get(globalThis, ABSENT)).toBe('stubbed too')
    })
})

test('puts back the own property a name had, and removes a name that had none', () => {
    expect(Object.getOwnPropertyDescriptor(globalThis, HELD)).toEqual(held)
    expect(Object.hasOwn(globalThis, ABSENT)).toBe(false)
})
