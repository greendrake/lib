import { describe, expect, test } from 'bun:test'
import { isDomainValid, isEmailValid } from '../src/main'

describe('isDomainValid', () => {
    test.each([
        ['example.com', true],
        ['sub.example.co.nz', true],
        ['EXAMPLE.COM', true],
        ['  example.com  ', true],
        ['example', false],
        ['example.invalidtldxyz', false],
        ['-bad.com', false],
        ['bad-.com', false],
        ['', false],
        [null, false],
        [undefined, false]
    ] as [string | null | undefined, boolean][])('isDomainValid(%p) → %p', (value, expected) => {
        expect(isDomainValid(value)).toBe(expected)
    })
})

describe('isEmailValid', () => {
    test.each([
        ['user@example.com', true],
        ["o'brien@example.co.nz", true],
        ['user+tag@example.com', true],
        ['user@example', false],
        ['user@example.invalidtldxyz', false],
        ['not-an-email', false],
        ['@example.com', false]
    ] as [string, boolean][])('isEmailValid(%p) → %p', (value, expected) => {
        expect(isEmailValid(value)).toBe(expected)
    })
})
