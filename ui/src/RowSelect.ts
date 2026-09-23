import { reactive, ref, onUnmounted, type Ref, type ComputedRef } from 'vue'
import useSelection from './useSelection'

const INTERACTIVE = 'a,button,input,select,textarea,[role="button"]'
const DRAG_THRESHOLD = 5

// Pointer-driven selection for a <table> host: row click/ctrl/shift selection
// (via useSelection), rubber-band drag selection over empty space, and — when
// onReorder is provided — drag-to-reorder of rows. The host binds containerRef
// to the table's scroll container and wires onPointerDown/onKeyDown.
export default function RowSelect<Id extends string | number>(items: Ref<Record<string, unknown>[]> | ComputedRef<Record<string, unknown>[]>, idField: string, onReorder?: (orderedIds: Id[]) => void) {
    const sel = useSelection<Id>(items, idField)
    const { selected, getId } = sel
    const containerRef = ref<HTMLElement | null>(null)
    const band = reactive({
        active: false,
        left: 0,
        top: 0,
        width: 0,
        height: 0
    })
    const reorder = reactive({
        active: false,
        fromIndex: -1,
        toIndex: -1
    })

    let startX = 0
    let startY = 0
    let dragStarted = false
    let preClickSnapshot: Set<Id> | null = null
    let cleanupDrag: (() => void) | null = null

    function getIntersectingIds(top: number, bottom: number): Id[] {
        const tbody = containerRef.value?.querySelector('tbody')
        if (!tbody) return []
        const rows = tbody.children
        const ids: Id[] = []
        for (let i = 0; i < rows.length && i < items.value.length; i++) {
            const r = rows[i].getBoundingClientRect()
            if (r.bottom > top && r.top < bottom) ids.push(getId(i))
        }
        return ids
    }

    function hitIndex(target: HTMLElement): number {
        const tbody = containerRef.value?.querySelector('tbody')
        if (!tbody) return -1
        const tr = target.closest('tr')
        if (!tr || tr.parentElement !== tbody) return -1
        const index = Array.prototype.indexOf.call(tbody.children, tr)
        return index >= 0 && index < items.value.length ? index : -1
    }

    function computeDropIndex(clientY: number): number {
        const tbody = containerRef.value!.querySelector('tbody')!
        const rows = tbody.children
        for (let i = 0; i < rows.length && i < items.value.length; i++) {
            const r = rows[i].getBoundingClientRect()
            if (clientY < (r.top + r.bottom) / 2) return i
        }
        return items.value.length
    }

    function armBandDrag(): void {
        const onMove = (me: PointerEvent) => {
            if (!dragStarted && Math.abs(me.clientX - startX) < DRAG_THRESHOLD && Math.abs(me.clientY - startY) < DRAG_THRESHOLD) return

            if (!dragStarted) {
                dragStarted = true
                selected.clear()
            }

            const cr = containerRef.value!.getBoundingClientRect()
            band.left = Math.min(startX, me.clientX) - cr.left
            band.top = Math.min(startY, me.clientY) - cr.top
            band.width = Math.abs(me.clientX - startX)
            band.height = Math.abs(me.clientY - startY)
            band.active = true

            const bandTop = Math.min(startY, me.clientY)
            const bandBottom = Math.max(startY, me.clientY)
            selected.clear()
            for (const rid of getIntersectingIds(bandTop, bandBottom)) selected.add(rid)
        }

        const onUp = () => {
            document.removeEventListener('pointermove', onMove)
            document.removeEventListener('pointerup', onUp)
            cleanupDrag = null
            band.active = false
            dragStarted = false
        }

        document.addEventListener('pointermove', onMove)
        document.addEventListener('pointerup', onUp)
        cleanupDrag = onUp
    }

    function armReorderDrag(fromIndex: number): void {
        const onMove = (me: PointerEvent) => {
            if (!dragStarted && Math.abs(me.clientX - startX) < DRAG_THRESHOLD && Math.abs(me.clientY - startY) < DRAG_THRESHOLD) return

            if (!dragStarted) {
                dragStarted = true
                // Restore selection state from before the tentative click
                if (preClickSnapshot) {
                    selected.clear()
                    for (const id of preClickSnapshot) selected.add(id)
                    preClickSnapshot = null
                }
                reorder.fromIndex = fromIndex
                reorder.active = true
            }

            reorder.toIndex = computeDropIndex(me.clientY)
        }

        const onUp = () => {
            document.removeEventListener('pointermove', onMove)
            document.removeEventListener('pointerup', onUp)
            cleanupDrag = null

            if (reorder.active) {
                const from = reorder.fromIndex
                let to = reorder.toIndex
                reorder.active = false
                reorder.fromIndex = -1
                reorder.toIndex = -1

                if (to > from) to--
                if (from !== to) {
                    const ids = items.value.map(item => item[idField] as Id)
                    const [moved] = ids.splice(from, 1)
                    ids.splice(to, 0, moved)
                    onReorder!(ids)
                }
            }

            dragStarted = false
        }

        document.addEventListener('pointermove', onMove)
        document.addEventListener('pointerup', onUp)
        cleanupDrag = onUp
    }

    function onPointerDown(e: PointerEvent): void {
        const target = e.target as HTMLElement
        if (target.closest(INTERACTIVE)) return

        e.preventDefault()
        containerRef.value?.focus()

        const rowIndex = hitIndex(target)
        const id = rowIndex >= 0 ? getId(rowIndex) : null

        if (id !== null && e.shiftKey) {
            sel.rangeTo(id)
            return
        }

        preClickSnapshot = new Set(selected)

        if (id !== null) {
            if (e.ctrlKey || e.metaKey) {
                sel.toggle(id)
            } else {
                sel.replace(id)
            }
        }

        startX = e.clientX
        startY = e.clientY
        dragStarted = false

        if (id !== null && onReorder) {
            armReorderDrag(rowIndex)
        } else {
            armBandDrag()
        }
    }

    function onKeyDown(e: KeyboardEvent): void {
        if ((e.ctrlKey || e.metaKey) && e.key === 'a') {
            e.preventDefault()
            sel.selectAll()
            return
        }

        const isUp = e.key === 'ArrowUp'
        const isDown = e.key === 'ArrowDown'
        if (!isUp && !isDown) return

        e.preventDefault()
        sel.moveCursor(isUp ? 'up' : 'down', e.shiftKey)
    }

    onUnmounted(() => {
        if (cleanupDrag) cleanupDrag()
    })

    return {
        selected,
        containerRef,
        band,
        reorder,
        onPointerDown,
        onKeyDown
    }
}
