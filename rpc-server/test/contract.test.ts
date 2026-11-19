import { describe, expect, test } from 'bun:test'
import { createDispatcher } from '../src/main'
import type { CallContext, MethodDefs } from '../src/main'
import { schema } from './standardSchema'

// What a table is held to by its contract is the compiler's to check: each
// `@ts-expect-error` below marks a table the check has to refuse, and the
// package's type-check fails should one of them ever compile.

interface Thing {
    id: string
    name: string
}

interface Contract {
    'thing.get': (ref: { id: string }) => Thing
    'thing.count': () => number
}

const ref = schema<{ id: string }>(value => {
    const id = (value as { id?: unknown } | undefined)?.id
    return typeof id === 'string' ? { value: { id } } : { issues: [{ message: 'an id, please', path: ['id'] }] }
})

const named = schema<{ name: string }>(value => {
    const name = (value as { name?: unknown } | undefined)?.name
    return typeof name === 'string' ? { value: { name } } : { issues: [{ message: 'a name, please', path: ['name'] }] }
})

// The handler's argument comes from the contract, and its result may be the
// contract's or a promise of it.
const served: MethodDefs<Contract> = {
    'thing.get': {
        auth: 'none',
        args: ref,
        handler: (_ctx, { id }) => ({ id, name: `thing ${id}` })
    },
    'thing.count': { auth: 'none', handler: () => Promise.resolve(2) }
}

const context: CallContext<never> = {
    principal: null,
    clientIp: '127.0.0.1',
    signal: AbortSignal.any([]),
    topics: null
}

describe('a table typed by its contract', () => {
    test('is a dispatch table, and answers as the contract says', async () => {
        const dispatcher = createDispatcher(served, {})
        expect(await dispatcher.dispatch({ method: 'thing.get', arguments: [{ id: 't1' }] }, context)).toEqual({ success: true, data: { id: 't1', name: 'thing t1' } })
        expect(await dispatcher.dispatch({ method: 'thing.count' }, context)).toEqual({ success: true, data: 2 })
    })

    test('defines every method its contract declares', () => {
        // @ts-expect-error — 'thing.count' is declared, and not defined
        const missing: MethodDefs<Contract> = { 'thing.get': served['thing.get'] }
        expect(Object.keys(missing)).toEqual(['thing.get'])
    })

    test('validates the argument its contract declares', () => {
        const wrong: MethodDefs<Contract> = {
            ...served,
            'thing.get': {
                auth: 'none',
                // @ts-expect-error — the schema produces a name, and the contract declares an id
                args: named,
                handler: (_ctx, args) => ({ id: 'x', name: String(args) })
            }
        }
        expect(Object.keys(wrong)).toEqual(['thing.get', 'thing.count'])
    })

    test('returns the result its contract declares', () => {
        const wrong: MethodDefs<Contract> = {
            ...served,
            // @ts-expect-error — a count is a number
            'thing.count': { auth: 'none', handler: () => 'two' }
        }
        expect(Object.keys(wrong)).toEqual(['thing.get', 'thing.count'])
    })

    test('has a schema for every argument its contract declares', () => {
        const unchecked: MethodDefs<Contract> = {
            ...served,
            // @ts-expect-error — 'thing.get' declares an argument, and nothing validates it
            'thing.get': { auth: 'none', handler: (_ctx, { id }) => ({ id, name: id }) }
        }
        expect(Object.keys(unchecked)).toEqual(['thing.get', 'thing.count'])
    })

    test('takes no argument its contract does not declare', () => {
        const extra: MethodDefs<Contract> = {
            ...served,
            'thing.count': {
                auth: 'none',
                // @ts-expect-error — 'thing.count' declares no argument
                args: ref,
                handler: () => 2
            }
        }
        expect(Object.keys(extra)).toEqual(['thing.get', 'thing.count'])
    })

    test('can define a method only where its contract declares one required parameter or none', () => {
        interface Wide {
            'pair.add': (a: number, b: number) => number
            'thing.find': (query?: { name: string }) => Thing[]
            'thing.tag': (...tags: string[]) => void
        }
        const twoValidated: Pick<MethodDefs<Wide>, 'pair.add'> = {
            // @ts-expect-error — a method takes one argument at most
            'pair.add': {
                auth: 'none',
                args: schema<number>(value => ({ value: Number(value) })),
                handler: () => 0
            }
        }
        const twoUnchecked: Pick<MethodDefs<Wide>, 'pair.add'> = {
            // @ts-expect-error — a method takes one argument at most, with or without a schema
            'pair.add': { auth: 'none', handler: () => 0 }
        }
        const optional: Pick<MethodDefs<Wide>, 'thing.find'> = {
            // @ts-expect-error — an optional value is a field of the argument
            'thing.find': {
                auth: 'none',
                args: named,
                handler: () => []
            }
        }
        const rest: Pick<MethodDefs<Wide>, 'thing.tag'> = {
            // @ts-expect-error — a method takes one argument at most, however many a rest parameter gathers
            'thing.tag': {
                auth: 'none',
                args: schema<string>(value => ({ value: String(value) })),
                handler: () => undefined
            }
        }
        expect([twoValidated, twoUnchecked, optional, rest].flatMap(Object.keys)).toEqual(['pair.add', 'pair.add', 'thing.find', 'thing.tag'])
    })
})
