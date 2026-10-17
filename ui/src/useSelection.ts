import { shallowReactive, shallowRef, toValue, type MaybeRefOrGetter, type Ref, type ComputedRef } from 'vue'

type Key = string | number

// The arrows' steps through the list.
const arrowSteps: Record<string, number> = { ArrowUp: -1, ArrowDown: 1 }

// Flat, index-addressable multi-selection over an ordered item list: single
// (replace), ctrl-toggle, shift-range from an anchor, keyboard cursor moves,
// select-all. The selected Set holds item ids read from items[i][idField];
// the anchor and the cursor are rows, addressed by items[i][keyField] — the
// id itself unless an item shows in several rows (a tree node under two
// parents), whose keys tell them apart. Without multi, every pick replaces.
export default function useSelection<Id extends Key>(items: Ref<Record<string, unknown>[]> | ComputedRef<Record<string, unknown>[]>, idField: string, { keyField = idField, multi = true }: { keyField?: string; multi?: MaybeRefOrGetter<boolean> } = {}) {
    const selected: Set<Id> = shallowReactive(new Set())
    // The row the keyboard moves from; the anchor is the one ranges extend from.
    const cursor = shallowRef<Key | null>(null)
    let anchor: Key | null = null

    const getId = (index: number): Id => items.value[index][idField] as Id
    const keyAt = (index: number): Key => items.value[index][keyField] as Key
    const indexOf = (key: Key): number => items.value.findIndex(item => item[keyField] === key)

    // Selects the rows from index a to index b, either way round, alone.
    function selectRange(a: number, b: number): void {
        selected.clear()
        for (let i = Math.min(a, b); i <= Math.max(a, b); i++) selected.add(getId(i))
    }

    function replace(key: Key): void {
        selected.clear()
        selected.add(getId(indexOf(key)))
        anchor = key
        cursor.value = key
    }

    /**
     * A pointer's pick of a row: Shift selects the range from the anchor,
     * Ctrl/Cmd toggles the row — as Shift does with no anchor in the list —
     * a plain pick selects it alone.
     */
    function pick(key: Key, e: MouseEvent): void {
        const multiple = toValue(multi)
        const from = multiple && e.shiftKey && anchor !== null ? indexOf(anchor) : -1
        if (from !== -1) {
            selectRange(from, indexOf(key))
            cursor.value = key
        } else if (multiple && (e.shiftKey || e.ctrlKey || e.metaKey)) {
            const id = getId(indexOf(key))
            if (selected.has(id)) selected.delete(id)
            else selected.add(id)
            anchor = key
            cursor.value = key
        } else {
            replace(key)
        }
    }

    /**
     * Ctrl/Cmd+A selects all; the arrows move the cursor, selecting its row
     * alone, or with Shift the range from the anchor to it. Returns the
     * cursor's new index, for the host to scroll into view, or -1.
     */
    function onKeyDown(e: KeyboardEvent): number {
        if (toValue(multi) && (e.ctrlKey || e.metaKey) && e.key === 'a') {
            e.preventDefault()
            for (let i = 0; i < items.value.length; i++) selected.add(getId(i))
            return -1
        }
        const step = arrowSteps[e.key]
        if (step === undefined) return -1
        e.preventDefault()
        if (!items.value.length) return -1
        const at = cursor.value === null ? -1 : indexOf(cursor.value)
        const next = at === -1 ? 0 : Math.min(Math.max(at + step, 0), items.value.length - 1)
        if (!toValue(multi) || !e.shiftKey) {
            replace(keyAt(next))
            return next
        }
        if (anchor === null || indexOf(anchor) === -1) anchor = keyAt(at === -1 ? next : at)
        selectRange(indexOf(anchor), next)
        cursor.value = keyAt(next)
        return next
    }

    return {
        selected,
        cursor,
        getId,
        replace,
        pick,
        onKeyDown
    }
}
