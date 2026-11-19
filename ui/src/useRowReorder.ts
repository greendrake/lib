import { reactive, type Ref } from 'vue'
import usePointerDrag from './usePointerDrag'

/** The elements of a row a press on works rather than drags: links, buttons and form controls. */
export const interactive = 'a,button,input,select,textarea,[role="button"]'

/** A row drag in progress: the dragged row's index, and the gap it would drop into — 0 above the first row, the row count below the last. */
export interface RowReorderState {
    active: boolean
    fromIndex: number
    toIndex: number
}

// Drag-to-reorder of the rows of the <tbody> in a container, one row per item,
// in the items' order. `start` takes a press on the row at `fromIndex`: once
// the pointer has moved past the drag threshold, `onDrag` runs and the state
// follows the gap nearest the pointer. The release hands `onReorder` the items
// in their new order, unless the row is back in its place.
export default function useRowReorder<T>(containerRef: Ref<HTMLElement | null>, items: () => readonly T[], onReorder: (items: T[]) => void) {
    const reorder: RowReorderState = reactive({
        active: false,
        fromIndex: -1,
        toIndex: -1
    })
    const follow = usePointerDrag()

    // The items' rows.
    const rows = (): Element[] => Array.from(containerRef.value!.querySelector('tbody')!.children).slice(0, items().length)

    // The index of the row an element is in, -1 for none.
    function rowAt(target: Element): number {
        const tr = target.closest('tr')
        return tr ? rows().indexOf(tr) : -1
    }

    function start(press: PointerEvent, fromIndex: number, onDrag: () => void = () => {}): void {
        follow(press, {
            move: e => {
                if (!reorder.active) {
                    onDrag()
                    Object.assign(reorder, { active: true, fromIndex })
                }
                const below = rows().findIndex(row => {
                    const r = row.getBoundingClientRect()
                    return e.clientY < (r.top + r.bottom) / 2
                })
                reorder.toIndex = below < 0 ? items().length : below
            },
            end: released => {
                const { toIndex } = reorder
                Object.assign(reorder, {
                    active: false,
                    fromIndex: -1,
                    toIndex: -1
                })
                const to = toIndex > fromIndex ? toIndex - 1 : toIndex
                if (!released || to === fromIndex) return
                const reordered = [...items()]
                reordered.splice(to, 0, ...reordered.splice(fromIndex, 1))
                onReorder(reordered)
            }
        })
    }

    // A press on a row, off its interactive elements, drags the row; it
    // selects no text.
    function onPointerDown(e: PointerEvent): void {
        const target = e.target as Element
        const fromIndex = rowAt(target)
        if (fromIndex < 0 || target.closest(interactive)) return
        e.preventDefault()
        start(e, fromIndex)
    }

    // Row i's marks while a drag lasts: `data-dragged` on the dragged row, and
    // `data-drop` on the row the gap borders, `before` it or, below the last
    // row, `after` it.
    const marks = (i: number) => ({
        'data-dragged': (reorder.active && i === reorder.fromIndex) || undefined,
        'data-drop': !reorder.active ? undefined : i === reorder.toIndex ? 'before' : i === items().length - 1 && reorder.toIndex === items().length ? 'after' : undefined
    })

    return {
        reorder,
        rows,
        rowAt,
        start,
        onPointerDown,
        marks
    }
}
