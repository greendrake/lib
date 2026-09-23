<script lang="ts">
// Extra fields pass through to the section/header slots.
export interface AccordionSection {
    id: string
    title: string
    [key: string]: unknown
}
</script>
<script setup lang="ts">
// Vertical stack of independently-collapsible sections. Multiple sections may be
// open at once (all start open). Each section gets a clickable title (toggles
// open) plus a `header` slot for per-section controls (e.g. a toolbar) that does
// not toggle the section.
import { ref } from 'vue'

const props = defineProps<{
    sections: AccordionSection[]
}>()

const open = ref(new Set(props.sections.map(s => s.id)))

function toggle(id: string) {
    const next = new Set(open.value)
    if (next.has(id)) {
        next.delete(id)
    } else {
        next.add(id)
    }
    open.value = next
}
</script>
<template>
    <div class="Accordion">
        <section v-for="section in sections" :key="section.id" class="Accordion__section">
            <header class="Accordion__header">
                <button type="button" class="Accordion__toggle" :class="{ open: open.has(section.id) }" @click="toggle(section.id)"><span class="Accordion__caret">▶</span>{{ section.title }}</button>
                <div class="Accordion__tools">
                    <slot name="header" :section="section" />
                </div>
            </header>
            <div v-show="open.has(section.id)" class="Accordion__body">
                <slot :section="section" />
            </div>
        </section>
    </div>
</template>
<style lang="scss">
.Accordion {
    &__section:not(:last-child) {
        border-bottom: 1px solid var(--border-color-light);
    }
    &__header {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        padding: var(--space-1) var(--space-2);
    }
    &__toggle {
        flex-grow: 1;
        display: flex;
        align-items: center;
        gap: var(--space-1);
        text-align: start;
        border: none;
        background: transparent;
        cursor: default;
        font-weight: bold;
    }
    &__caret {
        font-size: 9px;
        line-height: 1;
        transition: transform 0.1s ease;
        .Accordion__toggle.open & {
            transform: rotate(90deg);
        }
    }
    &__tools {
        display: flex;
        align-items: center;
        gap: var(--space-1);
        flex-wrap: wrap;
    }
}
</style>
