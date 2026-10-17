<script setup lang="ts" generic="T">
import { messages } from '@greendrake/ui'
import type { DetailLoader } from './useDetailLoader'

// A detail pane's record as it loads: the loading line, the error the load
// ended in, or — loaded — the default slot, given the record as `data`.
defineProps<{ loader: DetailLoader<T> }>()
defineSlots<{ default(props: { data: T }): unknown }>()
</script>
<template>
    <div v-if="loader.loading.value">{{ messages.loading }}…</div>
    <div v-else-if="loader.error.value" class="DetailPane__error">{{ loader.error.value }}</div>
    <slot v-else-if="loader.data.value !== null" :data="loader.data.value" />
</template>
<style lang="scss">
.DetailPane__error {
    color: var(--error-color);
}
</style>
