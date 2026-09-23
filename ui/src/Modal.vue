<template>
    <Teleport to="body">
        <div ref="rootEl" class="Modal" :class="{ 'Modal--expanded': expanded }" v-bind="$attrs">
            <!-- tabindex="-1" is the stack's fallback focus target when the modal
                 holds no focusable content — see modalStack focus confinement. -->
            <div ref="windowEl" class="Modal__window" role="dialog" aria-modal="true" :aria-label="title" tabindex="-1" @touchstart="onTouchStart" @touchmove="onTouchMove" @touchend="onTouchEnd" @touchcancel="resetDrag">
                <!-- Titled header with an explicit close — the sheet idiom
                     (F14): every modal states what it is and how to leave. -->
                <div v-if="title" class="Modal__header">
                    <span class="Modal__title">{{ title }}</span>
                    <button class="Modal__toolbar-btn close" :aria-label="messages.close" @click="onCancel" />
                </div>
                <div v-if="closable || expandable" class="Modal__toolbar">
                    <button v-if="expandable" class="Modal__toolbar-btn" :class="expanded ? 'collapse' : 'expand'" @click="expanded = !expanded" />
                    <button v-if="closable" class="Modal__toolbar-btn close" @click="onCancel" />
                </div>
                <div class="Modal__main" :class="mainClass">
                    <slot />
                </div>
                <div v-if="buttons" class="Modal__buttons">
                    <slot name="buttons">
                        <button v-if="!hideCancel" class="cancel" @click="onCancel">
                            <slot name="cancel">{{ cancelLabel ?? messages.cancel }}</slot>
                        </button>
                        <button v-if="!hideOk" class="ok" :disabled="okDisabled" @click="onOK">
                            <slot name="ok">{{ okLabel ?? messages.ok }}</slot>
                        </button>
                    </slot>
                </div>
            </div>
        </div>
    </Teleport>
</template>
<script lang="ts">
// Drag-to-dismiss, the gesture half of the bottom-sheet idiom the narrow
// presentation adopts below. A downward drag past either bound closes the
// sheet: a quarter of its own height, or — however short — a parting flick
// faster than this, which is how a decisive flick reads as dismissal rather
// than as a nudge that springs back.
const DISMISS_TRAVEL = 0.25
const DISMISS_VELOCITY = 0.5 // px/ms
// A drag beginning inside a control that reads drags itself — caret placement,
// text selection — belongs to that control, not to the sheet.
const OWN_DRAG = 'input, textarea, select, [contenteditable]'

// A touch the sheet is following, or still deciding on: where it started, where
// it last was and when (the parting velocity comes from that segment), how far
// the sheet currently stands from home, and whether the touch has yet resolved
// into a drag this sheet owns rather than a scroll the content owns.
interface SheetDrag {
    startY: number
    lastY: number
    lastAt: number
    by: number
    velocity: number
    owned: boolean
}
</script>
<script setup lang="ts">
import { onMounted, onUnmounted, provide, ref } from 'vue'
import { mediaBelow } from '@greendrake/scss-kit/breakpoints'
import { TOUCH_PAN_SLOP_PX } from './touchSlop'
import { registerModal, type InitialFocus } from './modalStack'
import { inModalKey } from './inModal'
import { messages } from './messages'

defineOptions({ inheritAttrs: false })

provide(inModalKey, true)

const props = withDefaults(
    defineProps<{
        // Renders a titled header with an explicit close affordance.
        title?: string
        closable?: boolean
        expandable?: boolean
        buttons?: boolean
        mainClass?: string | Record<string, boolean>
        okLabel?: string
        cancelLabel?: string
        okDisabled?: boolean
        hideOk?: boolean
        hideCancel?: boolean
        // 'dialog' for a modal whose fields belong to options inside it rather
        // than to the modal's purpose: opening then leaves them untouched
        // instead of summoning a soft keyboard over the choice being made.
        initialFocus?: InitialFocus
    }>(),
    {
        closable: false,
        expandable: false,
        initialFocus: 'field',
        buttons: true,
        okDisabled: false,
        hideOk: false,
        hideCancel: false
    }
)

const emit = defineEmits<{
    cancel: []
    ok: []
}>()

const expanded = ref(false)

const onCancel = () => emit('cancel')
const onOK = () => emit('ok')

// Escape/popstate close the top-most open modal only, and the stack confines
// focus to it (background inert, Tab wrap, focus restore) — see modalStack.
let unregister: () => void
onMounted(() => {
    unregister = registerModal(onCancel, rootEl.value, props.initialFocus)
})
onUnmounted(() => unregister())

// The teleported root element, for hosts that need to attach native listeners
// to the modal surface (e.g. Gallery's swipe handling).
const rootEl = ref<HTMLElement | null>(null)
defineExpose({ rootEl })

// The sheet under the finger. Null whenever no drag owns the current touch —
// which is every touch outside the sheet presentation, and every one the checks
// in onTouchStart hand back.
const windowEl = ref<HTMLElement | null>(null)
let drag: SheetDrag | null = null

const resetDrag = (): void => {
    drag = null
    // Clearing the inline transition hands the element back to the stylesheet's,
    // which is what springs it home from wherever the finger left it.
    Object.assign(windowEl.value!.style, { transition: '', transform: '' })
}

const onTouchStart = (e: TouchEvent): void => {
    resetDrag()
    // A second finger makes this a pinch, which is the content's gesture.
    if (e.touches.length > 1) return
    // Only the narrow presentation is a sheet; a centred dialog has no edge to
    // be pushed off (the respond-below block below is the same boundary).
    if (!matchMedia(mediaBelow('narrow')).matches) return
    const target = e.target as HTMLElement
    if (target.closest(OWN_DRAG)) return
    // The browser's own scroll chaining, in the one direction that matters: the
    // drag is the sheet's only if nothing between the touch and the sheet has
    // room to scroll back up first. Anything unscrollable sits at 0 and passes.
    for (let n: HTMLElement | null = target; n && n !== windowEl.value!.parentElement; n = n.parentElement) {
        if (n.scrollTop > 0) return
    }
    const { clientY } = e.touches[0]
    drag = {
        startY: clientY,
        lastY: clientY,
        lastAt: e.timeStamp,
        by: 0,
        velocity: 0,
        owned: false
    }
}

const onTouchMove = (e: TouchEvent): void => {
    if (!drag) return
    const y = e.touches[0].clientY
    const by = y - drag.startY
    if (!drag.owned) {
        // Which gesture this is only becomes readable once the touch has
        // travelled the platform's slop: jitter at the start of a scroll would
        // otherwise decide it.
        if (Math.abs(by) < TOUCH_PAN_SLOP_PX) return
        // Upward — the content is what the user means to move, so hand the
        // whole touch back and let it scroll untouched.
        if (by < 0) return resetDrag()
        drag.owned = true
    }
    // Owning the drag means owning what the browser would otherwise do with it:
    // rubber-band the sheet's own overscroll.
    e.preventDefault()
    const elapsed = e.timeStamp - drag.lastAt
    // Velocity from the latest segment only — a slow drag that ends in a flick
    // is a dismissal, and an average over the whole gesture would miss it.
    if (elapsed > 0) drag.velocity = (y - drag.lastY) / elapsed
    drag.lastY = y
    drag.lastAt = e.timeStamp
    // Dragging back past the start does not lift the sheet above its resting
    // edge; it just returns it there.
    drag.by = Math.max(0, by)
    Object.assign(windowEl.value!.style, { transition: 'none', transform: `translateY(${drag.by}px)` })
}

const onTouchEnd = (): void => {
    if (!drag) return
    const dismissed = drag.owned && (drag.by > windowEl.value!.offsetHeight * DISMISS_TRAVEL || drag.velocity > DISMISS_VELOCITY)
    resetDrag()
    if (dismissed) onCancel()
}
</script>
<style lang="scss" scoped>
@use '@greendrake/scss-kit' as *;
.Modal {
    position: fixed;
    top: 0;
    bottom: 0;
    left: 0;
    right: 0;
    z-index: z('modal');
    display: flex;
    justify-content: center;
    align-items: center;

    // Narrow viewports get the bottom-sheet presentation: full-width window
    // pinned to the bottom edge, rounded top corners, internal scroll. One
    // rule here converts every modal in the app to the mobile idiom (F14).
    @include respond-below('narrow') {
        align-items: flex-end;

        .Modal__window {
            width: 100%;
            max-width: none;
            max-height: calc(100dvh - var(--space-5));
            overflow-y: auto;
            border-radius: calc(var(--modal-border-radius) * 3) calc(var(--modal-border-radius) * 3) 0 0;
            padding-bottom: var(--safe-area-inset-bottom, 0px);
            // Drag-to-dismiss moves this element directly, suppressing the
            // transition while the finger is down; what is left for the
            // transition is the spring home from a drag released short of
            // dismissal.
            transition: transform 0.2s ease-out;
        }
    }

    &::before {
        position: absolute;
        content: '';
        top: 0;
        bottom: 0;
        left: 0;
        right: 0;
        backdrop-filter: var(--modal-backdrop-filter, none);
    }

    &__main {
        padding: var(--space-4);
        flex-grow: 1;
        :slotted(h2) {
            margin: 0;
            text-align: center;
        }
        // Baseline form layout for a <form> in the modal's main slot. Without
        // this, default flow renders label and input side-by-side and they
        // visually collide inside the narrow modal width. Stack them, give
        // inputs full width, space sibling rows, and hide the redundant
        // submit button (Modal renders its own OK that drives @ok — the
        // submit button is kept in markup only for Enter-to-submit semantics).
        :slotted(form) > div:not(:first-child) {
            margin-top: var(--space-2);
        }
        :slotted(form) label {
            display: block;
        }
        :slotted(form) input[type='text'],
        :slotted(form) input[type='email'],
        :slotted(form) input[type='password'],
        :slotted(form) textarea,
        :slotted(form) .Password {
            width: 100%;
        }
        :slotted(form) > button[type='submit'] {
            display: none;
        }
    }

    &__window {
        position: relative;
        border-radius: var(--modal-border-radius);
        z-index: z('raised');
        background-color: var(--modal-background);
        box-shadow: var(--modal-box-shadow, none);
        display: flex;
        flex-direction: column;

        &::before {
            border-radius: var(--modal-border-radius);
            position: absolute;
            content: '';
            top: 0;
            bottom: 0;
            left: 0;
            right: 0;
            z-index: z('below');
            backdrop-filter: var(--modal-window-backdrop-filter, none);
        }
    }

    &__header {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: var(--space-2);
        padding: var(--space-3) var(--space-4) 0;
        .Modal__title {
            font-weight: 600;
            font-size: var(--font-lg);
        }
    }

    &__toolbar {
        position: absolute;
        top: 6px;
        right: 6px;
        z-index: z('raised');
        display: flex;
        gap: var(--space-1);
    }

    &__toolbar-btn {
        @include icon;
        // Grow the hit area to the AA minimum; the mask keeps its intrinsic
        // glyph size centred, so only the target — not the icon — enlarges.
        min-width: 24px;
        min-height: 24px;
        padding: 0;
        background-color: var(--font-color);
        opacity: 0.5;
        &:hover {
            opacity: 1;
        }
        &.close {
            @include i('close');
        }
        &.expand {
            @include i('expand');
        }
        &.collapse {
            @include i('collapse');
        }
    }

    &--expanded &__window {
        width: 100vw;
        height: 100vh;
        max-width: 100vw;
        max-height: 100vh;
        border-radius: 0;
    }

    &__buttons {
        padding: 0 var(--space-4) var(--space-4);
        display: flex;
        justify-content: space-between;
        gap: var(--space-4);

        &:has(button:only-child) {
            justify-content: center;
        }

        button {
            min-width: var(--modal-button-min-width, 64px);
            &:disabled {
                @include disabled;
            }
        }
    }
}
</style>
