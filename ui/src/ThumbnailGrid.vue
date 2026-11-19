<script lang="ts">
import type { Ref } from 'vue'
import { displayedImage, mediaType, type MediaFile } from './media'
import { messages, type UiMessages } from './messages'

export type ThumbnailGridItem = Record<string, unknown>

// The kinds of file a tile's icon tells apart, each by its accessible name.
const kindNames = {
    image: 'fileImage',
    video: 'fileVideo',
    audio: 'fileAudio',
    document: 'fileDocument',
    spreadsheet: 'fileSpreadsheet',
    presentation: 'filePresentation',
    archive: 'fileArchive',
    other: 'fileOther'
} as const satisfies Record<string, keyof UiMessages>

type FileKind = keyof typeof kindNames

// A type's kind: the type's own, else its top-level type's, else other.
const kinds: Record<string, FileKind> = {
    'image/vnd.djvu': 'document',
    'application/pdf': 'document',
    'application/msword': 'document',
    'application/vnd.openxmlformats-officedocument.wordprocessingml.document': 'document',
    'application/vnd.oasis.opendocument.text': 'document',
    'application/rtf': 'document',
    'application/epub+zip': 'document',
    'text/csv': 'spreadsheet',
    'application/vnd.ms-excel': 'spreadsheet',
    'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet': 'spreadsheet',
    'application/vnd.oasis.opendocument.spreadsheet': 'spreadsheet',
    'application/vnd.ms-powerpoint': 'presentation',
    'application/vnd.openxmlformats-officedocument.presentationml.presentation': 'presentation',
    'application/vnd.oasis.opendocument.presentation': 'presentation',
    'application/zip': 'archive',
    'application/gzip': 'archive',
    'application/x-tar': 'archive',
    'application/x-bzip2': 'archive',
    'application/x-xz': 'archive',
    'application/x-7z-compressed': 'archive',
    'application/vnd.rar': 'archive',
    'application/x-rar-compressed': 'archive'
}
const topLevelKinds: Record<string, FileKind> = {
    image: 'image',
    video: 'video',
    audio: 'audio',
    text: 'document'
}

// What a tile shows of its file: the image, where a browser displays it — or
// of no type known, as a bare image URL — else the icon of its kind; neither
// without a file.
const picture = (file: MediaFile | null): { image: string | null; kind: FileKind | null } => {
    if (file === null) {
        return { image: null, kind: null }
    }
    const type = mediaType(file)
    if (type === null || displayedImage(type)) {
        return { image: file.url, kind: null }
    }
    return { image: null, kind: kinds[type] ?? topLevelKinds[type.split('/')[0]] ?? 'other' }
}

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
// both views from one closure. Selection is a v-model array of id values: a
// click picks a tile as a file manager's does (useSelection: alone, Ctrl/Cmd
// toggling it, Shift the range from the last tile picked), a band dragged over
// the tiles selects those it touches, a click on the background clears it; it
// holds tiles of the page shown alone.
// A double click opens a tile's item; a right-click asks the host for the
// menu of the selection the tile is in. With `draggable`, a tile drags the
// selection it is in, the host filling the drag in, and a band starts on the
// background alone. A page is as many tiles as the view holds; a resize that
// changes that reloads. The pager goes to the first, previous, next or last
// page, to the page its slider is released at, or to the one typed into its
// field. Reloads whenever `filters` changes (host puts its filter signature
// there).
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'
import SelectionBand from './SelectionBand.vue'
import useSelection from './useSelection'
import useSelectionBand from './useSelectionBand'

const props = withDefaults(
    defineProps<{
        loader: ThumbnailGridLoader
        filters?: Record<string, unknown>
        /** The file an item's tile shows; null for none. */
        thumbnail: (item: ThumbnailGridItem) => MediaFile | null
        titleField?: string
        idField?: string
        /** Whether tiles drag: each emits `tiledragstart`. */
        draggable?: boolean
    }>(),
    {
        filters: () => ({}),
        titleField: 'title',
        idField: 'id',
        draggable: false
    }
)

const model = defineModel<Array<string | number>>('selected', { default: () => [] })

const emit = defineEmits<{
    /** A tile double-clicked: its item. */
    open: [item: ThumbnailGridItem]
    /** A tile right-clicked, selected now: the items of the selection it is in, in page order, and the event, for a context menu to open at. */
    tilecontextmenu: [items: ThumbnailGridItem[], event: MouseEvent]
    /** A tile's drag begun, with `draggable`, the tile selected now: the items of the selection it is in, in page order, and the event, for the host to fill the drag's data in. */
    tiledragstart: [items: ThumbnailGridItem[], event: DragEvent]
}>()

const items = ref<ThumbnailGridItem[]>([])
const total = ref(0)
const offset = ref(0)
// The tiles a page holds: as many as the view does, measured once mounted.
const pageSize = ref(0)
const loading = ref(false)

// The item fields are addressed by configurable names, so their types are the
// grid's contract rather than the item type's: id is a string/number key,
// title display text.
const idOf = (item: ThumbnailGridItem) => item[props.idField] as string | number

const tiles = computed(() =>
    items.value.map(item => ({
        item,
        id: idOf(item),
        title: item[props.titleField] as string,
        ...picture(props.thumbnail(item))
    }))
)

// Selection is owned by useSelection's Set; the v-model array mirrors it.
const selection = useSelection<string | number>(tiles, 'id', { model })
const selected = selection.selected

// A slower answer to an earlier load is dropped.
let latest = 0
async function load() {
    const current = ++latest
    const res = await props.loader(
        {
            offset: offset.value,
            limit: pageSize.value,
            ...props.filters
        },
        loading
    )
    if (current !== latest) return
    items.value = res.data
    total.value = res.total
    // The selection holds tiles of the page shown alone: what it acts on is
    // in sight.
    for (const id of selected) {
        if (!items.value.some(item => idOf(item) === id)) selected.delete(id)
    }
}

// Reloads the page shown, or the last page there is once it is past the items
// left.
async function reload() {
    await load()
    const last = Math.max(0, Math.ceil(total.value / pageSize.value) - 1) * pageSize.value
    if (offset.value > last) offset.value = last
}

// Takes items out of the page, and out of the selection.
function removeItems(ids: (string | number)[]) {
    const gone = new Set(ids)
    const before = items.value.length
    items.value = items.value.filter(item => !gone.has(idOf(item)))
    total.value -= before - items.value.length
    for (const id of gone) selected.delete(id)
}

// A page moved to, or resized, loads once; new filters load the first page.
watch([offset, pageSize], load)
watch(
    () => props.filters,
    () => {
        if (offset.value === 0) load()
        else offset.value = 0
    },
    { deep: true }
)

const tilesRef = ref<HTMLElement | null>(null)
const titleProbe = ref<HTMLElement | null>(null)

// The tiles the view holds, in full rows: as many columns as the grid lays
// out, as many rows as its height takes of tiles — a square picture a column
// wide (the tile's borders, alike all round, cancel out) over a title, as tall
// as the hidden one probing it.
function fittingPageSize(): number {
    const grid = tilesRef.value!
    const style = getComputedStyle(grid)
    const columns = style.gridTemplateColumns.split(' ')
    const gap = parseFloat(style.rowGap)
    const height = grid.clientHeight - parseFloat(style.paddingTop) - parseFloat(style.paddingBottom)
    const tile = parseFloat(columns[0]) + titleProbe.value!.offsetHeight
    return columns.length * Math.max(1, Math.floor((height + gap) / (tile + gap)))
}

// A resize changing what the view holds moves to the page holding the first
// tile shown, at the new size.
const resized = new ResizeObserver(() => {
    const size = fittingPageSize()
    if (size === pageSize.value) return
    offset.value = Math.floor(offset.value / size) * size
    pageSize.value = size
})
onMounted(() => {
    pageSize.value = fittingPageSize()
    resized.observe(tilesRef.value!)
})
onUnmounted(() => resized.disconnect())
// The tiles' elements, each by its tile's id: rendered in the tiles' order.
const { band, start } = useSelectionBand<string | number>(tilesRef, () => Array.from(tilesRef.value!.querySelectorAll(':scope > .ThumbnailGrid__tile'), (el, i): [string | number, Element] => [tiles.value[i].id, el]))

// Whether the press a click ends drew a band: that click picks nothing.
let banded = false

// A band replaces the selection, or with Ctrl/Cmd or Shift held, adds to it.
// A draggable tile drags rather than bands.
function onPointerDown(e: PointerEvent) {
    banded = false
    if (props.draggable && (e.target as Element).closest('.ThumbnailGrid__tile')) return
    const kept = e.ctrlKey || e.metaKey || e.shiftKey ? [...selected] : []
    start(e, ids => {
        banded = true
        selection.selectOnly([...kept, ...ids])
    })
}

// The second click of a double click picks nothing: the tile it opens stays
// as its first click left it.
function onTileClick(id: string | number, e: MouseEvent) {
    if (banded || e.detail > 1) return
    selection.pick(id, e)
}

// The items a right-click or a drag acts on: those of the selection the tile
// is in, in page order, after selecting the tile alone unless it is selected
// already.
function selectionOf(id: string | number): ThumbnailGridItem[] {
    if (!selected.has(id)) selection.replace(id)
    return tiles.value.filter(tile => selected.has(tile.id)).map(tile => tile.item)
}

// A plain click on the background, outside any tile, clears the selection, as
// a file manager's does.
function onBackgroundClick(e: MouseEvent) {
    if (banded || e.ctrlKey || e.metaKey || e.shiftKey) return
    selected.clear()
}

// The pages there are, and the one shown: with no items, a single empty page.
// The offset is always a page's.
const pages = computed(() => (total.value ? Math.ceil(total.value / pageSize.value) : 1))
const page = computed(() => (total.value ? offset.value / pageSize.value + 1 : 1))

function go(to: number) {
    offset.value = (to - 1) * pageSize.value
}

// The page the pager points at, as its field shows it: the page shown, but for
// one typed into the field, or the slider is dragged to, until it is gone to.
const pointed = ref('1')
watch(page, p => {
    pointed.value = String(p)
})
// The page pointed at, if there is such a page; the slider shows it, else the
// page shown.
const pointedPage = computed(() => {
    const p = Number(pointed.value)
    return Number.isInteger(p) && p >= 1 && p <= pages.value ? p : null
})

// The slider released, or the field entered or left, goes to the page pointed
// at, if there is one; the field then shows the page shown.
function goPointed() {
    if (pointedPage.value !== null) go(pointedPage.value)
    pointed.value = String(page.value)
}

defineExpose({ reload, removeItems })
</script>
<template>
    <div class="ThumbnailGrid" :class="{ spinner: loading }">
        <div ref="tilesRef" class="ThumbnailGrid__tiles" @pointerdown="onPointerDown" @click.self="onBackgroundClick">
            <div v-for="tile in tiles" :key="tile.id" class="ThumbnailGrid__tile" :class="{ selected: selected.has(tile.id) }" :draggable="draggable || undefined" @click="onTileClick(tile.id, $event)" @dblclick="emit('open', tile.item)" @contextmenu="emit('tilecontextmenu', selectionOf(tile.id), $event)" @dragstart="emit('tiledragstart', selectionOf(tile.id), $event)">
                <img v-if="tile.image" class="ThumbnailGrid__picture" :src="tile.image" :alt="tile.title" draggable="false" />
                <div v-else class="ThumbnailGrid__picture">
                    <span v-if="tile.kind" class="ThumbnailGrid__icon" :class="`ThumbnailGrid__icon--${tile.kind}`" role="img" :aria-label="messages[kindNames[tile.kind]]"></span>
                </div>
                <div class="ThumbnailGrid__title">{{ tile.title }}</div>
            </div>
            <div v-if="!items.length" class="ThumbnailGrid__empty">{{ messages.nothingFound }}</div>
            <div ref="titleProbe" class="ThumbnailGrid__title ThumbnailGrid__probe" aria-hidden="true">&nbsp;</div>
            <SelectionBand :band="band" />
        </div>
        <div class="ThumbnailGrid__pager">
            <input type="range" class="ThumbnailGrid__slider" min="1" :max="pages" :value="pointedPage ?? page" :disabled="pages === 1" :aria-label="messages.page" @input="pointed = ($event.target as HTMLInputElement).value" @change="goPointed" />
            <div class="ThumbnailGrid__nav">
                <button type="button" :disabled="page === 1" :aria-label="messages.firstPage" @click="go(1)">«</button>
                <button type="button" :disabled="page === 1" :aria-label="messages.previousPage" @click="go(page - 1)">‹</button>
                <input v-model="pointed" type="text" inputmode="numeric" class="ThumbnailGrid__page" :size="String(pages).length" :aria-label="messages.goToPage" @keydown.enter="goPointed" @blur="goPointed" />
                <span class="ThumbnailGrid__pages">/ {{ pages }}</span>
                <button type="button" :disabled="page >= pages" :aria-label="messages.nextPage" @click="go(page + 1)">›</button>
                <button type="button" :disabled="page >= pages" :aria-label="messages.lastPage" @click="go(pages)">»</button>
            </div>
            <span class="ThumbnailGrid__range">{{ total ? offset + 1 : 0 }}–{{ Math.min(offset + pageSize, total) }} / {{ total }}</span>
        </div>
    </div>
</template>
<style lang="scss">
@use '@greendrake/scss-kit' as *;

.ThumbnailGrid {
    display: flex;
    flex-direction: column;
    min-height: 0;
    &__tiles {
        // Places the selection band.
        position: relative;
        // A press over the tiles draws a band; it selects no text.
        user-select: none;
        flex-grow: 1;
        // A page fits by its size; the one a resize outgrows, until the page
        // of the new size replaces it, is clipped rather than scrolled: a
        // scrollbar would narrow the grid, and resize it again.
        overflow: hidden;
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
            outline: 1px solid var(--border-color-hover);
        }
    }
    &__picture {
        display: grid;
        place-items: center;
        width: 100%;
        aspect-ratio: 1;
        object-fit: cover;
        background: var(--form-background);
    }
    &__icon {
        @include icon-base;
        width: 40%;
        height: 40%;
        opacity: 0.5;
        @each $kind in (image, video, audio, document, spreadsheet, presentation, archive) {
            &--#{$kind} {
                @include i('file-#{$kind}');
            }
        }
        &--other {
            @include i('file');
        }
    }
    &__title {
        padding: var(--space-1);
        font-size: var(--font-xs);
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
    }
    &__probe {
        position: absolute;
        visibility: hidden;
    }
    &__empty {
        padding: var(--space-4);
        opacity: 0.6;
    }
    // The slider spans the pager over the controls: the page navigation
    // centred, the range of items shown at the end. Its rows keep their
    // height whatever they hold, so a page loaded never resizes the tiles.
    &__pager {
        display: grid;
        grid-template-columns: 1fr auto 1fr;
        align-items: center;
        gap: var(--space-1) var(--space-2);
        padding: var(--space-1) var(--space-2);
        border-top: 1px solid var(--border-color-light);
        white-space: nowrap;
    }
    // The browser's own track and thumb, without a field's box.
    &__slider {
        grid-column: 1 / -1;
        min-width: 0;
        accent-color: var(--accent-color);
        &,
        &:focus {
            padding: 0;
            border: none;
            background: none;
        }
        &:focus-visible {
            outline: 1px solid var(--border-color-hover);
        }
    }
    &__nav {
        grid-column: 2;
        display: flex;
        align-items: center;
        gap: var(--space-2);
    }
    &__page {
        text-align: center;
    }
    &__range {
        grid-column: 3;
        justify-self: end;
    }
}
</style>
