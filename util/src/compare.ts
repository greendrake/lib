export type Comparator<T> = (a: T, b: T) => number

// Tie-breaking composition: later comparators apply only where earlier ones tie.
export const composeComparators =
    <T>(...comparators: Comparator<T>[]): Comparator<T> =>
    (a, b) => {
        for (const compare of comparators) {
            const result = compare(a, b)
            if (result !== 0) {
                return result
            }
        }
        return 0
    }

export const byProp = <T, K extends keyof T>(prop: K, descending?: boolean): Comparator<T> => {
    const direction = descending ? -1 : 1
    return (a, b) => {
        const aVal = a[prop]
        const bVal = b[prop]
        return aVal === bVal ? 0 : aVal > bVal ? direction : -direction
    }
}

// Order by position in a preset list (case-insensitive); values missing from
// the preset tie with everything (defer to the next comparator).
export const byPreset = <T, K extends keyof T>(prop: K, preset: string[]): Comparator<T> => {
    const indexByValue = new Map(preset.map((key, i) => [key.toLowerCase(), i]))
    return (a, b) => {
        const iA = indexByValue.get(String(a[prop]).toLowerCase())
        const iB = indexByValue.get(String(b[prop]).toLowerCase())
        return iA === undefined || iB === undefined || iA === iB ? 0 : iA > iB ? 1 : -1
    }
}
