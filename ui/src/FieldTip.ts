// The declaration has to reach every program that compiles this file — this
// package's own check, a sibling package, an app, an outside consumer — and a
// path reference is what travels with the source. The rule's advice does not
// apply: an `import` cannot carry an ambient wildcard module declaration.
// eslint-disable-next-line @typescript-eslint/triple-slash-reference
/// <reference path="./styles.d.ts" />
import { watch, onUnmounted, type Ref, type ComponentPublicInstance } from 'vue'
import { useTippy } from 'vue-tippy'
// Self-contained box/arrow styling for these tips, scoped to the `fieldTip` theme
// applied below (the app ships no global tippy.js base stylesheet).
import './FieldTip.scss'

type Tooltip = ReturnType<typeof useTippy>

const getElement = (ref: string | Ref<HTMLElement | ComponentPublicInstance | null>): HTMLElement | null => {
    if (typeof ref === 'string') {
        return document.getElementById(ref)
    }
    const val = ref.value
    if (val === null) {
        return null
    }
    return '$el' in val ? (val.$el as HTMLElement) : val
}

// Binds a manually-triggered tippy tooltip to a field element, driven by a
// reactive content source: setting it to a string shows the tip (as text —
// tip messages are plain strings by contract), setting it to false hides it.
// The tip also resets the source to false when dismissed by the user, so the
// field can re-trigger it later.
export default (toWatchRef: Ref<string | false>, targetRef: string | Ref<HTMLElement | ComponentPublicInstance | null>): void => {
    let tooltip: Tooltip | undefined

    onUnmounted(() => {
        if (tooltip && !tooltip.state.value.isDestroyed) {
            tooltip.destroy()
        }
    })

    watch(toWatchRef, content => {
        const el = getElement(targetRef)
        if (!content && tooltip) {
            tooltip.destroy()
        } else if (content && el) {
            tooltip = useTippy(el, {
                content,
                theme: 'fieldTip',
                trigger: 'manual',
                interactive: true,
                showOnCreate: true,
                onHidden: () => {
                    toWatchRef.value = false
                }
            })
        }
    })
}
