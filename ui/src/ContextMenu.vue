<script lang="ts">
/** A context menu entry: its label, and what choosing it does. */
export interface ContextMenuItem {
    label: string
    run: () => unknown
}

const appendTo = () => document.body
</script>
<script setup lang="ts">
import { ref } from 'vue'
import { Tippy, type TippyComponent } from 'vue-tippy'

// A menu at the pointer in place of the browser's own: open(event) from a
// contextmenu listener shows it there. Choosing an item closes it and runs
// the item; a press outside it, or any key, closes it.
defineProps<{
    items: ContextMenuItem[]
}>()

const tip = ref<TippyComponent>()
let at = new DOMRect()
const pointer = (): DOMRect => at

// Any key closes the menu while it shows. Unmounted while it shows, it is
// destroyed without hiding: that ends the listening too.
const hide = (): void => tip.value!.hide()
const listen = (): void => document.addEventListener('keydown', hide)
const unlisten = (): void => document.removeEventListener('keydown', hide)

function open(e: MouseEvent): void {
    e.preventDefault()
    at = new DOMRect(e.clientX, e.clientY, 0, 0)
    tip.value!.show()
}

function choose(item: ContextMenuItem): void {
    hide()
    item.run()
}

defineExpose({ open })
</script>
<template>
    <Tippy ref="tip" to="parent" trigger="manual" theme="ContextMenu" role="menu" placement="bottom-start" interactive :append-to="appendTo" :aria="{ content: null, expanded: false }" :arrow="false" :offset="[0, 0]" :duration="0" :get-reference-client-rect="pointer" :on-show="listen" :on-hide="unlisten" :on-destroy="unlisten">
        <button v-for="item in items" :key="item.label" type="button" role="menuitem" class="ContextMenu__item" @click="choose(item)">{{ item.label }}</button>
    </Tippy>
</template>
<style lang="scss">
.tippy-box[data-theme~='ContextMenu'] {
    min-width: 120px;
    padding: var(--space-1) 0;
    border: 1px solid var(--border-color-light);
    background: var(--form-background);
    .tippy-content {
        padding: 0;
    }
}
.ContextMenu__item {
    display: block;
    width: 100%;
    margin: 0;
    padding: var(--space-1) var(--space-3);
    border: none;
    border-radius: 0;
    background: none;
    color: var(--font-color);
    font: inherit;
    text-align: start;
    white-space: nowrap;
    cursor: default;

    &:hover {
        background: var(--form-background-hover);
    }
}
</style>
