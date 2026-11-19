<script setup lang="ts" generic="T">
// A record's columns as a label/value grid: each column's label beside what
// its cell shows of the record, as a table's would. A column whose value the
// record leaves unset (null, undefined or '') is left out.
import { computed } from 'vue'
import DataCell from './DataCell.vue'
import { valueAt, type ColumnDefinition } from './InfiniteScrollTable.types'

const props = defineProps<{
    columns: ColumnDefinition<T>[]
    record: T
}>()

const shown = computed(() =>
    props.columns.filter(column => {
        const value = valueAt(props.record, column.field)
        return value != null && value !== ''
    })
)
</script>
<template>
    <dl class="RecordDetails">
        <template v-for="column in shown" :key="column.field">
            <dt>{{ column.label }}</dt>
            <dd :data-field="column.field"><DataCell :column="column" :record="record" /></dd>
        </template>
    </dl>
</template>
<style lang="scss" scoped>
@use './labelValueGrid' as *;

.RecordDetails {
    @include label-value-grid;
}
</style>
