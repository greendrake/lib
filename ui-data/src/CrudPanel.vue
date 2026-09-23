<script lang="ts">
import type { RowPosition, VisibleRange } from './InfiniteScrollTable.types'

type Key = string | number

// The consumer-facing contract of CrudPanel's template-ref instance. Generic
// SFC instances can't be named via InstanceType (vue-tsc models them as call
// signatures), so consumers type their refs with this instead:
// `ref<CrudPanelExposed<Host>>()`.
export interface CrudPanelExposed<T = Record<string, unknown>> {
    openTab: (id: string, label?: string) => void
    closeTab: (id: string) => void
    updateTab: (id: string, newId?: string, newLabel?: string) => void
    removeRows: (keys: Key[]) => void
    addRow: (data: T, position?: RowPosition) => void
    patchRow: (key: Key, data: Partial<T>) => void
    getRow: (key: Key) => T | undefined
    refresh: (background?: boolean) => Promise<void>
    applySaved: (key: Key, record: T, tabId?: string) => void
    readonly selected: Set<Key> | undefined
    readonly range: VisibleRange
    readonly deleting: boolean
}
</script>
<script setup lang="ts" generic="T extends Record<string, unknown>">
import { ref, computed, useSlots } from 'vue'
import { SearchField, Modal, messages } from '@greendrake/ui'
import InfiniteScrollTable from './InfiniteScrollTable.vue'
import MasterDetail from './MasterDetail.vue'
import { localFilterDebounce } from './localFilterDebounce'
import { rowKeyOf, type ColumnDefinition, type Filters, type LoaderFunction, type RowKey, type InfiniteScrollTableExposed } from './InfiniteScrollTable.types'

// Master/detail CRUD shell: search box + InfiniteScrollTable on the left,
// tabbed detail panes on the right. Opt-in Create / Delete affordances live
// here so dashboards don't reimplement them.
//
// Detail editors follow the record-edit convention (see useRecordEditor):
//   1. The editor receives the row it's editing (via slot prop `record` —
//      sourced from the active table row).
//   2. On save it sends only the diff, gets the full updated record back,
//      and emits ('saved', key, record).
//   3. The parent wires that event to `crudRef.applySaved(key, record,
//      tab.id)` — patching the row in place (or adding a freshly created
//      one). NEVER call `refresh()` after a single-row edit, that's a
//      wasteful round trip.
//
// Slots:
//   - controls-left   — appended to the left of the controls bar, after the
//                       search field and the built-in Create button (if any).
//                       Useful for filters or extra action buttons.
//   - controls-middle — placed in the middle. Receives { range }.
//   - controls-right  — appended to the right of the controls bar, after the
//                       built-in Delete button (if any). Receives
//                       { selected } for custom selection-aware buttons.
//   - detail          — receives { tab, isNew, record, isDupe }.
//                         isNew    — tab.id starts with `new:`.
//                         record   — the active table row (undefined when
//                                    isNew, or when the table hasn't seen
//                                    the row yet).
//                         isDupe   — `(original, candidate) => boolean`,
//                                    backed by the `keyExists` prop, for
//                                    rename / new-tab key collision checks.
const props = withDefaults(
    defineProps<{
        columns: ColumnDefinition<T>[]
        loader: LoaderFunction<T>
        rowKey?: RowKey<T>
        rowHeight?: number
        filters?: Filters
        initialWidth?: number
        minWidth?: number
        fullWidthUntilTab?: boolean
        showRange?: boolean
        // null = table default; false = always filter remotely (see table prop).
        localSearchFields?: string[] | false | null
        // --- Create / Delete affordances (all opt-in) ---
        creatable?: boolean
        newTabLabel?: string
        onDelete?: ((keys: Key[]) => Promise<unknown>) | null
        deleteConfirmMessage?: string | null
        keyExists?: ((key: Key) => boolean) | null
    }>(),
    {
        rowKey: 'id',
        rowHeight: 28,
        filters: () => ({}),
        initialWidth: 200,
        minWidth: 120,
        fullWidthUntilTab: true,
        showRange: true,
        localSearchFields: null,
        creatable: false,
        newTabLabel: undefined,
        onDelete: null,
        deleteConfirmMessage: null,
        keyExists: null
    }
)

const slots = useSlots()
const masterDetailRef = ref<InstanceType<typeof MasterDetail>>()
// Typed against the table's exposed contract rather than its (generic) instance
// type, so T keeps flowing through patchRow/addRow/getRow.
const tableRef = ref<InfiniteScrollTableExposed<T>>()
const filterDebounce = localFilterDebounce(tableRef)

const newTabLabelText = computed(() => props.newTabLabel ?? messages.newTab)

// Activating a row opens a detail tab — but only when there's a detail to show.
// A consumer that provides no `detail` slot (e.g. a glance-and-delete table) gets
// a table-only panel: rows select for deletion and never open an empty pane.
function onRowActivate(record: T): void {
    if (!slots.detail) return
    const key = String(rowKeyOf(record, props.rowKey))
    masterDetailRef.value!.openTab(key, key)
}

const searchQuery = ref<string | undefined>('')
const visibleRange = ref({
    start: 0,
    end: 0,
    total: 0
})

const mergedFilters = computed<Filters>(() => ({
    ...props.filters,
    q: searchQuery.value || undefined
}))
const singleColumn = computed(() => props.columns.length === 1)

function onRangeChanged(range: VisibleRange): void {
    visibleRange.value = range
}

let newTabSeq = 0
const deleting = ref(false)
const deleteConfirm = ref(false)

const canDelete = computed(() => !!props.onDelete && (tableRef.value?.selected?.size ?? 0) > 0)

function onCreate(): void {
    masterDetailRef.value!.openTab(`new:${++newTabSeq}`, newTabLabelText.value)
}

function onRequestDelete(): void {
    if (!canDelete.value || deleting.value) return
    if (props.deleteConfirmMessage) {
        deleteConfirm.value = true
    } else {
        doDelete()
    }
}

async function doDelete(): Promise<void> {
    const sel = tableRef.value?.selected
    if (!sel?.size) return
    const keys = [...sel]
    deleting.value = true
    deleteConfirm.value = false
    try {
        await props.onDelete!(keys)
        for (const k of keys) masterDetailRef.value?.closeTab(String(k))
        tableRef.value?.removeRows(keys)
        tableRef.value?.selected?.clear()
    } finally {
        deleting.value = false
    }
}

// Key collision check used by detail editors when renaming a record or
// creating a new one. `original` is the current key (or '' for new); we
// don't flag a match against the same value.
function isDupe(original: Key, candidate: Key): boolean {
    if (!props.keyExists) return false
    const trimmed = typeof candidate === 'string' ? candidate.trim() : candidate
    return trimmed !== '' && trimmed !== original && props.keyExists(trimmed)
}

// Default post-save bookkeeping for the record-edit convention: patch the row
// in place when it already exists; otherwise add the freshly created row and,
// given the originating `new:` tab's id, rename that tab to the persisted key
// so the editor's `record` slot prop resolves and subsequent saves route to
// update instead of create.
function applySaved(key: Key, record: T, tabId?: string): void {
    if (tableRef.value?.getRow(key)) {
        tableRef.value.patchRow(key, record)
    } else {
        tableRef.value?.addRow(record)
        if (tabId && tabId !== String(key)) {
            masterDetailRef.value?.updateTab(tabId, String(key), String(key))
        }
    }
}

const exposed: CrudPanelExposed<T> = {
    openTab: (id, label) => masterDetailRef.value!.openTab(id, label),
    closeTab: id => masterDetailRef.value!.closeTab(id),
    updateTab: (id, newId, newLabel) => masterDetailRef.value!.updateTab(id, newId, newLabel),
    removeRows: keys => {
        tableRef.value?.removeRows(keys)
        tableRef.value?.selected?.clear()
    },
    addRow: (data, position) => tableRef.value?.addRow(data, position),
    patchRow: (key, data) => tableRef.value?.patchRow(key, data),
    getRow: key => tableRef.value?.getRow(key),
    refresh: background => tableRef.value?.refresh(background) ?? Promise.resolve(),
    applySaved,
    get selected() {
        return tableRef.value?.selected
    },
    get range() {
        return visibleRange.value
    },
    get deleting() {
        return deleting.value
    }
}
defineExpose(exposed)
</script>
<template>
    <div class="CrudPanel">
        <MasterDetail ref="masterDetailRef" :initial-width="initialWidth" :min-width="minWidth" :full-width-until-tab="fullWidthUntilTab">
            <template #master>
                <div class="CrudPanel__controls">
                    <SearchField v-model="searchQuery" :debounce="filterDebounce" :placeholder="messages.search" />
                    <button v-if="creatable" type="button" class="CrudPanel__create" @click="onCreate">{{ messages.create }}</button>
                    <slot name="controls-left" />
                    <div class="CrudPanel__controls__gap" />
                    <slot name="controls-middle" :range="visibleRange" />
                    <button type="button" class="CrudPanel__refresh" :title="messages.refresh" @click="tableRef?.refresh()">↻</button>
                    <div v-if="showRange && visibleRange.total" class="CrudPanel__range">{{ visibleRange.start }}-{{ visibleRange.end }}/{{ visibleRange.total }}</div>
                    <button v-if="onDelete" type="button" class="CrudPanel__delete" :class="{ disabled: !canDelete || deleting, spinner: deleting }" @click="onRequestDelete">{{ messages.delete }}</button>
                    <slot name="controls-right" :selected="tableRef?.selected" />
                    <Modal v-if="deleteConfirm" class="CrudPanel__delete-modal" :title="messages.delete" :ok-label="deleting ? messages.deleting : messages.delete" :ok-disabled="deleting" @ok="doDelete" @cancel="deleteConfirm = false">
                        <p>{{ deleteConfirmMessage }}</p>
                    </Modal>
                </div>
                <InfiniteScrollTable ref="tableRef" :loader="loader" :columns="columns" :row-height="rowHeight" :filters="mergedFilters" :row-key="rowKey" :hide-header="singleColumn" v-bind="localSearchFields !== null ? { localSearchFields } : {}" selectable @rowactivate="onRowActivate" @range-changed="onRangeChanged" />
            </template>
            <template #detail="{ tab }">
                <slot name="detail" :tab="tab" :is-new="tab.id.startsWith('new:')" :record="tab.id.startsWith('new:') ? undefined : tableRef?.getRow(tab.id)" :is-dupe="isDupe" />
            </template>
        </MasterDetail>
    </div>
</template>
<style lang="scss">
.CrudPanel {
    display: flex;
    flex-direction: column;
    flex-grow: 1;
    min-height: 0;

    .MasterDetail {
        flex-grow: 1;
    }

    &__controls {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 8px;
        padding: 8px;
        flex-grow: 0;
        border-bottom: 1px solid var(--border-color-light);
        &__gap {
            flex-grow: 1;
        }
    }
    &__range {
        font-size: 13px;
        opacity: 0.8;
        white-space: nowrap;
    }
    &__refresh {
        cursor: default;
        background: transparent;
        border: 1px solid var(--border-color-light);
        border-radius: var(--border-radius, 4px);
        padding: 2px 8px;
        font-size: 14px;
        line-height: 1;
        opacity: 0.7;
        &:hover {
            opacity: 1;
        }
    }
    .data-table td .cell-content {
        padding: var(--list-row-padding, 3px 6px);
        box-sizing: border-box;
        display: flex;
        align-items: center;
    }
    .data-table tbody {
        --row-border-bottom: 1px solid var(--border-color-light);
    }
    &__delete-modal {
        .Modal__window {
            max-width: 400px;
        }
        p {
            margin: 0;
        }
    }
}
</style>
