export const randomString = (prefix?: string): string => `${prefix || ''}${Math.random().toString(36).substring(2)}`

export const dClone = <T>(data: T): T => JSON.parse(JSON.stringify(data))

const isEmptyObject = (o: unknown): boolean => !!o && (o as object).constructor === Object && Object.keys(o as object).length === 0

export const isEmpty = (o: unknown, emptyArraysAreNotEmpty?: boolean): boolean => [null, undefined, '', NaN].includes(o as null) || (Array.isArray(o) && !o.length && !emptyArraysAreNotEmpty) || isEmptyObject(o)

export const arrayRemove = <T>(a: T[], el: T): void => {
    const index = a.indexOf(el)
    if (index > -1) {
        a.splice(index, 1)
    }
}

export const capitalise = (s: string): string => (s ? s.charAt(0).toUpperCase() + s.slice(1) : '')

// Compact human duration, two most significant units: 3661s → "1h 1m".
export const humanSeconds = (seconds: number): string => {
    const levels: [number, string][] = [
        [Math.floor(seconds / 31536000), 'y'],
        [Math.floor((seconds % 31536000) / 86400), 'd'],
        [Math.floor(((seconds % 31536000) % 86400) / 3600), 'h'],
        [Math.floor((((seconds % 31536000) % 86400) % 3600) / 60), 'm'],
        [(((seconds % 31536000) % 86400) % 3600) % 60, 's']
    ]
    const parts: string[] = []
    for (const [value, unit] of levels) {
        if (value === 0) continue
        parts.push(`${value}${unit}`)
        if (parts.length === 2) break
    }
    return parts.join(' ')
}

export const humanFileSize = (bytes: number, si = false, dp = 1): string => {
    const thresh = si ? 1000 : 1024
    if (Math.abs(bytes) < thresh) {
        return bytes + ' B'
    }
    const units = si ? ['kB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB'] : ['KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB', 'ZiB', 'YiB']
    let u = -1
    const r = 10 ** dp
    let size = bytes
    do {
        size /= thresh
        ++u
    } while (Math.round(Math.abs(size) * r) / r >= thresh && u < units.length - 1)
    return size.toFixed(dp) + ' ' + units[u]
}

const mimeByExtension: Record<string, string> = {
    heic: 'image/heic',
    heif: 'image/heif',
    avif: 'image/avif',
    webp: 'image/webp'
}

// File.type is empty when the OS has no registered association (common for
// HEIC on non-Apple platforms) — fall back to the extension map.
export const fileMime = (file: File): string => {
    if (file.type) return file.type
    const ext = file.name.split('.').pop()?.toLowerCase()
    return (ext && mimeByExtension[ext]) || 'application/octet-stream'
}

export interface Deferred<T> {
    promise: Promise<T>
    resolve: (value: T | PromiseLike<T>) => void
    reject: (reason?: unknown) => void
}

export const createDeferred = <T = void>(): Deferred<T> => {
    let resolve!: (value: T | PromiseLike<T>) => void
    let reject!: (reason?: unknown) => void
    const promise = new Promise<T>((res, rej) => {
        resolve = res
        reject = rej
    })
    return {
        promise,
        resolve,
        reject
    }
}
