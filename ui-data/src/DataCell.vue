<script setup lang="ts" generic="T">
// What a column shows of a record: its component, bound to the props it
// takes of the record, else its value, through its formatter where it has one.
import { valueAt, type ColumnDefinition } from './InfiniteScrollTable.types'

defineProps<{
    column: ColumnDefinition<T>
    record: T
}>()
</script>
<template>
    <component :is="column.component" v-if="column.component" v-bind="column.componentProps?.(record)" />
    <template v-else>{{ column.formatter ? column.formatter(valueAt(record, column.field), record) : valueAt(record, column.field) }}</template>
</template>
