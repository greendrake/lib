import { describe, expect, test } from 'bun:test'
import { fmtMoney, toDecimal, toMinor } from '../src/money'

// Intl's output is the test machine's locale's; the cases below build the
// expectation through Intl too, at the scale's decimals.
const formatted = (amount: string, currency: string, scale: number): string =>
    new Intl.NumberFormat(undefined, {
        style: 'currency',
        currency,
        minimumFractionDigits: scale,
        maximumFractionDigits: scale
    }).format(amount as Intl.StringNumericLiteral)

describe('fmtMoney', () => {
    test('formats minor units at the scale', () => {
        expect(fmtMoney(1250, { currency: 'NZD', scale: 2 })).toBe(formatted('12.50', 'NZD', 2))
        expect(fmtMoney(5, { currency: 'USD', scale: 2 })).toBe(formatted('0.05', 'USD', 2))
        expect(fmtMoney(1500, { currency: 'JPY', scale: 0 })).toBe(formatted('1500', 'JPY', 0))
        expect(fmtMoney(12345, { currency: 'KWD', scale: 3 })).toBe(formatted('12.345', 'KWD', 3))
        expect(fmtMoney(0, { currency: 'USD', scale: 2 })).toBe(formatted('0', 'USD', 2))
    })

    test('counts the decimals the scale has, not those Intl has for the currency', () => {
        const intl = (amount: number, currency: string): string => new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(amount)
        expect(fmtMoney(1250, { currency: 'USD', scale: 3 })).toBe(formatted('1.250', 'USD', 3))
        expect(fmtMoney(1250, { currency: 'USD', scale: 3 })).not.toBe(intl(1.25, 'USD'))
        expect(fmtMoney(1250, { currency: 'USD', scale: 0 })).not.toBe(intl(1250, 'USD'))
    })

    test('keeps every digit past what a float holds', () => {
        expect(fmtMoney(9007199254740991, { currency: 'USD', scale: 2 })).toBe(formatted('90071992547409.91', 'USD', 2))
    })

    test('formats a denomination again as it did first', () => {
        expect(fmtMoney(1250, { currency: 'NZD', scale: 2 })).toBe(fmtMoney(1250, { currency: 'NZD', scale: 2 }))
    })

    test('refuses what is no count of minor units', () => {
        for (const minor of [12.5, -1, Number.NaN, 2 ** 53]) {
            expect(() => fmtMoney(minor, { currency: 'USD', scale: 2 })).toThrow(TypeError)
        }
    })
})

describe('toDecimal', () => {
    test('is the decimal string minor units count at the scale, toMinor read back', () => {
        expect(toDecimal(1250, 2)).toBe('12.50')
        expect(toDecimal(5, 2)).toBe('0.05')
        expect(toDecimal(750, 0)).toBe('750')
        expect(toDecimal(12345, 3)).toBe('12.345')
        for (const [minor, scale] of [
            [1250, 2],
            [5, 2],
            [750, 0]
        ]) {
            expect(toMinor(toDecimal(minor, scale), scale)).toBe(minor)
        }
    })

    test('refuses what is no count of minor units', () => {
        expect(() => toDecimal(12.5, 2)).toThrow(TypeError)
    })
})

describe('toMinor', () => {
    test('is the minor units a decimal amount names at the scale', () => {
        expect(toMinor('12.50', 2)).toBe(1250)
        expect(toMinor('12.5', 2)).toBe(1250)
        expect(toMinor('12', 2)).toBe(1200)
        expect(toMinor('0.05', 2)).toBe(5)
        expect(toMinor('1500', 0)).toBe(1500)
        expect(toMinor('1500.00', 0)).toBe(1500)
        expect(toMinor('12.345', 3)).toBe(12345)
    })

    test('is null for a fraction finer than the scale, or past what a count holds', () => {
        expect(toMinor('12.345', 2)).toBeNull()
        expect(toMinor('1500.5', 0)).toBeNull()
        expect(toMinor('99999999999999999', 2)).toBeNull()
    })

    test('is null for what is no amount', () => {
        for (const amount of ['', '12.', '.5', '.00', '-1', '1,200.00', '1e3', 'abc']) {
            expect(toMinor(amount, 2)).toBeNull()
        }
    })
})
