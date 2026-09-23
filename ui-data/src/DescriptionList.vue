<script lang="ts">
export interface DescriptionItem {
    label: string
    value: string | number | null | undefined
    /** Render the value in a <code> element. */
    code?: boolean
}
</script>
<script setup lang="ts">
import { computed } from 'vue'

// The label/value grid repeated across detail panes. Values arrive
// display-ready (formatted, with any placeholder text already applied);
// conditional rows are inlined as falsy entries: `[cond && { … }]`.
const props = defineProps<{
    items: (DescriptionItem | false | null | undefined)[]
}>()

const rows = computed(() => props.items.filter((item): item is DescriptionItem => !!item))
</script>
<template>
    <dl class="DescriptionList">
        <template v-for="(item, i) in rows" :key="i">
            <dt>{{ item.label }}</dt>
            <dd>
                <code v-if="item.code">{{ item.value ?? '' }}</code>
                <template v-else>{{ item.value ?? '' }}</template>
            </dd>
        </template>
    </dl>
</template>
<style lang="scss" scoped>
.DescriptionList {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--space-1) var(--space-3);
    margin: 0;

    dt {
        font-weight: 600;
    }

    dd {
        margin: 0;
        word-break: break-word;

        code {
            background: var(--form-background);
            padding: 0 var(--space-1);
        }
    }
}
</style>
