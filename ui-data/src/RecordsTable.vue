<script lang="ts">
import type { ColumnDefinition } from './InfiniteScrollTable.types'

// A column of a records table: a table's column as far as a static table
// renders one — the field, its heading, the cell's text and class.
export type RecordColumn<T> = Pick<ColumnDefinition<T>, 'field' | 'label' | 'formatter' | 'cellClass'>
</script>
<script setup lang="ts" generic="T extends Record<string, unknown>">
// A record's records of a kind — a payment attempt's refunds, a certificate's
// access log — all of them at once, a row each marked as testid, its status
// (where it has one) the row's data-status; nothing for none. A row's key is
// rowKey's, given the row and where it stands: records with no identity of
// their own, as a log's, go by their place.
defineProps<{ rows: T[]; columns: RecordColumn<T>[]; rowKey: (row: T, index: number) => string; testid: string }>()
</script>
<template>
    <table v-if="rows.length" class="RecordsTable">
        <thead>
            <tr>
                <th v-for="column in columns" :key="column.field">{{ column.label }}</th>
            </tr>
        </thead>
        <tbody>
            <tr v-for="(row, index) in rows" :key="rowKey(row, index)" :data-testid="testid" :data-status="row.status">
                <td v-for="column in columns" :key="column.field" :class="column.cellClass?.(row[column.field], row)">{{ column.formatter ? column.formatter(row[column.field], row) : row[column.field] }}</td>
            </tr>
        </tbody>
    </table>
</template>
<style lang="scss">
.RecordsTable {
    width: 100%;
    margin-top: var(--space-2);
    border-collapse: collapse;
    font-size: var(--font-xs);
    th,
    td {
        padding: var(--space-1);
        text-align: start;
        border-bottom: 1px solid var(--border-color-light);
        word-break: break-all;
    }
}
</style>
