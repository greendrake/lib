<script lang="ts">
export interface TabDef {
    id: string
    label: string
    closable?: boolean
}
</script>
<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
    defineProps<{
        tabs: TabDef[]
        modelValue: string | null
        closable?: boolean
    }>(),
    {
        closable: false
    }
)

defineEmits<{
    'update:modelValue': [id: string]
    close: [id: string]
}>()

const activeIndex = computed(() => props.tabs.findIndex(t => t.id === props.modelValue))
</script>
<template>
    <div class="TabBar" :style="{ '--TabBar-active-index': activeIndex }">
        <div v-for="(tab, index) in tabs" :key="tab.id" class="TabBar__tab" :class="{ 'TabBar__tab--active': tab.id === modelValue }" :style="{ '--TabBar-tab-index': index }" @click="$emit('update:modelValue', tab.id)">
            <span class="TabBar__tab-label">{{ tab.label }}</span>
            <button v-if="tab.closable ?? closable" class="TabBar__tab-close" @click.stop="$emit('close', tab.id)">&times;</button>
        </div>
        <div v-if="$slots.default" class="TabBar__panel">
            <slot />
        </div>
    </div>
</template>
<style lang="scss">
@use '@greendrake/scss-kit' as *;

.TabBar {
    display: flex;

    &__tab {
        display: flex;
        align-items: center;
        gap: 6px;
        padding: 6px 12px;
        cursor: default;

        &:hover {
            background-color: var(--form-background-hover);
        }
    }

    &__panel {
        padding: var(--space-3) var(--space-2);
    }

    &__tab-label {
        white-space: nowrap;
    }

    &__tab-close {
        display: flex;
        align-items: center;
        justify-content: center;
        width: 16px;
        height: 16px;
        padding: 0;
        border: none;
        background: transparent;
        font-size: 14px;
        line-height: 1;
        cursor: default;
        opacity: 0.5;
        border-radius: 3px;

        &:hover {
            opacity: 1;
            background-color: var(--border-color-light);
        }
    }

    @include respond-above('narrow') {
        flex-wrap: wrap;
        border-bottom: 1px solid var(--border-color-light);

        // When a panel slot is present, the underline moves to the panel's
        // top edge so it sits between the tab row and the panel instead of
        // under everything.
        &:has(&__panel) {
            border-bottom: none;
        }

        &__tab {
            border-bottom: 2px solid transparent;

            &--active {
                border-bottom-color: var(--accent-color);
            }
        }

        &__panel {
            flex-basis: 100%;
            border-top: 1px solid var(--border-color-light);
        }
    }

    @include respond-below('narrow') {
        flex-direction: column;

        &__tab {
            padding: 12px 14px;
            border-inline-start: 3px solid transparent;
            border-block-start: 1px solid var(--border-color-light);
            order: calc(var(--TabBar-tab-index) * 2);

            &:not(:has(~ .TabBar__tab)) {
                border-block-end: 1px solid var(--border-color-light);
            }

            // Expand/collapse caret — an accordion idiom. The right-chevron used
            // before reads as "drill into a subpage", but the section expands
            // inline. The caret points down when collapsed and flips up on the
            // open section (so the expanded row keeps a visible affordance).
            &::after {
                content: '\25BE';
                margin-inline-start: auto;
                line-height: 1;
                opacity: 0.5;
                transition: transform 0.15s;
            }

            &--active {
                border-inline-start-color: var(--accent-color);
                background-color: var(--form-background-hover);

                &::after {
                    transform: rotate(180deg);
                    opacity: 0.9;
                }
            }
        }

        &__panel {
            order: calc(var(--TabBar-active-index) * 2 + 1);
            // Tie the expanded content to its header: continue the active tab's
            // accent edge and tint so the open section reads as one unit rather
            // than free-floating content below the list.
            border-inline-start: 3px solid var(--accent-color);
            background-color: color-mix(in srgb, var(--form-background-hover) 40%, transparent);
        }
    }
}
</style>
