import './dom'
import { afterEach, describe, expect, test } from 'bun:test'
import { fmtBytes, fmtDate, fmtDateTime, fmtDay, fmtMarkupText, fmtNumber, parseInstant } from '../src/formatters'

// Intl's output is the test machine's locale's; the cases below assert what
// does not depend on it, or build the expectation through Intl too.

describe('parseInstant', () => {
    test('a zoneless string is UTC, a zoned one keeps its zone', () => {
        expect(parseInstant('2026-10-17 01:02:03').toISOString()).toBe('2026-10-17T01:02:03.000Z')
        expect(parseInstant('2026-10-17T01:02:03').toISOString()).toBe('2026-10-17T01:02:03.000Z')
        expect(parseInstant('2026-10-17T01:02:03+13:00').toISOString()).toBe('2026-10-16T12:02:03.000Z')
        expect(parseInstant('2026-10-17').toISOString()).toBe('2026-10-17T00:00:00.000Z')
        expect(parseInstant(0).toISOString()).toBe('1970-01-01T00:00:00.000Z')
        expect(parseInstant(new Date(0)).toISOString()).toBe('1970-01-01T00:00:00.000Z')
    })

    test('throws on any other value', () => {
        expect(() => parseInstant(true)).toThrow(TypeError)
    })
})

test('fmtDate is the instant in UTC', () => {
    expect(fmtDate('2026-10-17T01:02:03+13:00')).toBe('2026-10-16 12:02:03Z')
})

test('a missing value formats as nothing', () => {
    for (const format of [fmtDate, fmtNumber, fmtBytes, fmtDateTime('short'), fmtDay('short'), fmtMarkupText]) {
        for (const missing of [null, undefined, '']) {
            expect(format(missing)).toBe('')
        }
    }
})

describe('fmtNumber', () => {
    const digits = (s: string): string => s.replace(/\D/g, '')

    test('keeps every digit, of a number or a decimal string', () => {
        expect(digits(fmtNumber(3.14159))).toBe('314159')
        expect(digits(fmtNumber('0.000125'))).toBe('0000125')
        expect(digits(fmtNumber('12345678901234567890.123456789'))).toBe('12345678901234567890123456789')
    })

    test('refuses what is not a number', () => {
        expect(() => fmtNumber('abc')).toThrow(TypeError)
        expect(() => fmtNumber(' ')).toThrow(TypeError)
        expect(() => fmtNumber(true)).toThrow(TypeError)
    })
})

test('fmtBytes picks the unit an amount reads under 1024 in, rounded to a tenth', () => {
    const amount = (n: number): string => new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(n)
    const cases: [number | string, string][] = [
        [0, `${amount(0)} B`],
        [1023, `${amount(1023)} B`],
        [1024, `${amount(1)} KiB`],
        ['2048', `${amount(2)} KiB`],
        [1536, `${amount(1.5)} KiB`],
        // 1023.95 KiB rounds to 1024: the next unit's 1.
        [1048525, `${amount(1)} MiB`],
        // 1023.94 KiB rounds to 1023.9 and stays.
        [1048514, `${amount(1023.9)} KiB`],
        [1048576, `${amount(1)} MiB`],
        [2 ** 30, `${amount(1)} GiB`],
        [2 ** 40, `${amount(1)} TiB`],
        [2 ** 50, `${amount(1)} PiB`],
        [2 ** 60, `${amount(1)} EiB`],
        // Past the last unit, amounts grow in it.
        [2 ** 70, `${amount(1024)} EiB`]
    ]
    for (const [bytes, want] of cases) {
        expect(fmtBytes(bytes)).toBe(want)
    }
})

describe('date styles', () => {
    const tz = process.env.TZ
    afterEach(() => {
        process.env.TZ = tz
    })

    test('one formatter per style', () => {
        expect(fmtDateTime('short')).toBe(fmtDateTime('short'))
        expect(fmtDateTime('short')).not.toBe(fmtDateTime('long'))
        expect(fmtDay('medium')).toBe(fmtDay('medium'))
        expect(fmtDay('medium')).not.toBe(fmtDay('full'))
    })

    test('fmtDay never shifts the day with the time zone', () => {
        process.env.TZ = 'America/Los_Angeles'
        // In this zone the day's UTC midnight is still the day before.
        expect(new Intl.DateTimeFormat(undefined, { day: 'numeric' }).format(new Date('2026-10-17'))).toBe('16')
        expect(fmtDay('long')('2026-10-17')).toBe(new Intl.DateTimeFormat(undefined, { dateStyle: 'long', timeZone: 'UTC' }).format(Date.UTC(2026, 9, 17)))
        expect(fmtDay('long')('2026-10-17')).toContain('17')
    })
})

test('fmtMarkupText reads markup as one line of text', () => {
    expect(fmtMarkupText('<p>Fish &amp;\n <b>chips</b></p><p>  daily</p><script>x()</script>')).toBe('Fish & chips daily')
    expect(() => fmtMarkupText(5)).toThrow(TypeError)
})
