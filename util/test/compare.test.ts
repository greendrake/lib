import { describe, expect, test } from 'bun:test'
import { byPreset, byProp, composeComparators } from '../src/compare'

interface Item {
    kind: string
    n: number
}

describe('comparators', () => {
    test('byProp ascending and descending', () => {
        const items: Item[] = [
            { kind: 'a', n: 2 },
            { kind: 'b', n: 1 }
        ]
        expect([...items].sort(byProp('n')).map(i => i.n)).toEqual([1, 2])
        expect([...items].sort(byProp('n', true)).map(i => i.n)).toEqual([2, 1])
    })

    test('byPreset orders by list position, unknowns tie', () => {
        const items: Item[] = [
            { kind: 'Solar', n: 1 },
            { kind: 'grid', n: 2 },
            { kind: 'mystery', n: 3 }
        ]
        const sorted = [...items].sort(byPreset('kind', ['grid', 'solar']))
        expect(sorted.map(i => i.kind)).toEqual(['grid', 'Solar', 'mystery'])
    })

    test('composeComparators breaks ties in order', () => {
        const items: Item[] = [
            { kind: 'b', n: 1 },
            { kind: 'a', n: 1 },
            { kind: 'c', n: 0 }
        ]
        const sorted = [...items].sort(composeComparators<Item>(byProp('n'), byProp('kind')))
        expect(sorted.map(i => i.kind)).toEqual(['c', 'a', 'b'])
    })
})
