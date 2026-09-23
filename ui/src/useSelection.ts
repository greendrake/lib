import { shallowReactive, type Ref, type ComputedRef } from 'vue'

// Flat, index-addressable multi-selection over an ordered item list: single
// (replace), ctrl-toggle, shift-range from an anchor, keyboard cursor moves,
// select-all. The selected Set holds item ids read from items[i][idField].
export default function useSelection<Id extends string | number>(items: Ref<Record<string, unknown>[]> | ComputedRef<Record<string, unknown>[]>, idField: string) {
    const selected: Set<Id> = shallowReactive(new Set())
    let anchor: Id | null = null
    let cursor: Id | null = null

    function getId(index: number): Id {
        return items.value[index][idField] as Id
    }

    function indexOf(id: Id): number {
        return items.value.findIndex(item => (item[idField] as Id) === id)
    }

    function addRange(fromId: Id, toId: Id): void {
        const a = indexOf(fromId)
        const b = indexOf(toId)
        if (a === -1 || b === -1) return
        const [lo, hi] = a <= b ? [a, b] : [b, a]
        for (let i = lo; i <= hi; i++) selected.add(getId(i))
    }

    function toggle(id: Id): void {
        if (selected.has(id)) selected.delete(id)
        else selected.add(id)
        anchor = id
        cursor = id
    }

    function replace(id: Id): void {
        selected.clear()
        selected.add(id)
        anchor = id
        cursor = id
    }

    function rangeTo(id: Id): void {
        if (anchor === null) {
            toggle(id)
            return
        }
        selected.clear()
        addRange(anchor, id)
        cursor = id
    }

    /**
     * Move cursor up or down.
     * If extend is true (shift held), add to selection.
     * If extend is false, replace selection with single item.
     * Returns the new cursor index (useful for scrolling into view), or -1 if no items.
     */
    function moveCursor(direction: 'up' | 'down', extend: boolean): number {
        if (!items.value.length) return -1
        const max = items.value.length - 1
        const curIdx = cursor !== null ? indexOf(cursor) : -1
        const next = direction === 'up' ? Math.max(0, curIdx <= 0 ? 0 : curIdx - 1) : Math.min(max, curIdx === -1 ? 0 : curIdx + 1)
        const nextId = getId(next)

        if (extend) {
            selected.add(nextId)
            cursor = nextId
            if (anchor === null) anchor = nextId
        } else {
            selected.clear()
            selected.add(nextId)
            anchor = nextId
            cursor = nextId
        }
        return next
    }

    function selectAll(): void {
        for (let i = 0; i < items.value.length; i++) selected.add(getId(i))
    }

    function clear(): void {
        selected.clear()
        anchor = null
        cursor = null
    }

    return {
        selected,
        getId,
        indexOf,
        addRange,
        toggle,
        replace,
        rangeTo,
        moveCursor,
        selectAll,
        clear
    }
}
