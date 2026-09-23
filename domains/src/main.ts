// TLD snapshot: IANA root-zone TLD list (single-label, lowercase, punycode
// form for IDN TLDs). Refresh with `bun run update-tlds` (see scripts/).
import TLDs from './tlds.json'

const tldSet = new Set(TLDs as string[])

const domainLabelReString = '(?!-)[a-zA-Z0-9-]{1,63}(?<!-)'
const domainReString = `${domainLabelReString}(?:\\.(${domainLabelReString}))+$`
const domainRe = new RegExp(`^${domainReString}$`)
const emailRe = new RegExp("^['a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@" + domainReString)

const normalise = (d: string | null | undefined): string => (d ? d.trim().toLowerCase() : '')

// Structurally valid, dotted, and ending in a real (IANA-listed) TLD.
export const isDomainValid = (d: string | null | undefined): boolean => {
    const name = normalise(d)
    if (!domainRe.test(name)) {
        return false
    }
    const lastLabel = name.split('.').pop()!
    return tldSet.has(lastLabel)
}

export const isEmailValid = (v: string): boolean => emailRe.test(v) && isDomainValid(v.split('@')[1])
