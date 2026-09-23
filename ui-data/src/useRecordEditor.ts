import { reactive, computed, ref, type Ref, type ComputedRef } from 'vue'

/**
 * Record-edit pattern for CRUD detail editors.
 *
 * Convention (matches what CrudPanel and patchRow expect):
 *
 *   1. The detail editor receives the full record it's editing (loaded by the
 *      caller from a list row, an API call, etc.).
 *   2. It keeps two parallel objects:
 *        - `data`     — the canonical state acknowledged by the backend.
 *        - `workData` — a reactive working copy bound to inputs.
 *   3. On save, only fields that differ between `workData` and `data` are
 *      sent. Sending the entire record on every save is wasteful and risks
 *      clobbering server-side changes to fields the user didn't touch.
 *   4. The `save(patch)` callback returns the FULL updated record. The
 *      backend may modify fields beyond what the user changed (timestamps,
 *      derived fields, server-side defaults); the editor must acknowledge
 *      those by overwriting its local state with the response. This makes
 *      the next diff correct and surfaces server changes in the UI.
 *   5. The caller wires `('saved', key, record)` → `CrudPanel.applySaved(key,
 *      record, tab.id)` to update the row in place — no list re-fetch.
 *
 * Field shapes supported: strings, numbers, booleans, arrays, plain objects.
 * Equality is by canonical JSON — sufficient for typical CRUD payloads.
 *
 * What this composable does NOT do:
 *   - HTTP. The caller supplies `save(patch)`.
 *   - Validation. Caller-side concern; gate `submit()` behind your own checks.
 *   - File uploads, multi-step save flows. Build those around `changes` /
 *     `data` / `workData` and call `applyResponse(updated)` manually instead
 *     of `submit()` for those cases.
 */
export interface RecordEditor<T extends Record<keyof T & string, unknown>> {
    /** Canonical state acknowledged by the backend. Mutates after each save. */
    data: T
    /** Reactive working copy bound to inputs. Edit this freely. */
    workData: T
    /** Diff of workData vs data, or null when nothing has changed. */
    changes: ComputedRef<Partial<T> | null>
    /** True when there are unsaved changes. */
    dirty: ComputedRef<boolean>
    /** True while a `submit()` call is in flight. */
    saving: Ref<boolean>
    /**
     * Send the current diff via `save(patch)`, then assign the response onto
     * both `data` and `workData`. No-op (returns null) when not dirty.
     */
    submit: () => Promise<T | null>
    /** Discard local edits, reverting workData to data. */
    reset: () => void
    /**
     * Manually acknowledge a server-side update (useful when the save flow
     * is custom — e.g. file uploads — and `submit()` isn't called).
     */
    applyResponse: (updated: T) => void
}

export interface RecordEditorOptions<T extends Record<keyof T & string, unknown>> {
    /** Initial record snapshot. Deep-cloned into both data and workData. */
    initial: T
    /** Sends the diff to the backend; must resolve to the full updated record. */
    save: (patch: Partial<T>) => Promise<T>
    /**
     * Restrict which keys are considered editable / diffed. Defaults to the
     * keys present on `initial`. Useful when the record contains read-only
     * fields (e.g. id, created_at) that should never be sent in a patch.
     */
    fields?: readonly (keyof T)[]
    /**
     * Canonicalise a record before it enters the editor — applied to `initial`
     * and to every save response. Use it to collapse irrelevant wire
     * differences (`undefined` vs `null`, absent vs empty string) so the diff
     * doesn't oscillate on them.
     */
    normalise?: (record: T) => T
}

export default function useRecordEditor<T extends Record<keyof T & string, unknown>>(options: RecordEditorOptions<T>): RecordEditor<T> {
    const fields = (options.fields ?? Object.keys(options.initial)) as (keyof T)[]
    const normalise = options.normalise ?? ((record: T): T => record)
    const pick = (src: T): T => {
        const out = {} as T
        for (const k of fields) out[k] = src[k]
        return out
    }

    // JSON clone — matches the diff equality function below and handles Vue
    // proxies (structuredClone chokes on them) without an extra `toRaw` step.
    const clone = <V>(v: V): V => JSON.parse(JSON.stringify(v)) as V
    const initial = normalise(clone(options.initial))
    const data = reactive(initial) as T
    const workData = reactive(clone(pick(initial))) as T
    const saving = ref(false)

    const changes = computed<Partial<T> | null>(() => {
        const diff: Partial<T> = {}
        let any = false
        for (const k of fields) {
            if (!equal(workData[k], data[k])) {
                diff[k] = workData[k]
                any = true
            }
        }
        return any ? diff : null
    })

    const dirty = computed(() => changes.value !== null)

    function applyResponse(updated: T): void {
        const normalised = normalise(updated)
        Object.assign(data, normalised)
        Object.assign(workData, pick(normalised))
    }

    async function submit(): Promise<T | null> {
        const patch = changes.value
        if (!patch) return null
        saving.value = true
        try {
            const updated = await options.save(patch)
            applyResponse(updated)
            return updated
        } finally {
            saving.value = false
        }
    }

    function reset(): void {
        Object.assign(workData, pick(data))
    }

    return {
        data,
        workData,
        changes,
        dirty,
        saving,
        submit,
        reset,
        applyResponse
    }
}

function equal(a: unknown, b: unknown): boolean {
    if (a === b) return true
    return JSON.stringify(a) === JSON.stringify(b)
}
