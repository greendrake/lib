// The settings form's model. The backend describes each setting, its kind and its
// value; a section's form state is its drafts, one per editable setting in its control's
// terms, and a change writes the setting's value from its draft.
import type { Setting } from './types'

/** The kinds of setting edited as text. */
const TEXT_TYPES = ['integer', 'fraction', 'text', 'ipv4'] as const

/** A setting edited as text. */
export type TextSetting = Extract<Setting, { Type: (typeof TEXT_TYPES)[number] }>

/** Whether setting is edited as text. */
export const isText = (setting: Setting): setting is TextSetting => TEXT_TYPES.some(type => type === setting.Type)

/** A setting a change writes. */
export type Writable = Exclude<Setting, { Type: 'readonly' }>

/** A section's form state: each editable setting's draft in its control's terms, by key. */
export interface Drafts {
    text: Record<string, string>
    switch: Record<string, boolean>
    // Null where the camera's value is none of the options.
    choice: Record<string, string | null>
}

const WHOLE = /^-?\d+$/

const DOTTED = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/

// A percentage to two decimals.
const PERCENT = /^\d{1,3}(\.\d{1,2})?$/

/** What a text of setting's kind takes. */
const aText = (setting: Extract<Setting, { Type: 'text' }>): string => {
    const { MinLength: least, MaxLength: most, Excluded: excluded } = setting
    const length =
        least !== undefined && most !== undefined ? ` of ${least} to ${most} characters` : least !== undefined ? ` of at least ${least} characters` : most !== undefined ? ` of at most ${most} characters` : ''
    return `A text${length}${excluded ? ` without ${[...excluded].join(' or ')}` : ''}`
}

/** Checks a text typed for setting, saying why it is not a value of its kind: as the backend checks it, as it is typed. */
export const checkText = (setting: TextSetting, text: string): true | string => {
    switch (setting.Type) {
        case 'integer': {
            const n = Number(text)
            const bounds = [setting.Min !== undefined && `from ${setting.Min}`, setting.Max !== undefined && `to ${setting.Max}`].filter(Boolean).join(' ')
            return (WHOLE.test(text) && (setting.Min === undefined || n >= setting.Min) && (setting.Max === undefined || n <= setting.Max)) || `A whole number ${bounds}`.trimEnd()
        }
        case 'fraction':
            return (PERCENT.test(text) && Number(text) <= 100) || 'A percentage from 0 to 100, to two decimals'
        case 'ipv4':
            return (DOTTED.test(text) && text.split('.').every(byte => Number(byte) <= 255)) || 'An IPv4 address: four numbers to 255 joined by dots'
        case 'text': {
            // Characters, as the backend counts them: code points.
            const length = [...text].length
            const fits = (setting.MinLength === undefined || length >= setting.MinLength) && (setting.MaxLength === undefined || length <= setting.MaxLength)
            return (fits && ![...(setting.Excluded ?? '')].some(c => text.includes(c))) || aText(setting)
        }
    }
}

/** A share of the picture as the percentage it is edited in, to two decimals: 0.943359375 is "94.34". */
const percentOf = (share: number): string => String(Number((share * 100).toFixed(2)))

/** The drafts of settings as read. */
export const draftsOf = (settings: Setting[]): Drafts => {
    const drafts: Drafts = {
        text: {},
        switch: {},
        choice: {}
    }
    for (const s of settings) {
        if (s.Type === 'integer') drafts.text[s.Key] = String(s.Value)
        if (s.Type === 'fraction') drafts.text[s.Key] = percentOf(s.Value)
        if (s.Type === 'text' || s.Type === 'ipv4') drafts.text[s.Key] = s.Value
        if (s.Type === 'switch') drafts.switch[s.Key] = s.Value
        if (s.Type === 'choice') drafts.choice[s.Key] = s.Options.find(o => o.Value === s.Value)?.Value ?? null
    }
    return drafts
}

/** What a change of setting writes: its draft as a value of its kind; nothing while a text is not yet one. */
export const writeOf = (setting: Writable, drafts: Drafts): unknown => {
    switch (setting.Type) {
        case 'switch':
            return drafts.switch[setting.Key]
        case 'choice':
            return drafts.choice[setting.Key] ?? undefined
        case 'pinned':
            return setting.Only
    }
    const text = drafts.text[setting.Key]!
    if (checkText(setting, text) !== true) {
        return undefined
    }
    switch (setting.Type) {
        case 'integer':
            return Number(text)
        case 'fraction':
            return Number(text) / 100
    }
    return text
}

/**
 * The drafts once a write has been read back: a draft still as it was sent becomes what
 * the camera has, which can differ from what it was sent; one edited while the write was
 * out stays as edited, for its own change to write.
 */
export const settle = (drafts: Drafts, sent: Drafts, now: Drafts): void => {
    const take = <T>(draft: Record<string, T>, was: Record<string, T>, has: Record<string, T>): void => {
        for (const key of Object.keys(has)) {
            if (draft[key] === was[key]) draft[key] = has[key]!
        }
    }
    take(drafts.text, sent.text, now.text)
    take(drafts.switch, sent.switch, now.switch)
    take(drafts.choice, sent.choice, now.choice)
}
