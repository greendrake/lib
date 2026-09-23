<script lang="ts">
import type { Ref } from 'vue'

export type ThumbnailGridItem = Record<string, unknown>

export interface ThumbnailGridLoaderParams {
    offset: number
    limit: number
    [key: string]: unknown
}

export type ThumbnailGridLoader = (params: ThumbnailGridLoaderParams, loading: Ref<boolean>) => Promise<{ data: ThumbnailGridItem[]; total: number }>
</script>
<script setup lang="ts">
// Paged, multi-select thumbnail grid. Backed by the same loader contract as
// the data table ((params, loading) => { data, total }) so a host can feed
// both views from one closure. Selection is a v-model array of id values. Reloads
// whenever `filters` changes (host puts its filter signature there).
import { ref, watch, onMounted } from 'vue'
import { messages } from './messages'

const props = withDefaults(
    defineProps<{
        loader: ThumbnailGridLoader
        filters?: Record<string, unknown>
        thumbnailField?: string
        titleField?: string
        idField?: string
        pageSize?: number
    }>(),
    {
        filters: () => ({}),
        thumbnailField: 'thumbnail',
        titleField: 'title',
        idField: 'node_id',
        pageSize: 24
    }
)

const selected = defineModel<Array<string | number>>('selected', { default: () => [] })

const items = ref<ThumbnailGridItem[]>([])
const total = ref(0)
const offset = ref(0)
const loading = ref(false)

// The item fields are addressed by configurable names, so their types are the
// grid's contract rather than the item type's: id is a string/number key,
// thumbnail a URL string, title display text.
const idOf = (item: ThumbnailGridItem) => item[props.idField] as string | number
const thumbnailOf = (item: ThumbnailGridItem) => item[props.thumbnailField] as string
const titleOf = (item: ThumbnailGridItem) => item[props.titleField] as string

async function load() {
    const res = await props.loader(
        {
            offset: offset.value,
            limit: props.pageSize,
            ...props.filters
        },
        loading
    )
    items.value = res.data
    total.value = res.total
}

function refresh() {
    offset.value = 0
    load()
}

watch(
    () => props.filters,
    () => {
        offset.value = 0
        load()
    },
    { deep: true }
)
watch(offset, load)
onMounted(load)

function toggle(id: string | number) {
    selected.value = selected.value.includes(id) ? selected.value.filter(x => x !== id) : [...selected.value, id]
}

function end() {
    return Math.min(offset.value + props.pageSize, total.value)
}
function prev() {
    if (offset.value > 0) offset.value = Math.max(0, offset.value - props.pageSize)
}
function next() {
    if (end() < total.value) offset.value += props.pageSize
}

defineExpose({ refresh })
</script>
<template>
    <div class="ThumbnailGrid" :class="{ spinner: loading }">
        <div class="ThumbnailGrid__tiles">
            <div v-for="item in items" :key="idOf(item)" class="ThumbnailGrid__tile" :class="{ selected: selected.includes(idOf(item)) }" @click="toggle(idOf(item))">
                <img class="ThumbnailGrid__img" :src="thumbnailOf(item)" :alt="titleOf(item)" />
                <div class="ThumbnailGrid__title">{{ titleOf(item) }}</div>
            </div>
            <div v-if="!items.length" class="ThumbnailGrid__empty">{{ messages.nothingFound }}</div>
        </div>
        <div class="ThumbnailGrid__pager">
            <button type="button" :disabled="offset === 0" @click="prev">‹</button>
            <span class="ThumbnailGrid__range">{{ total ? offset + 1 : 0 }}–{{ end() }} / {{ total }}</span>
            <button type="button" :disabled="end() >= total" @click="next">›</button>
        </div>
    </div>
</template>
<style lang="scss">
.ThumbnailGrid {
    display: flex;
    flex-direction: column;
    min-height: 0;
    &__tiles {
        flex-grow: 1;
        overflow-y: auto;
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
        gap: var(--space-2);
        padding: var(--space-2);
        align-content: start;
    }
    &__tile {
        border: 1px solid var(--border-color-light);
        border-radius: var(--border-radius);
        overflow: hidden;
        cursor: default;
        &.selected {
            border-color: var(--border-color-hover);
            outline: 2px solid var(--border-color-hover);
        }
    }
    &__img {
        display: block;
        width: 100%;
        aspect-ratio: 1;
        object-fit: cover;
        background: var(--form-background);
    }
    &__title {
        padding: var(--space-1);
        font-size: var(--font-xs);
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
    }
    &__empty {
        padding: var(--space-4);
        opacity: 0.6;
    }
    &__pager {
        display: flex;
        align-items: center;
        justify-content: center;
        gap: var(--space-2);
        padding: var(--space-1);
        border-top: 1px solid var(--border-color-light);
    }
}
</style>
