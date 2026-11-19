import { reactive, type Ref } from 'vue'
import usePointerDrag from './usePointerDrag'

/** Where a selection band is drawn, in its container's content coordinates: those an absolutely positioned child of the container is placed by, scrolling with its content. */
export interface SelectionBandState {
    active: boolean
    left: number
    top: number
    width: number
    height: number
}

// Rubber-band selection over a container's items. `start` takes a press of
// the primary mouse or pen button within the container's client area (a touch
// scrolls instead, as a press on its scrollbar does): once it has moved past
// the drag threshold, the band spans from the press to the pointer, and each
// move hands `onSelect` the ids of the items whose boxes the band touches, in
// the order `items` lists them. The band lasts until the button is released.
export default function useSelectionBand<Id>(containerRef: Ref<HTMLElement | null>, items: () => [Id, Element][]) {
    const band: SelectionBandState = reactive({
        active: false,
        left: 0,
        top: 0,
        width: 0,
        height: 0
    })
    const follow = usePointerDrag()

    function start(e: PointerEvent, onSelect: (ids: Id[]) => void): void {
        const container = containerRef.value!
        const box = container.getBoundingClientRect()
        // Where the press is in the client area.
        const inX = e.clientX - box.left - container.clientLeft
        const inY = e.clientY - box.top - container.clientTop
        if (inX < 0 || inX >= container.clientWidth || inY < 0 || inY >= container.clientHeight) return
        const fromX = inX + container.scrollLeft
        const fromY = inY + container.scrollTop

        follow(e, {
            move: me => {
                // What turns a client point into content coordinates, as the
                // container stands now.
                const now = container.getBoundingClientRect()
                const x = container.scrollLeft - now.left - container.clientLeft
                const y = container.scrollTop - now.top - container.clientTop
                const toX = me.clientX + x
                const toY = me.clientY + y
                Object.assign(band, {
                    active: true,
                    left: Math.min(fromX, toX),
                    top: Math.min(fromY, toY),
                    width: Math.abs(toX - fromX),
                    height: Math.abs(toY - fromY)
                })
                const touched = items().filter(([, el]) => {
                    const r = el.getBoundingClientRect()
                    return r.right + x > band.left && r.left + x < band.left + band.width && r.bottom + y > band.top && r.top + y < band.top + band.height
                })
                onSelect(touched.map(([id]) => id))
            },
            end: () => (band.active = false)
        })
    }

    return { band, start }
}
