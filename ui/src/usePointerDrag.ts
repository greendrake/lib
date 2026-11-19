import { onUnmounted } from 'vue'

// How far a press moves before it drags rather than clicks.
const DRAG_THRESHOLD = 5

/** What a drag does: `move` takes each pointer move once the press has moved past the threshold; `end` runs once a drag that began is over, `released` true for a release of the button, false for a cancel or the unmount of the component. */
export interface PointerDragHandlers {
    move: (e: PointerEvent) => void
    end: (released: boolean) => void
}

// The drag a press of the primary mouse or pen button turns into once the
// pointer has moved past the threshold (a touch scrolls instead). It follows
// the pointer over the whole document until the button is released.
export default function usePointerDrag() {
    let stop: ((released: boolean) => void) | null = null

    function follow(press: PointerEvent, { move, end }: PointerDragHandlers): void {
        if (press.button !== 0 || press.pointerType === 'touch') return
        let dragging = false
        const onMove = (e: PointerEvent): void => {
            if (!dragging && Math.abs(e.clientX - press.clientX) < DRAG_THRESHOLD && Math.abs(e.clientY - press.clientY) < DRAG_THRESHOLD) return
            dragging = true
            move(e)
        }
        const onUp = (): void => finish(true)
        const onCancel = (): void => finish(false)
        const finish = (released: boolean): void => {
            document.removeEventListener('pointermove', onMove)
            document.removeEventListener('pointerup', onUp)
            document.removeEventListener('pointercancel', onCancel)
            stop = null
            if (dragging) end(released)
        }
        document.addEventListener('pointermove', onMove)
        document.addEventListener('pointerup', onUp)
        document.addEventListener('pointercancel', onCancel)
        stop = finish
    }

    onUnmounted(() => stop?.(false))

    return follow
}
