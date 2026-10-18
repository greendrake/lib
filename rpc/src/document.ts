// The state-stream wire, for both ends: what a document is, the RFC 7386 merge patch
// between two of them, and applying one.

/** A document, or an object within one: a JSON object. */
export type JsonObject = Record<string, unknown>

/** What an RFC 7386 merge patch over T can carry: any of an object's members, patched in turn, or null to remove it. Anything else replaces. */
export type MergePatch<T> = T extends readonly unknown[] ? T : T extends object ? { [K in keyof T]?: MergePatch<T[K]> | null } : T

/** A state stream's frame: the whole document, when a socket starts keeping it, or a merge patch over it. */
export type StateFrame<D extends object> = { doc: D } | { patch: MergePatch<D> }

const isObject = (value: unknown): value is JsonObject => typeof value === 'object' && value !== null && !Array.isArray(value)

// A member read only where the object has it of its own: an id such as "__proto__" or
// "toString" names nothing inherited.
const own = (object: JsonObject, key: string): unknown => (Object.hasOwn(object, key) ? object[key] : undefined)

// A member written as the object's own, whatever its key. Assignment does that for every
// key but "__proto__", whose inherited accessor would set the object's prototype instead.
const put = (object: JsonObject, key: string, value: unknown): void => {
    if (key === '__proto__') {
        Object.defineProperty(object, key, {
            value,
            writable: true,
            enumerable: true,
            configurable: true
        })
        return
    }
    object[key] = value
}

// The null members of object, and of the objects within it, removed.
const dropNulls = (object: JsonObject): void => {
    for (const [key, value] of Object.entries(object)) {
        if (value === null) {
            delete object[key]
        } else if (isObject(value)) {
            dropNulls(value)
        }
    }
}

/**
 * The wire form of the document `type`: value as JSON, decoded, its null members
 * dropped — a patch says removal with null, so a document cannot hold one. A value that
 * is no JSON object is no document, and throws.
 */
export const documentOf = (type: string, value: object): JsonObject => {
    const decoded: unknown = JSON.parse(JSON.stringify(value))
    if (!isObject(decoded)) {
        throw new Error(`document ${type} is not a JSON object`)
    }
    dropNulls(decoded)
    return decoded
}

const equal = (a: unknown, b: unknown): boolean => {
    if (Array.isArray(a) && Array.isArray(b)) {
        return a.length === b.length && a.every((value, i) => equal(value, b[i]))
    }
    if (isObject(a) && isObject(b)) {
        const keys = Object.keys(a)
        return keys.length === Object.keys(b).length && keys.every(key => Object.hasOwn(b, key) && equal(a[key], b[key]))
    }
    return a === b
}

/**
 * The RFC 7386 merge patch that turns the document prev into next, or null where they
 * are equal: a member next lacks is null, one whose value changed carries the new value
 * — patched in turn where both values are objects — and an unchanged one is left out.
 */
export const mergePatchBetween = (prev: JsonObject, next: JsonObject): JsonObject | null => {
    const removed = Object.keys(prev)
        .filter(key => !Object.hasOwn(next, key))
        .map((key): [string, unknown] => [key, null])
    const changed = Object.entries(next).flatMap(([key, value]): [string, unknown][] => {
        if (!Object.hasOwn(prev, key)) {
            return [[key, value]]
        }
        const was = prev[key]
        if (isObject(was) && isObject(value)) {
            const patch = mergePatchBetween(was, value)
            return patch ? [[key, patch]] : []
        }
        return equal(was, value) ? [] : [[key, value]]
    })
    const entries = [...removed, ...changed]
    // fromEntries defines each member, so an id that is "__proto__" is one like any other.
    return entries.length > 0 ? Object.fromEntries(entries) : null
}

/**
 * Applies an RFC 7386 merge patch to target, in place, so a reactive target re-renders
 * only what the patch touches. A member named `__proto__` is the one exception: it is
 * defined rather than assigned, which a reactive target does not see, and Vue tracks no
 * key of that name.
 */
export const applyMergePatch = (target: JsonObject, patch: JsonObject): void => {
    for (const [key, value] of Object.entries(patch)) {
        if (value === null) {
            delete target[key]
            continue
        }
        if (!isObject(value)) {
            put(target, key, value)
            continue
        }
        const current = own(target, key)
        if (isObject(current)) {
            applyMergePatch(current, value)
            continue
        }
        const fresh: JsonObject = {}
        applyMergePatch(fresh, value)
        put(target, key, fresh)
    }
}
