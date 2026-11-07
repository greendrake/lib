import { describe, expect, test } from 'bun:test'
import { ApiError } from '@greendrake/rpc'
import { NetworkError } from '@greendrake/util'
import { apiUXHooks } from '../src/apiUXHooks'
import { useExceptionState } from '../src/exceptionState'
import { toastsUp, withSource } from './support'

// A fresh store, and what its failures' answers did, in order.
const fresh = () => {
    withSource()
    const answered: string[] = []
    const answers = (name: string) => ({
        retry: () => {
            answered.push(`retry ${name}`)
        },
        cancel: () => {
            answered.push(`cancel ${name}`)
        }
    })
    return {
        state: useExceptionState(),
        answered,
        answers
    }
}

describe('the exception toast', () => {
    test('stands for every failure raised while it shows: Try again retries them all, Cancel cancels them all', async () => {
        const { state, answered, answers } = fresh()
        state.fail(new ApiError('A'), answers('a'))
        state.fail(new ApiError('B'), answers('b'))
        expect(toastsUp().length).toBe(1)
        await toastsUp()[0]!.retry()
        expect(answered).toEqual(['retry a', 'retry b'])
        toastsUp()[0]!.cancel()
        expect(answered).toEqual(['retry a', 'retry b', 'cancel a', 'cancel b'])
        expect(state.exception).toBe(false)
        expect(toastsUp()).toEqual([])
    })

    test('shows the latest failure’s kind, and goes with the last withdrawn', () => {
        const { state } = fresh()
        const other = state.fail(new ApiError('A'))
        const network = state.fail(new NetworkError('gone'))
        expect(state.exception).toBe('network')
        network()
        expect(state.exception).toBe('other')
        expect(toastsUp().map(t => t.kind)).toEqual(['other'])
        other()
        expect(state.exception).toBe(false)
        expect(toastsUp()).toEqual([])
    })

    test('a withdrawn failure leaves the others to it', async () => {
        const { state, answered, answers } = fresh()
        const a = state.fail(new ApiError('A'), answers('a'))
        state.fail(new ApiError('B'), answers('b'))
        a()
        await toastsUp()[0]!.retry()
        expect(answered).toEqual(['retry b'])
    })

    test('two raisers failing with one error object each hold an answer', () => {
        const { state, answered, answers } = fresh()
        const shared = new ApiError('A')
        state.fail(shared, answers('a'))
        state.fail(shared, answers('b'))
        toastsUp()[0]!.cancel()
        expect(answered).toEqual(['cancel a', 'cancel b'])
    })
})

describe('apiUXHooks.onError', () => {
    test('raises the toast for an error nothing raised', () => {
        const { state } = fresh()
        apiUXHooks.onError(new Error('render'))
        expect(state.exception).toBe('other')
        expect(toastsUp().length).toBe(1)
    })

    test('passes by an error raised already — after Cancel, or after cancelAll', () => {
        const { state, answers } = fresh()
        const cancelled = new ApiError('A')
        state.fail(cancelled, answers('a'))
        toastsUp()[0]!.cancel()
        apiUXHooks.onError(cancelled)
        const navigatedFrom = new ApiError('B')
        state.fail(navigatedFrom, answers('b'))
        state.cancelAll()
        apiUXHooks.onError(navigatedFrom)
        expect(state.exception).toBe(false)
        expect(toastsUp()).toEqual([])
    })
})
