import { describe, expect, test } from 'bun:test'
import { ResponseCache } from '../src/main'

const makeCache = (): ResponseCache =>
    new ResponseCache({
        ttl: 300,
        endpoints: {
            'search.listings': true,
            'user.profile': 0.02
        }
    })

describe('ResponseCache', () => {
    test('ttlFor reflects config', () => {
        const cache = makeCache()
        expect(cache.ttlFor('search.listings')).toBe(300)
        expect(cache.ttlFor('user.profile')).toBe(0.02)
        expect(cache.ttlFor('other')).toBe(false)
    })

    test('set/get keyed by method+args; uncached methods are no-ops', () => {
        const cache = makeCache()
        cache.set('search.listings', [{ q: 'a' }], 'result-a')
        expect(cache.get('search.listings', [{ q: 'a' }])).toEqual({ hit: true, data: 'result-a' })
        expect(cache.get('search.listings', [{ q: 'b' }]).hit).toBe(false)

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
        cache.set('search.listings', ['a'], 1)
        cache.set('search.listings', ['b'], 2)
        cache.invalidate('search.listings', 'a')
        expect(cache.get('search.listings', ['a']).hit).toBe(false)
        expect(cache.get('search.listings', ['b']).hit).toBe(true)
        cache.invalidate('search.listings')
        expect(cache.get('search.listings', ['b']).hit).toBe(false)
    })

    test('clear drops every entry, whatever its method', () => {
        const cache = makeCache()
        cache.set('search.listings', ['a'], 1)
        cache.set('user.profile', ['u1'], 2)
        cache.clear()
        expect(cache.get('search.listings', ['a']).hit).toBe(false)
        expect(cache.get('user.profile', ['u1']).hit).toBe(false)
        // Still usable afterwards: clearing empties the entries, not the config.
        cache.set('search.listings', ['a'], 3)
        expect(cache.get('search.listings', ['a'])).toEqual({ hit: true, data: 3 })
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
        const cache = new ResponseCache({ ttl: 300, endpoints: { 'listing.get': true, 'search.listings': true } }, () => locale)
        cache.set('listing.get', ['L1'], 'english')
        cache.set('search.listings', [{ q: 'a' }], 'english results')
        locale = 'he'
        cache.set('listing.get', ['L1'], 'hebrew')
        cache.set('search.listings', [{ q: 'a' }], 'hebrew results')

        // By args, from whichever scope happens to be current…
        cache.invalidate('listing.get', 'L1')
        expect(cache.get('listing.get', ['L1']).hit).toBe(false)
        locale = 'en-GB'
        expect(cache.get('listing.get', ['L1']).hit).toBe(false)

        // …and by method, the whole family.
        cache.invalidate('search.listings')
        expect(cache.get('search.listings', [{ q: 'a' }]).hit).toBe(false)
        locale = 'he'
        expect(cache.get('search.listings', [{ q: 'a' }]).hit).toBe(false)
    })

    test('prime seeds only configured endpoints', () => {
        const cache = makeCache()
        cache.prime('search.listings', ['q'], 'pushed')
        cache.prime('unconfigured', ['q'], 'pushed')
        expect(cache.get('search.listings', ['q'])).toEqual({ hit: true, data: 'pushed' })
        expect(cache.get('unconfigured', ['q']).hit).toBe(false)
    })
})
