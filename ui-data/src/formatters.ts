import { inlineMarkup } from './inlineMarkup'
import { messages } from './messages'

// Wire timestamps drift between carrying an explicit zone ('Z' or ±hh:mm) and
// omitting it. JavaScript parses a zoneless date-time string as LOCAL time, so
// a missing zone is normalized to UTC before parsing.
const HAS_ZONE = /(?:Z|[+-]\d{2}:?\d{2})$/i

/** An instant from an ISO/'YYYY-MM-DD HH:MM:SS' string (a zoneless one taken as UTC), a number or a Date. */
export const parseInstant = (v: unknown): Date => {
    if (typeof v === 'string') {
        const iso = v.replace(' ', 'T')
        return new Date(iso.includes('T') && !HAS_ZONE.test(iso) ? iso + 'Z' : iso)
    }
    if (typeof v === 'number' || v instanceof Date) {
        return new Date(v)
    }
    throw new TypeError(`parseInstant: unsupported value of type ${typeof v}`)
}

// A formatter rendering a missing value (null, undefined, '') as ''.
const blankable =
    (format: (v: unknown) => string) =>
    (v: unknown): string =>
        v == null || v === '' ? '' : format(v)

/** 'YYYY-MM-DD HH:MM:SSZ': the instant (parseInstant) in UTC, as the suffix says. */
export const fmtDate = blankable(v => parseInstant(v).toISOString().replace('T', ' ').slice(0, 19) + 'Z')

/** Table-cell boolean: a tick for true, blank for false. */
export const fmtBool = (v: unknown): string => (v ? '✓' : '')

/** Prose boolean for detail views, in the words `configureUiDataMessages` sets. */
export const fmtYesNo = (v: unknown): string => (v ? messages.yes : messages.no)

// A number or a numeric string, as Intl formats it: a string exactly, digit
// for digit.
const numeric = (v: unknown): number | Intl.StringNumericLiteral => {
    if (typeof v === 'number') {
        return v
    }
    if (typeof v === 'string' && v.trim() !== '' && !Number.isNaN(Number(v))) {
        return v as Intl.StringNumericLiteral
    }
    throw new TypeError(`not a number: ${String(v)}`)
}

// The ES2023 ceiling: no fraction digit a value carries is rounded away.
const localeNumber = new Intl.NumberFormat(undefined, { maximumFractionDigits: 100 })

/** A number (or numeric string) in the browser locale's notation, never rounded. */
export const fmtNumber = blankable(v => localeNumber.format(numeric(v)))

const BYTE_UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB']

const byteAmount = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 })

/** A byte count in binary units (B, KiB, MiB, …), at most one fraction digit, in the browser locale's notation. */
export const fmtBytes = blankable(v => {
    let amount = Number(numeric(v))
    let unit = 0
    // A unit holds amounts that read under 1024 once rounded to a tenth.
    while (unit < BYTE_UNITS.length - 1 && Math.abs(Math.round(amount * 10) / 10) >= 1024) {
        amount /= 1024
        unit++
    }
    return `${byteAmount.format(amount)} ${BYTE_UNITS[unit]}`
})

/** An Intl date/time style. */
export type DateStyle = 'full' | 'long' | 'medium' | 'short'

// One formatter per style, built on first use.
const perStyle = (build: (style: DateStyle) => Intl.DateTimeFormat): ((style: DateStyle) => (v: unknown) => string) => {
    const built = new Map<DateStyle, (v: unknown) => string>()
    return style => {
        let format = built.get(style)
        if (!format) {
            const intl = build(style)
            format = blankable(v => intl.format(parseInstant(v)))
            built.set(style, format)
        }
        return format
    }
}

/** A formatter of instants (parseInstant) in the browser's locale and time zone, at an Intl date and time style. */
export const fmtDateTime = perStyle(style => new Intl.DateTimeFormat(undefined, { dateStyle: style, timeStyle: style }))

/** A formatter of calendar dates ('YYYY-MM-DD') in the browser's locale, at an Intl date style; the day never shifts with the time zone. */
export const fmtDay = perStyle(style => new Intl.DateTimeFormat(undefined, { dateStyle: style, timeZone: 'UTC' }))

/** Markup as the one line of text it reads (inlineMarkup's text, whitespace collapsed). */
export const fmtMarkupText = blankable(v => {
    if (typeof v !== 'string') {
        throw new TypeError(`fmtMarkupText: markup must be a string, not ${typeof v}`)
    }
    return (inlineMarkup(v).textContent ?? '').replace(/\s+/g, ' ').trim()
})
