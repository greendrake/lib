import { ref, type Ref, type ComputedRef } from 'vue'
import useSelection from './useSelection'
import useSelectionBand from './useSelectionBand'
import useRowReorder, { interactive } from './useRowReorder'

// Pointer-driven selection for a <table> host: row click/ctrl/shift selection
// (via useSelection), rubber-band drag selection (via useSelectionBand), and —
// when onReorder is provided — drag-to-reorder of rows (via useRowReorder). The
// host binds containerRef to the table's container and wires
// onPointerDown/onKeyDown.
export default function RowSelect<Id extends string | number>(items: Ref<Record<string, unknown>[]> | ComputedRef<Record<string, unknown>[]>, idField: string, onReorder?: (orderedIds: Id[]) => void) {
    const sel = useSelection<Id>(items, idField)
    const { selected, getId, selectOnly } = sel
    const containerRef = ref<HTMLElement | null>(null)
    const {
        reorder,
        rows,
        rowAt,
        start: startReorder,
        marks
    } = useRowReorder(
        containerRef,
        () => items.value,
        reordered => onReorder!(reordered.map(item => item[idField] as Id))
    )
    // The items' rows, each by its item's id.
    const { band, start: startBand } = useSelectionBand<Id>(containerRef, () => rows().map((row, i): [Id, Element] => [getId(i), row]))

    function onPointerDown(e: PointerEvent): void {
        const target = e.target as HTMLElement
        if (target.closest(interactive)) return

        e.preventDefault()
        containerRef.value?.focus()

        const rowIndex = rowAt(target)
        const id = rowIndex >= 0 ? getId(rowIndex) : null

        const preClick = new Set(selected)
        if (id !== null) {
            sel.pick(id, e)
            // A Shift pick extends the selection only: no drag follows.
            if (e.shiftKey) return
        }

        if (id !== null && onReorder) {
            // A drag moves the row and leaves the selection as the press found it.
            startReorder(e, rowIndex, () => selectOnly(preClick))
        } else {
            // The band selects the rows it touches alone.
            startBand(e, selectOnly)
        }
    }

    return {
        selected,
        containerRef,
        band,
        reorder,
        marks,
        onPointerDown,
        onKeyDown: sel.onKeyDown
    }
}
