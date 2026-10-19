import { describe, expect, test } from 'bun:test'
import { ApiError, createDispatcher, memoryNonceStore, method, trustProxy } from '../src/main'
import type { AuthResolver, CallContext, Methods } from '../src/main'
import { schema } from './standardSchema'

// The application's principal, as a resolver of its own would name it.
interface Caller {
    id: string
    admin: boolean
}

const USER: Caller = { id: 'u1', admin: false }
const ADMIN: Caller = { id: 'root', admin: true }

const adminOnly = (caller: Caller | null): true | string => (caller?.admin ? true : 'ADMIN_REQUIRED')

// Never asked: the dispatcher is handed principals directly here.
const RESOLVER: AuthResolver<Caller> = { resolveToken: () => Promise.resolve(null) }

const positive = schema<{ amount: number }>(value => {
    const amount = (value as { amount?: unknown } | undefined)?.amount
    return typeof amount === 'number' && amount > 0 ? { value: { amount } } : { issues: [{ message: 'must be a positive number', path: ['amount'] }] }
})

const methods: Methods<Caller> = {
    open: { auth: 'none', handler: () => 'anyone' },
    mine: { auth: 'required', handler: ctx => ctx.principal?.id },
    admin: {
        auth: 'required',
        access: adminOnly,
        handler: () => 'root only'
    },
    // A policy over anonymous callers too: an `auth: 'none'` method's access
    // sees null for them.
    'strangers.only': {
        auth: 'none',
        access: caller => (caller === null ? true : 'ALREADY_SIGNED_IN'),
        handler: () => 'welcome'
    },
    charge: method({
        auth: 'none',
        args: positive,
        handler: (_ctx, args) => args.amount * 2
    }),
    refuse: {
        auth: 'none',
        handler: () => {
            throw new ApiError('NOT_ENOUGH_ROPE', { short_by: 3 })
        }
    },
    fault: {
        auth: 'none',
        handler: () => {
            throw new Error('a bug')
        }
    }
}

const context = <P>(principal: P | null = null): CallContext<P> => ({
    principal,
    clientIp: '127.0.0.1',
    signal: AbortSignal.any([]),
    topics: null
})

describe('dispatch', () => {
    const dispatcher = createDispatcher(methods, { auth: RESOLVER })

    test('an unknown method is not a fault', async () => {
        expect(await dispatcher.dispatch({ method: 'nope' }, context())).toEqual({
            success: false,
            error: 'UNKNOWN_METHOD',
            error_details: undefined
        })
    })

    test('inherited property names are not methods', async () => {
        expect(await dispatcher.dispatch({ method: 'toString' }, context())).toMatchObject({ error: 'UNKNOWN_METHOD' })
    })

    test.each([
        ['none, anonymous', 'open', null, { success: true, data: 'anyone' }],
        ['required, anonymous', 'mine', null, { success: false, error: 'AUTH_REQUIRED' }],
        ['required, signed in', 'mine', USER, { success: true, data: 'u1' }],
        ['a principal is asked of before access is', 'admin', null, { success: false, error: 'AUTH_REQUIRED' }],
        ['access refusing', 'admin', USER, { success: false, error: 'ADMIN_REQUIRED' }],
        ['access admitting', 'admin', ADMIN, { success: true, data: 'root only' }],
        ['access over an anonymous caller', 'strangers.only', null, { success: true, data: 'welcome' }],
        ['access over a signed-in one', 'strangers.only', USER, { success: false, error: 'ALREADY_SIGNED_IN' }]
    ])('auth rule: %s', async (_name, method, principal, expected) => {
        expect(await dispatcher.dispatch({ method }, context(principal))).toMatchObject(expected)
    })

    test('an ApiError is the handler saying what happened', async () => {
        expect(await dispatcher.dispatch({ method: 'refuse' }, context())).toEqual({
            success: false,
            error: 'NOT_ENOUGH_ROPE',
            error_details: { short_by: 3 }
        })
    })

    test('anything else a handler throws is INTERNAL_ERROR, and a fault reported', async () => {
        const faults: unknown[][] = []
        const reporting = createDispatcher(methods, { auth: RESOLVER, onFault: (method, error) => faults.push([method, error]) })
        expect(await reporting.dispatch({ method: 'fault' }, context())).toMatchObject({ error: 'INTERNAL_ERROR' })
        expect(faults).toEqual([['fault', new Error('a bug')]])
    })

    test('a fault goes to console.error unless the application says where', async () => {
        const logged: unknown[] = []
        const original = console.error
        console.error = (...args: unknown[]) => logged.push(args)
        try {
            expect(await dispatcher.dispatch({ method: 'fault' }, context())).toMatchObject({ error: 'INTERNAL_ERROR' })
        } finally {
            console.error = original
        }
        expect(logged).toHaveLength(1)
    })

    test('a fault on a request whose caller has gone is not reported', async () => {
        const faults: unknown[] = []
        const reporting = createDispatcher(methods, { auth: RESOLVER, onFault: (_method, error) => faults.push(error) })
        const gone = new AbortController()
        gone.abort()
        expect(await reporting.dispatch({ method: 'fault' }, { ...context(), signal: gone.signal })).toMatchObject({ error: 'INTERNAL_ERROR' })
        expect(faults).toBeEmpty()
    })

    test('a method requiring a principal on a service with no resolver is refused as the table is built', () => {
        expect(() => createDispatcher(methods)).toThrow('createDispatcher: mine, admin require a principal, and there is no AuthResolver to name one')
    })
})

describe('client address', () => {
    const forwarded = (header: string | null): Request => new Request('http://x/', header === null ? undefined : { headers: { 'x-forwarded-for': header } })

    test("the socket peer's unless the application says otherwise", () => {
        expect(createDispatcher({}).clientIp(forwarded('203.0.113.9'), '198.51.100.2')).toBe('198.51.100.2')
    })

    test.each([
        ['the entry the proxy appended, not what the client wrote', '6.6.6.6, 203.0.113.9', '203.0.113.9'],
        ['one entry', '203.0.113.9', '203.0.113.9'],
        ['no header: the peer', null, '198.51.100.2']
    ])('trustProxy: %s', (_name, header, expected) => {
        expect(createDispatcher({}, { clientIp: trustProxy }).clientIp(forwarded(header), '198.51.100.2')).toBe(expected)
    })
})

describe('arguments', () => {
    const dispatcher = createDispatcher(methods, { auth: RESOLVER })

    test.each([
        ['the array the client sends', [{ amount: 21 }]],
        ['the bare object another language sends', { amount: 21 }]
    ])('unwraps %s', async (_name, args) => {
        expect(await dispatcher.dispatch({ method: 'charge', arguments: args as unknown[] }, context())).toEqual({ success: true, data: 42 })
    })

    test('a schema failure names the fields', async () => {
        expect(await dispatcher.dispatch({ method: 'charge', arguments: [{ amount: -1 }] }, context())).toEqual({
            success: false,
            error: 'INVALID_ARGUMENT',
            error_details: [{ field: 'amount', message: 'must be a positive number' }]
        })
    })

    test('missing arguments meet the schema like any other invalid value', async () => {
        expect(await dispatcher.dispatch({ method: 'charge' }, context())).toMatchObject({ error: 'INVALID_ARGUMENT' })
    })

    test('a method with no schema is handed nothing', async () => {
        expect(await dispatcher.dispatch({ method: 'open', arguments: ['ignored'] }, context())).toMatchObject({ success: true })
    })
})

describe('nonce dedup', () => {
    test('the second use of a nonce is refused', async () => {
        const dispatcher = createDispatcher(methods, { auth: RESOLVER, nonces: memoryNonceStore(60_000) })
        expect(await dispatcher.dispatch({ method: 'open', nonce: 'n1' }, context())).toMatchObject({ success: true })
        expect(await dispatcher.dispatch({ method: 'open', nonce: 'n1' }, context())).toMatchObject({ error: 'DUPLICATE_REQUEST' })
        expect(await dispatcher.dispatch({ method: 'open', nonce: 'n2' }, context())).toMatchObject({ success: true })
    })

    test('a call turned away before its method is reached leaves its nonce unspent', async () => {
        const dispatcher = createDispatcher(methods, { auth: RESOLVER, nonces: memoryNonceStore(60_000) })
        expect(await dispatcher.dispatch({ method: 'nope', nonce: 'n1' }, context(ADMIN))).toMatchObject({ error: 'UNKNOWN_METHOD' })
        expect(await dispatcher.dispatch({ method: 'admin', nonce: 'n1' }, context())).toMatchObject({ error: 'AUTH_REQUIRED' })
        expect(await dispatcher.dispatch({ method: 'admin', nonce: 'n1' }, context(USER))).toMatchObject({ error: 'ADMIN_REQUIRED' })
        expect(await dispatcher.dispatch({ method: 'admin', nonce: 'n1' }, context(ADMIN))).toMatchObject({ success: true })
    })

    test('a nonce is forgotten once its TTL has run out', async () => {
        const dispatcher = createDispatcher(methods, { auth: RESOLVER, nonces: memoryNonceStore(1) })
        expect(await dispatcher.dispatch({ method: 'open', nonce: 'n1' }, context())).toMatchObject({ success: true })
        await Bun.sleep(5)
        expect(await dispatcher.dispatch({ method: 'open', nonce: 'n1' }, context())).toMatchObject({ success: true })
    })

    test('without a store a nonce is carried but not checked', async () => {
        const dispatcher = createDispatcher(methods, { auth: RESOLVER })
        expect(await dispatcher.dispatch({ method: 'open', nonce: 'n1' }, context())).toMatchObject({ success: true })
        expect(await dispatcher.dispatch({ method: 'open', nonce: 'n1' }, context())).toMatchObject({ success: true })
    })
})

describe('locale', () => {
    test('the envelope tag reaches the handler through the application resolver', async () => {
        const dispatcher = createDispatcher({ where: { auth: 'none', handler: ctx => ctx.locale } }, { locale: requested => (requested === 'fr-CA' ? 'fr' : 'en') })
        expect(await dispatcher.dispatch({ method: 'where', locale: 'fr-CA' }, context())).toMatchObject({ data: 'fr' })
        expect(await dispatcher.dispatch({ method: 'where' }, context())).toMatchObject({ data: 'en' })
    })

    test('without a resolver it is empty', async () => {
        const dispatcher = createDispatcher({ where: { auth: 'none', handler: ctx => ctx.locale } })
        expect(await dispatcher.dispatch({ method: 'where', locale: 'fr-CA' }, context())).toMatchObject({ data: '' })
    })
})
