import { afterEach, describe, expect, test } from 'bun:test'
import { applyMergePatch } from '../src/document'
import { documentOf, mergePatchBetween, type JsonObject } from '../src/main'

// The cases, each a document before, the patch and the document after, read both ways:
// the patch the server computes between the two, and the document the client gets by
// applying it.
const cases: [name: string, prev: JsonObject, patch: JsonObject, next: JsonObject][] = [
    ['an added member is carried whole', { a: 1 }, { gate: { State: 'online' } }, { a: 1, gate: { State: 'online' } }],
    ['a removed member is null', { gate: { State: 'online' }, porch: 'x' }, { gate: null }, { porch: 'x' }],
    ['a changed scalar is carried', { load: 798, mode: 3 }, { load: 801 }, { load: 801, mode: 3 }],
    ['objects within objects are patched in turn', { gate: { State: 'online', Error: '' }, porch: { State: 'online' } }, { gate: { State: 'offline', Error: 'refused' } }, { gate: { State: 'offline', Error: 'refused' }, porch: { State: 'online' } }],
    ['a member gone from an object within is null there', { gate: { State: 'online', Streams: { main: 'H.265' } } }, { gate: { Streams: null } }, { gate: { State: 'online' } }],
    ['an object replaced by a scalar, and a scalar by an object, are carried whole', { a: { b: 1 }, c: 2 }, { a: 1, c: { d: 2 } }, { a: 1, c: { d: 2 } }],
    ['a changed array is carried whole', { d: [1, 2] }, { d: [1, 3] }, { d: [1, 3] }]
]

// A copy of a case's document, so applying a patch leaves the table as it was.
const copy = (doc: JsonObject): JsonObject => JSON.parse(JSON.stringify(doc)) as JsonObject

describe('mergePatchBetween', () => {
    test.each(cases)('%s', (_, prev, patch, next) => {
        expect(mergePatchBetween(prev, next)).toEqual(patch)
    })

    test('equal documents patch nothing, whatever the order of their members', () => {
        expect(
            mergePatchBetween(
                {
                    a: 1,
                    b: { c: 'x' },
                    d: [1, 2]
                },
                {
                    b: { c: 'x' },
                    d: [1, 2],
                    a: 1
                }
            )
        ).toBeNull()
    })
})

describe('applyMergePatch', () => {
    test.each(cases)('%s', (_, prev, patch, next) => {
        const target = copy(prev)
        applyMergePatch(target, patch)
        expect(target).toEqual(next)
    })

    // RFC 7386: an object where there was none is the patch applied to an empty one, so
    // its own nulls remove nothing and land as nothing.
    test('an object put where there was none drops its nulls', () => {
        const target: JsonObject = { a: 2 }
        applyMergePatch(target, { a: { b: 1, c: null } })
        expect(target).toEqual({ a: { b: 1 } })
    })

    test('patches in place, keeping the objects it patches', () => {
        const gate = { State: 'online' }
        const target: JsonObject = { gate }
        applyMergePatch(target, { gate: { State: 'offline' } })
        expect(target.gate).toBe(gate)
        expect(gate.State).toBe('offline')
    })
})

describe('an id that names something every object inherits', () => {
    afterEach(() => {
        // Clean up after a failure, so one does not leak into every later test.
        delete (Object.prototype as JsonObject).State
    })

    test('is a member like any other, and reaches no prototype', () => {
        const frame = JSON.parse('{"__proto__": {"State": "online"}, "toString": {"State": "offline"}}') as JsonObject
        const target: JsonObject = {}
        applyMergePatch(target, frame)
        expect(({} as JsonObject).State).toBeUndefined()
        expect(Object.getPrototypeOf(target)).toBe(Object.prototype)
        expect(Object.keys(target)).toEqual(['__proto__', 'toString'])
        expect(Object.getOwnPropertyDescriptor(target, '__proto__')?.value).toEqual({ State: 'online' })

        applyMergePatch(target, JSON.parse('{"__proto__": {"State": "offline"}}') as JsonObject)
        expect(Object.getOwnPropertyDescriptor(target, '__proto__')?.value).toEqual({ State: 'offline' })
        expect(({} as JsonObject).State).toBeUndefined()
    })

    test('is diffed as one of the document’s own', () => {
        const prev = JSON.parse('{"__proto__": {"State": "online"}}') as JsonObject
        const next = JSON.parse('{"__proto__": {"State": "offline"}}') as JsonObject
        const patch = mergePatchBetween(prev, next)!
        expect(Object.getOwnPropertyDescriptor(patch, '__proto__')?.value).toEqual({ State: 'offline' })
        expect(mergePatchBetween({}, JSON.parse('{"toString": 1}') as JsonObject)).toEqual(JSON.parse('{"toString": 1}') as JsonObject)
    })
})

describe('documentOf', () => {
    test('is the value as JSON, without its null members', () => {
        expect(
            documentOf('cameras', {
                gate: {
                    State: 'online',
                    Error: null,
                    at: new Date(0)
                },
                porch: null,
                gone: undefined
            })
        ).toEqual({ gate: { State: 'online', at: '1970-01-01T00:00:00.000Z' } })
    })

    test('refuses a value that is no JSON object', () => {
        expect(() => documentOf('cameras', [])).toThrow('document cameras is not a JSON object')
    })
})
