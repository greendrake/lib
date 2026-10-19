import { describe, expect, test } from 'bun:test'
import { ApiError } from '@greendrake/rpc'
import { inWords, wording } from '../src/problems'

const worded: [code: string, message: string][] = [
    ['UNKNOWN_CAMERA', 'There is no camera "shed".'],
    ['NO_SETTINGS', 'The camera has no settings to change.'],
    ['CAMERA_REFUSED', 'The camera refused: 107 no permission'],
    ['INVALID_VALUE', 'Not a whole number from 1 to 65535.'],
    ['CAMERA_OFFLINE', 'The camera is offline.'],
    ['UNKNOWN_SETTING', 'The camera has no setting "x".'],
    ['UNKNOWN_GROUP', 'There is no group of settings "x".']
]

describe('a failure the engine words', () => {
    test('is shown in the engine’s words', () => {
        for (const [code, message] of worded) {
            expect(wording(new ApiError(code, { Message: message }))).toBe(message)
        }
    })
    test('and any other by its message', () => {
        expect(wording(new ApiError('INTERNAL_ERROR'))).toBe('INTERNAL_ERROR')
        // An error that only reads like the code is not the engine's.
        expect(wording(new Error('CAMERA_OFFLINE'))).toBe('CAMERA_OFFLINE')
    })
})

describe('an action in words', () => {
    test('fails with the words of its failure, the failure kept as the cause', async () => {
        const failure = new ApiError('CAMERA_OFFLINE', { Message: 'The camera is offline.' })
        const action = inWords(() => Promise.reject(failure))
        await expect(action()).rejects.toThrow('The camera is offline.')
        await expect(action()).rejects.toMatchObject({ cause: failure })
    })
    test('and answers as the action does when it succeeds', async () => {
        expect(await inWords((n: number) => Promise.resolve(n * 2))(21)).toBe(42)
    })
})
