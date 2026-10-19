import { describe, expect, test } from 'bun:test'
import { ResponseCache } from '../src/main'

const makeCache = (): ResponseCache =>
    new ResponseCache({
        ttl: 300,
        endpoints: {
            'thing.search': true,
            'user.profile': 0.02
        }
    })

describe('ResponseCache', () => {
    test('ttlFor reflects config', () => {
        const cache = makeCache()
        expect(cache.ttlFor('thing.search')).toBe(300)
        expect(cache.ttlFor('user.profile')).toBe(0.02)
        expect(cache.ttlFor('other')).toBe(false)
    })

    test('set/get keyed by method+args; uncached methods are no-ops', () => {
        const cache = makeCache()
        cache.set('thing.search', [{ q: 'a' }], 'result-a')
        expect(cache.get('thing.search', [{ q: 'a' }])).toEqual({ hit: true, data: 'result-a' })
        expect(cache.get('thing.search', [{ q: 'b' }]).hit).toBe(false)

        cache.set('other', [], 'x')
        expect(cache.get('other', []).hit).toBe(false)
    })

    test('entries expire after their TTL', async () => {
        const cache = makeCache()
        cache.set('user.profile', ['u1'], 'profile')
        expect(cache.get('user.profile', ['u1']).hit).toBe(true)
        await new Promise(resolve => setTimeout(resolve, 30))
        expect(cache.get('user.profile', ['u1']).hit).toBe(false)
    })

    test('invalidate: exact args vs whole method family', () => {
        const cache = makeCache()
        cache.set('thing.search', ['a'], 1)
        cache.set('thing.search', ['b'], 2)
        cache.invalidate('thing.search', 'a')
        expect(cache.get('thing.search', ['a']).hit).toBe(false)
        expect(cache.get('thing.search', ['b']).hit).toBe(true)
        cache.invalidate('thing.search')
        expect(cache.get('thing.search', ['b']).hit).toBe(false)
    })

    test('clear drops every entry, whatever its method', () => {
        const cache = makeCache()
        cache.set('thing.search', ['a'], 1)
        cache.set('user.profile', ['u1'], 2)
        cache.clear()
        expect(cache.get('thing.search', ['a']).hit).toBe(false)
        expect(cache.get('user.profile', ['u1']).hit).toBe(false)
        // Still usable afterwards: clearing empties the entries, not the config.
        cache.set('thing.search', ['a'], 3)
        expect(cache.get('thing.search', ['a'])).toEqual({ hit: true, data: 3 })
    })

    test('a scope keeps answers to the same call apart', () => {
        let locale = 'en-GB'
        const cache = new ResponseCache({ ttl: 300, endpoints: { 'taxonomy.tree': true } }, () => locale)
        cache.set('taxonomy.tree', ['category'], 'Music, Film & Books')

        locale = 'he'
        // The reply was written in the language it was asked in, so it cannot
        // answer the same question asked in another.
        expect(cache.get('taxonomy.tree', ['category']).hit).toBe(false)
        cache.set('taxonomy.tree', ['category'], 'מוזיקה, סרטים וספרים')
        expect(cache.get('taxonomy.tree', ['category'])).toEqual({ hit: true, data: 'מוזיקה, סרטים וספרים' })

        // And the first one is still there to switch back to.
        locale = 'en-GB'
        expect(cache.get('taxonomy.tree', ['category'])).toEqual({ hit: true, data: 'Music, Film & Books' })
    })

    test('invalidate reaches every scope: the data changed, not the language', () => {
        let locale = 'en-GB'
        const cache = new ResponseCache({ ttl: 300, endpoints: { 'thing.get': true, 'thing.search': true } }, () => locale)
        cache.set('thing.get', ['T1'], 'english')
        cache.set('thing.search', [{ q: 'a' }], 'english results')
        locale = 'he'
        cache.set('thing.get', ['T1'], 'hebrew')
        cache.set('thing.search', [{ q: 'a' }], 'hebrew results')

        // By args, from whichever scope happens to be current…
        cache.invalidate('thing.get', 'T1')
        expect(cache.get('thing.get', ['T1']).hit).toBe(false)
        locale = 'en-GB'
        expect(cache.get('thing.get', ['T1']).hit).toBe(false)

        // …and by method, the whole family.
        cache.invalidate('thing.search')
        expect(cache.get('thing.search', [{ q: 'a' }]).hit).toBe(false)
        locale = 'he'
        expect(cache.get('thing.search', [{ q: 'a' }]).hit).toBe(false)
    })
})
