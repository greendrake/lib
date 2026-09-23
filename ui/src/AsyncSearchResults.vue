<script lang="ts">
export interface SearchResultItem {
    id: string
    // Rendered via v-html by the default item renderer — hence the explicit
    // name; build it only from trusted/escaped content. A custom itemComponent
    // may render whatever fields the item carries instead.
    // Required by the default renderer (rendered via v-html — supply
    // trusted/escaped markup); irrelevant when the host passes itemComponent.
    htmlTitle?: string
    [key: string]: unknown
}
</script>
<script setup lang="ts">
import type { Component } from 'vue'
import { messages } from './messages'

withDefaults(
    defineProps<{
        data: SearchResultItem[]
        itemComponent?: Component | null
        // The listbox element id the host input's aria-controls points at.
        listboxId?: string
    }>(),
    {
        itemComponent: null,
        listboxId: undefined
    }
)
const emit = defineEmits<{
    pick: [data: SearchResultItem]
}>()
const onClick = (data: SearchResultItem) => emit('pick', data)
</script>
<template>
    <div class="AsyncSearchResults">
        <div v-if="data.length" :id="listboxId" role="listbox" tabindex="0" :aria-label="messages.searchResults">
            <div v-for="d in data" :key="d.id" class="AsyncSearchResults__Result" role="option" :aria-selected="false" @click="onClick(d)">
                <component :is="itemComponent" v-if="itemComponent" :data="d" />
                <span v-else v-html="d.htmlTitle ?? ''" />
            </div>
        </div>
        <!-- Hosts that can do better than "nothing found" — offer near-misses,
             an alert, a way to create what is missing — fill the empty slot. -->
        <slot v-else name="empty">
            <div class="AsyncSearchResults__Result nothingFound">{{ messages.nothingFound }}</div>
        </slot>
        <slot />
    </div>
</template>
<style lang="scss">
.AsyncSearchResults {
    max-height: 200px;
    display: flex;
    border-radius: var(--border-radius);
    flex-direction: column;
    > div:first-child {
        overflow-y: auto;
    }
    &__Result {
        padding: var(--space-2);
        background-color: var(--form-background);
        &:not(:last-child) {
            border-bottom: 1px solid var(--border-color-light);
        }
        cursor: default;
        &:hover {
            background-color: var(--form-background-hover);
        }
        &.nothingFound {
            padding: var(--space-2);
        }
    }
}
</style>
