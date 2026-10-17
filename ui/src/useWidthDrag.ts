import { computed, shallowRef, toValue, type ComputedRef, type MaybeRefOrGetter } from 'vue'
import { capturePointer } from './capturePointer'

export interface WidthDrag {
    /** Whether a drag is in progress. */
    active: ComputedRef<boolean>
    /** The handle's pointerdown: captures the pointer and drags from `width`, handing each new width to `set`. */
    start(event: PointerEvent, width: number, set: (width: number) => void): void
    /** The handle's pointermove. */
    move(event: PointerEvent): void
    /** The handle's pointerup and pointercancel. */
    end(): void
}

// A width dragged by a handle at its end edge: the width follows the pointer's
// horizontal travel, never below `minWidth`.
export default function useWidthDrag(minWidth: MaybeRefOrGetter<number>): WidthDrag {
    const drag = shallowRef<{ startX: number; startWidth: number; set: (width: number) => void } | null>(null)
    return {
        active: computed(() => drag.value !== null),
        start(event, width, set) {
            event.preventDefault()
            capturePointer(event)
            drag.value = {
                startX: event.clientX,
                startWidth: width,
                set
            }
        },
        move(event) {
            const d = drag.value
            if (d) d.set(Math.max(toValue(minWidth), d.startWidth + event.clientX - d.startX))
        },
        end() {
            drag.value = null
        }
    }
}
