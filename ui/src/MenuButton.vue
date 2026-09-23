<script lang="ts">
// Placed by offsets rather than by a transform. A transformed element is the
// containing block for every position:fixed descendant, and a menu row may
// carry a control that places its own layer against the viewport (SelectBox's
// dropdown) — under the default translate that layer lands offset by the
// popper's own displacement, which on a bottom-anchored menu is most of the
// screen. Nothing else about the placement changes: Popper writes the same
// coordinates as top/left.
const popperOptions = { modifiers: [{ name: 'computeStyles', options: { gpuAcceleration: false } }] }

const appendTo = () => document.body
</script>
<script setup lang="ts">
import { computed } from 'vue'
import { Tippy } from 'vue-tippy'

interface TippyInstance {
    popper: HTMLElement
    reference: Element
    hide: () => void
}

const props = withDefaults(
    defineProps<{
        // Class(es) on the menu's content wrapper (`.Menubutton-Menu`); scope
        // per-instance *menu-item* styling here.
        menuClass?: string
        // Class(es) on the tippy box itself (the themed `.tippy-box`); scope
        // per-instance *box-level* styling here (overriding the shared HMenu
        // padding/min-width/background). Applied once on create — treat as static.
        boxClass?: string
        offset?: [number, number]
        // Pin the popper's width to the trigger element's measured width on
        // each open. Useful when the trigger is a styled box (e.g. AuthButton)
        // and the menu should sit flush under it.
        matchTriggerWidth?: boolean
    }>(),
    {
        menuClass: '',
        boxClass: '',
        offset: () => [0, -1] as [number, number],
        matchTriggerWidth: false
    }
)

const menuClasses = computed(() => ['Menubutton-Menu', props.menuClass].filter(Boolean))

// The themed box element — tippy nests it as the popper's only child.
const getBox = (instance: TippyInstance) => instance.popper.firstElementChild as HTMLElement | null

const onCreate = (instance: TippyInstance) => {
    instance.popper.addEventListener('click', () => instance.hide())
    if (props.boxClass) getBox(instance)?.classList.add(...props.boxClass.split(' ').filter(Boolean))
}
const onShow = (instance: TippyInstance) => {
    if (!props.matchTriggerWidth) return
    // The HMenu theme applies min-width: 100px to tippy-box, which would
    // override a narrower trigger. Set both width and min-width on the box so
    // the override holds.
    const box = getBox(instance)
    if (!box) return
    const w = `${(instance.reference as HTMLElement).offsetWidth}px`
    box.style.width = w
    box.style.minWidth = w
}
</script>
<template>
    <Tippy :append-to="appendTo" :aria="{ content: null, expanded: false }" :arrow="false" theme="HMenu" :interactive="true" :offset="props.offset" :popper-options="popperOptions" placement="bottom-end" trigger="mouseenter click" :on-create="onCreate" :on-show="onShow">
        <slot name="main" />
        <template #content>
            <div :class="menuClasses">
                <slot name="menuitems" />
            </div>
        </template>
    </Tippy>
</template>
<style lang="scss">
.Menubutton-Menu {
    a {
        display: flex;
        align-items: center;
    }
}
.tippy-box[data-theme~='HMenu'] {
    opacity: 1;
    border-radius: 0;
    padding: 0;
    transition: none;
    background: none;
    .tippy-content {
        padding: 0;
    }
    min-width: 100px;
}
</style>
