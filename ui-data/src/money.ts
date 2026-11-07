// Amounts as a payment's wire carries them: an integer count of a currency's
// minor units — cents, yen, fils — beside its ISO 4217 code and its scale, the
// decimals of its minor unit as the server reckons them: the browser's Intl
// follows a CLDR of its own, which departs from the server's for some
// currencies. Never a float: an amount is read and written through decimal
// strings.

/** The currency an amount is of: its ISO 4217 code and its scale. A payment on the wire is one. */
export interface Denomination {
    currency: string
    scale: number
}

// Each denomination's display formatter, made once: a table formats a column
// of amounts.
const formatters = new Map<string, Intl.NumberFormat>()

/** minor units at scale as the decimal string they count, digit for digit: 1250 at 2 is '12.50', 750 at 0 is '750' — the converse of toMinor. */
export const toDecimal = (minor: number, scale: number): string => {
    if (!Number.isSafeInteger(minor) || minor < 0) {
        throw new TypeError(`not an amount of minor units: ${minor}`)
    }
    const digits = String(minor).padStart(scale + 1, '0')
    return scale ? `${digits.slice(0, -scale)}.${digits.slice(-scale)}` : digits
}

/** minor units of a denomination in the browser locale's notation, with the currency's sign and the scale's decimals: 1250 of NZD at 2 is '$12.50' in en-NZ. */
export const fmtMoney = (minor: number, { currency, scale }: Denomination): string => {
    // Formatted from the decimal string, digit for digit.
    const amount = toDecimal(minor, scale) as Intl.StringNumericLiteral
    const key = `${currency}/${scale}`
    let formatter = formatters.get(key)
    if (!formatter) {
        formatter = new Intl.NumberFormat(undefined, {
            style: 'currency',
            currency,
            minimumFractionDigits: scale,
            maximumFractionDigits: scale
        })
        formatters.set(key, formatter)
    }
    return formatter.format(amount)
}

/** A decimal amount — a CurrencyInput's model, '12.50' — as the minor units of scale decimals the wire carries; null for what is no amount at that scale: no decimal, a fraction finer than it, or past what a count of them holds. */
export const toMinor = (amount: string, scale: number): number | null => {
    const match = /^(\d+)(?:\.(\d+))?$/.exec(amount)
    const fraction = (match?.[2] ?? '').replace(/0+$/, '')
    if (!match || fraction.length > scale) {
        return null
    }
    const minor = Number(match[1] + fraction.padEnd(scale, '0'))
    return Number.isSafeInteger(minor) ? minor : null
}
