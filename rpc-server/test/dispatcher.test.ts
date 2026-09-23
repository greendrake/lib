import { describe, expect, test } from 'bun:test'
import { ANONYMOUS, ApiError, createDispatcher, memoryNonceStore, method } from '../src/main'
import type { AuthInfo, CallContext, Methods } from '../src/main'
import { schema } from './standardSchema'

const positive = schema<{ amount: number }>(value => {
    const amount = (value as { amount?: unknown } | undefined)?.amount
    return typeof amount === 'number' && amount > 0 ? { value: { amount } } : { issues: [{ message: 'must be a positive number', path: ['amount'] }] }
})

const USER: AuthInfo = {
    userId: 'u1',
    isAdmin: false,
    phantom: false
}
const ADMIN: AuthInfo = {
    userId: 'root',
    isAdmin: true,
    phantom: false
}
const PHANTOM: AuthInfo = {
    userId: 'p1',
    isAdmin: false,
    phantom: true
}

const methods: Methods = {
    open: { auth: 'none', handler: () => 'anyone' },
    mine: { auth: 'user', handler: ctx => ctx.auth.userId },
    admin: { auth: 'admin', handler: () => 'root only' },
    'phantom.ok': {
        auth: 'user',
        allowPhantom: true,
        handler: () => 'provisional'
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

const context = (auth: AuthInfo = ANONYMOUS): CallContext => ({
    auth,
    clientIp: '127.0.0.1',
    signal: AbortSignal.any([])
})

describe('dispatch', () => {
    const dispatcher = createDispatcher(methods)

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
        ['none, anonymous', 'open', ANONYMOUS, { success: true, data: 'anyone' }],
        ['user, anonymous', 'mine', ANONYMOUS, { success: false, error: 'AUTH_REQUIRED' }],
        ['user, signed in', 'mine', USER, { success: true, data: 'u1' }],
        ['admin, ordinary user', 'admin', USER, { success: false, error: 'ADMIN_REQUIRED' }],
        ['admin, admin', 'admin', ADMIN, { success: true, data: 'root only' }],
        ['user, phantom', 'mine', PHANTOM, { success: false, error: 'PHANTOM_NOT_ALLOWED' }],
        ['phantom allowed', 'phantom.ok', PHANTOM, { success: true, data: 'provisional' }]
    ])('auth rule: %s', async (_name, method, auth, expected) => {
        expect(await dispatcher.dispatch({ method }, context(auth))).toMatchObject(expected)
    })

    test('an ApiError is the handler saying what happened', async () => {
        expect(await dispatcher.dispatch({ method: 'refuse' }, context())).toEqual({
            success: false,
            error: 'NOT_ENOUGH_ROPE',
            error_details: { short_by: 3 }
        })
    })

    test('anything else a handler throws is INTERNAL_ERROR', async () => {
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

    test('a fault on a request whose caller has gone is not logged', async () => {
        const logged: unknown[] = []
        const original = console.error
        console.error = (...args: unknown[]) => logged.push(args)
        try {
            const gone = new AbortController()
            gone.abort()
            expect(await dispatcher.dispatch({ method: 'fault' }, { ...context(), signal: gone.signal })).toMatchObject({ error: 'INTERNAL_ERROR' })
        } finally {
            console.error = original
        }
        expect(logged).toBeEmpty()
    })
})

describe('arguments', () => {
    const dispatcher = createDispatcher(methods)

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
        const dispatcher = createDispatcher(methods, { nonces: memoryNonceStore(60_000) })
        expect(await dispatcher.dispatch({ method: 'open', nonce: 'n1' }, context())).toMatchObject({ success: true })
        expect(await dispatcher.dispatch({ method: 'open', nonce: 'n1' }, context())).toMatchObject({ error: 'DUPLICATE_REQUEST' })
        expect(await dispatcher.dispatch({ method: 'open', nonce: 'n2' }, context())).toMatchObject({ success: true })
    })

    test('a nonce is forgotten once its TTL has run out', async () => {
        const dispatcher = createDispatcher(methods, { nonces: memoryNonceStore(1) })
        expect(await dispatcher.dispatch({ method: 'open', nonce: 'n1' }, context())).toMatchObject({ success: true })
        await Bun.sleep(5)
        expect(await dispatcher.dispatch({ method: 'open', nonce: 'n1' }, context())).toMatchObject({ success: true })
    })

    test('without a store a nonce is carried but not checked', async () => {
        const dispatcher = createDispatcher(methods)
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
