// Wire timestamps drift between carrying an explicit zone ('Z' or ±hh:mm) and
// omitting it. JavaScript parses a zoneless date-time string as LOCAL time, so
// a missing zone is normalized to UTC before parsing. Output is always
// 'YYYY-MM-DD HH:MM:SSZ' — the value IS UTC and the suffix says so.
const HAS_ZONE = /(?:Z|[+-]\d{2}:?\d{2})$/i

export const fmtDate = (v: unknown): string => {
    if (v == null || v === '') return ''
    let d: Date
    if (typeof v === 'string') {
        const iso = v.replace(' ', 'T')
        d = new Date(iso.includes('T') && !HAS_ZONE.test(iso) ? iso + 'Z' : iso)
    } else if (typeof v === 'number' || v instanceof Date) {
        d = new Date(v)
    } else {
        throw new TypeError(`fmtDate: unsupported value of type ${typeof v}`)
    }
    return d.toISOString().replace('T', ' ').slice(0, 19) + 'Z'
}

/** Table-cell boolean: a tick for true, blank for false. */
export const fmtBool = (v: unknown): string => (v ? '✓' : '')

/** Prose boolean for detail views. */
export const fmtYesNo = (v: unknown): string => (v ? 'yes' : 'no')
