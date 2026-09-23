<script lang="ts">
const DEBOUNCE_MS = 150
// Inertial scrolling: a released pan keeps scrolling, its velocity decaying
// exponentially with time constant τ. The velocity floor is both the minimum
// release velocity that starts a fling and the speed at which one stops; the
// release velocity is estimated over the trailing window of pan samples.
const FLING_DECAY_TAU_MS = 325
const FLING_MIN_VELOCITY_PX_MS = 0.05
const FLING_SAMPLE_WINDOW_MS = 100

// setPointerCapture throws NotFoundError when the pointer is no longer active —
// released or cancelled between event dispatch and this call, a real race for
// touch/pen and under synthetic pointer streams. A gesture whose pointer is
// already gone has nothing to capture, so exactly that error is a no-op;
// anything else still throws.
const capturePointer = (event: PointerEvent): void => {
    try {
        ;(event.currentTarget as Element).setPointerCapture(event.pointerId)
    } catch (e) {
        if (!(e instanceof DOMException && e.name === 'NotFoundError')) throw e
    }
}
</script>
<script setup lang="ts" generic="T extends Record<string, unknown>">
import { ref, computed, watch, onMounted, onUnmounted, type Ref } from 'vue'
import { useSelection, messages, TOUCH_PAN_SLOP_PX } from '@greendrake/ui'
import { rowKeyOf, type Props, type DropTarget, type Filters, type LoaderParams, type LoaderResponse, type RowPosition, type VisibleRange, type InfiniteScrollTableExposed } from './InfiniteScrollTable.types'

const props = withDefaults(defineProps<Props<T>>(), {
    rowHeight: 45,
    visibleHeight: null,
    filters: () => ({}),
    textFilterKey: 'q',
    localSearchFields: () => ['title'],
    rowKey: 'id',
    selectable: false,
    rowDraggable: false,
    hideHeader: false,
    bufferSize: 1000,
    renderAll: false
})

const emit = defineEmits<{
    'range-changed': [range: VisibleRange]
    rowactivate: [record: T]
    rowdragstart: [record: T, event: DragEvent]
    rowdrop: [event: DragEvent, target: DropTarget<T> | null]
}>()

// State (`as Ref<…>` on the row-bearing refs: ref()'s UnwrapRef cannot resolve
// over an unbound generic, so their types are pinned explicitly)
const fullDataset = ref([]) as Ref<T[]>
const filteredSortedData = ref([]) as Ref<T[]>
const bufferStart = ref(0)
const totalRecords = ref(0)
const scrollPosition = ref(0)
const prevScrollPosition = ref(0)
const loading = ref(false)
const isDragging = ref(false)
const debounceTimer = ref<ReturnType<typeof setTimeout> | null>(null)
const dragStartOffset = ref(0)
const containerRef = ref<HTMLElement | null>(null)
const calculatedHeight = ref(600)
const theadRef = ref<HTMLElement | null>(null)
const theadHeight = ref(0)
const lastRequestOffset = ref<number | null>(null)
const baselineFilters = ref<Filters | null>(null)
const hasAllRecords = ref(false)
const colWidths = ref<number[]>([])
const resizeCol = ref<{ index: number; startX: number; startWidth: number } | null>(null)
const dropTarget = ref(null) as Ref<DropTarget<T> | null>

const keyOf = (record: T): string | number => rowKeyOf(record, props.rowKey)

// Selection — useSelection identifies items by a field, so rows are adapted to
// `{ key }` objects; this also serves function rowKeys (composite keys with no
// backing field).
const selectionItems = computed<Record<string, unknown>[]>(() => filteredSortedData.value.map(record => ({ key: keyOf(record) })))
const selection = props.selectable ? useSelection(selectionItems, 'key') : null

function scrollToIndex(index: number): void {
    if (index < 0 || maxScrollOffset.value <= 0) return
    const rowTop = index * props.rowHeight
    const rowBottom = rowTop + props.rowHeight
    let offset = pixelOffset.value
    if (rowTop < offset) {
        offset = rowTop
    } else if (rowBottom > offset + availableBodyHeight.value) {
        offset = rowBottom - availableBodyHeight.value
    } else {
        return
    }
    stopFling()
    scrollPosition.value = Math.max(0, Math.min(1, offset / maxScrollOffset.value))
}

function onRowPointerDown(record: T, e: PointerEvent): void {
    // Touch/pen select on pointerup instead (see onRowPointerUp), so that
    // starting a scroll pan on a row doesn't change the selection.
    if (!selection || e.pointerType !== 'mouse') return
    const id = keyOf(record)
    if (e.shiftKey) {
        selection.rangeTo(id)
    } else if (e.ctrlKey || e.metaKey) {
        selection.toggle(id)
    } else {
        selection.replace(id)
    }
}

// A panning pointer is captured by the wrapper (see onWrapperPointerMove), so
// its pointerup never reaches the row — this fires only for genuine taps.
function onRowPointerUp(record: T, e: PointerEvent): void {
    if (!selection || e.pointerType === 'mouse') return
    selection.replace(keyOf(record))
}

function onTableKeyDown(e: KeyboardEvent): void {
    if (!selection) return
    if ((e.ctrlKey || e.metaKey) && e.key === 'a') {
        e.preventDefault()
        selection.selectAll()
        return
    }
    const isUp = e.key === 'ArrowUp'
    const isDown = e.key === 'ArrowDown'
    if (!isUp && !isDown) return
    e.preventDefault()
    const newIndex = selection.moveCursor(isUp ? 'up' : 'down', e.shiftKey)
    scrollToIndex(newIndex)
}

const initColWidths = (): void => {
    if (!theadRef.value) return
    const ths = theadRef.value.querySelectorAll('th')
    colWidths.value = Array.from(ths, th => th.offsetWidth)
}

// Column resize and scroll-handle dragging use pointer capture on the grabbed
// element, so the drag keeps tracking the pointer outside the element (and the
// component) without document-level listeners — and works for touch/pen too.
const onColResizeStart = (event: PointerEvent, index: number): void => {
    event.preventDefault()
    event.stopPropagation()
    capturePointer(event)
    initColWidths()
    resizeCol.value = {
        index,
        startX: event.clientX,
        startWidth: colWidths.value[index]
    }
}

const onColResizeMove = (event: PointerEvent): void => {
    if (!resizeCol.value) return
    const delta = event.clientX - resizeCol.value.startX
    colWidths.value[resizeCol.value.index] = Math.max(30, resizeCol.value.startWidth + delta)
}

const onColResizeEnd = (): void => {
    resizeCol.value = null
}

// Computed values
const effectiveHeight = computed(() => props.visibleHeight || calculatedHeight.value)
const availableBodyHeight = computed(() => effectiveHeight.value - theadHeight.value)
// Whole rows that fully fit the body viewport — the basis for overscan and the
// "fully visible" range readout.
const visibleCount = computed(() => Math.max(1, Math.floor(availableBodyHeight.value / props.rowHeight)))
// Rows actually rendered: the fully-fitting rows plus two — one for a partial row
// clipped at the top (the body is pixel-scrolled, so the first row is usually cut)
// and one for a partial row clipped at the bottom. Constant per viewport size, so
// the row node count stays stable across scrolling (no per-pixel add/remove churn).
// With `renderAll` (testability affordance) the window instead spans the whole
// dataset, putting every buffered row in the DOM regardless of viewport height.
const renderCount = computed(() => (props.renderAll ? Math.max(totalRecords.value, 1) : visibleCount.value + 2))

// PIXEL SCROLL MODEL — scrollPosition (0..1) maps to an absolute pixel offset into
// the full content (totalRecords × rowHeight). Only renderCount rows are in the
// DOM, starting at firstIndex; the row block is shifted up by rowPixelOffset (the
// sub-row remainder) so scrolling is continuous instead of snapping to whole rows.
// At max offset the last row sits flush against the bottom edge.
const contentHeight = computed(() => totalRecords.value * props.rowHeight)
const maxScrollOffset = computed(() => Math.max(0, contentHeight.value - availableBodyHeight.value))
const pixelOffset = computed(() => scrollPosition.value * maxScrollOffset.value)
const firstIndex = computed(() => Math.floor(pixelOffset.value / props.rowHeight))
const rowPixelOffset = computed(() => pixelOffset.value - firstIndex.value * props.rowHeight)

// The scrollbar spans the body viewport (offset below the header by theadHeight,
// set inline). The handle is the viewport's fraction of the total content height,
// positioned by scrollPosition.
const scrollbarHeight = computed(() => availableBodyHeight.value)
const scrollHandleHeight = computed(() => {
    if (maxScrollOffset.value <= 0) return 0
    return Math.max(50, scrollbarHeight.value * (availableBodyHeight.value / contentHeight.value))
})
const scrollHandleTop = computed(() => scrollPosition.value * (scrollbarHeight.value - scrollHandleHeight.value))

const visibleRecords = computed(() => {
    const bufferOffset = firstIndex.value - bufferStart.value
    return filteredSortedData.value.slice(bufferOffset, bufferOffset + renderCount.value)
})

// True while the viewport overlaps records the buffer doesn't hold — i.e. the
// user is looking at rows that still have to arrive. Drives the spinner and the
// display-hold below, and flips off by itself the moment the data lands,
// whatever its source (network fetch, ApiWrapper response cache). This is
// deliberately DERIVED state: the `loading` ref belongs to the loader/ApiWrapper
// (true only while a call is in flight) and must not be written here — a cached
// response never flips it, so a component that pre-set it true would stall.
// The render window is clamped to the dataset tail: past-the-end positions at
// the bottom of the list are not a gap.
const hasVisibleGaps = computed(() => {
    if (hasAllRecords.value || maxScrollOffset.value <= 0) return false
    const bufferOffset = firstIndex.value - bufferStart.value
    if (bufferOffset < 0) return true
    const needed = Math.min(renderCount.value, totalRecords.value - firstIndex.value)
    return bufferOffset + needed > filteredSortedData.value.length
})

// Rows actually painted, plus the sub-row offset they're painted at. Normally
// these mirror visibleRecords/rowPixelOffset. But a large scroll jump moves the
// viewport into records that aren't buffered yet (hasVisibleGaps), so
// visibleRecords briefly slices to empty while the next buffer loads — if
// painted directly the table would blank out and collapse. Instead the previous
// non-empty set is held (frozen in place) until the buffer arrives, so the
// table stays put under the loading spinner. The hold is released for a genuine
// empty result (filter/sort with nothing pending and no gaps).
const displayRecords = ref([]) as Ref<T[]>
const displayRowPixelOffset = ref(0)
watch(
    [visibleRecords, rowPixelOffset, loading, hasVisibleGaps],
    ([rows, offset, isLoading, gaps]) => {
        if (rows.length > 0) {
            displayRecords.value = rows
            displayRowPixelOffset.value = offset
        } else if (!isLoading && !gaps) {
            displayRecords.value = []
        }
    },
    { immediate: true }
)

const visibleRange = computed(() => {
    if (totalRecords.value === 0) {
        return {
            start: 0,
            end: 0,
            total: 0
        }
    }
    // Records whose rows are FULLY inside the viewport (an edge-clipped partial row
    // doesn't count toward the readout).
    const firstFull = Math.ceil(pixelOffset.value / props.rowHeight)
    const lastFull = Math.floor((pixelOffset.value + availableBodyHeight.value) / props.rowHeight) - 1
    return {
        start: firstFull + 1,
        end: Math.min(lastFull + 1, totalRecords.value),
        total: totalRecords.value
    }
})

// Local filtering/sorting helpers
const isTextFilterRefinement = (newFilters: Filters, baseline: Filters, textKey: string): boolean => {
    const newText = newFilters[textKey] || ''
    const baseText = baseline[textKey] || ''
    if (!baseText) return true
    return newText.toLowerCase().includes(baseText.toLowerCase())
}

const canOperateLocally = (newFilters: Filters): boolean => {
    // localSearchFields: false = the caller's search semantics cannot be
    // mirrored client-side; text-filter changes always go remote.
    if (props.localSearchFields === false || !hasAllRecords.value || !baselineFilters.value) return false
    const textKey = props.textFilterKey
    const sortKeys = ['sort_by', 'sort_dir']
    for (const key of Object.keys(newFilters)) {
        if (key === textKey || sortKeys.includes(key)) continue
        if (newFilters[key] !== baselineFilters.value[key]) return false
    }
    return isTextFilterRefinement(newFilters, baselineFilters.value, textKey)
}

const applyLocalFilter = (data: T[], textFilter: string | null | undefined, searchFields: string[] | false): T[] => {
    if (!textFilter || searchFields === false) return data
    const lowerFilter = textFilter.toLowerCase()
    return data.filter(record =>
        searchFields.some(field => {
            const value = record[field]
            return value && String(value).toLowerCase().includes(lowerFilter)
        })
    )
}

const getFieldType = (field: string, records: T[]): 'string' | 'number' | 'date' => {
    const col = props.columns.find(c => c.field === field)
    if (col?.sortType) return col.sortType
    const sample = records.find(r => r[field] != null)?.[field]
    if (sample == null) return 'string'
    if (typeof sample === 'number') return 'number'
    if (sample instanceof Date || (typeof sample === 'string' && /^\d{4}-\d{2}-\d{2}/.test(sample))) return 'date'
    return 'string'
}

const compareValues = (a: unknown, b: unknown, type: 'string' | 'number' | 'date'): number => {
    if (a == null && b == null) return 0
    if (a == null) return 1
    if (b == null) return -1
    switch (type) {
        case 'number':
            return (a as number) - (b as number)
        case 'date':
            return new Date(a as string).getTime() - new Date(b as string).getTime()
        default:
            return String(a).localeCompare(String(b), undefined, {
                sensitivity: 'base'
            })
    }
}

const applyLocalSort = (data: T[], sortBy: string | null | undefined, sortDir: string | null | undefined): T[] => {
    if (!sortBy) return data
    const type = getFieldType(sortBy, data)
    const dir = sortDir === 'asc' ? 1 : -1
    return [...data].sort((a, b) => dir * compareValues(a[sortBy], b[sortBy], type))
}

const applyLocalOperations = (resetScroll = true): void => {
    const textFilter = props.filters[props.textFilterKey]
    let result = applyLocalFilter(fullDataset.value, textFilter, props.localSearchFields)
    result = applyLocalSort(result, props.filters.sort_by, props.filters.sort_dir)
    filteredSortedData.value = result
    totalRecords.value = result.length
    if (resetScroll) {
        scrollPosition.value = 0
        bufferStart.value = 0
    }
}

// API functions
const fetchData = async (offset: number, limit: number, background: boolean): Promise<LoaderResponse<T>> => {
    const params: LoaderParams = {
        offset,
        limit
    }
    Object.entries(props.filters).forEach(([key, value]) => {
        if (value !== null && value !== undefined && value !== '') {
            params[key] = value
        }
    })
    return props.loader(params, loading, background)
}

const loadBuffer = async (targetOffset: number, scrollingDown = true, background = false): Promise<void> => {
    const paddingTotal = Math.max(0, props.bufferSize - renderCount.value)
    const beforeRatio = scrollingDown ? 0.3 : 0.7
    const paddingBefore = Math.round(beforeRatio * paddingTotal)

    let offset = Math.max(0, targetOffset - paddingBefore)

    const endNeeded = targetOffset + renderCount.value
    if (totalRecords.value > 0 && endNeeded >= totalRecords.value) {
        offset = Math.max(0, totalRecords.value - props.bufferSize)
    }

    if (offset === lastRequestOffset.value) {
        return
    }

    const { data, total } = await fetchData(offset, props.bufferSize, background)
    lastRequestOffset.value = offset

    if (data.length >= total) {
        fullDataset.value = data
        hasAllRecords.value = true
        baselineFilters.value = { ...props.filters }
        applyLocalOperations()
    } else {
        hasAllRecords.value = false
        filteredSortedData.value = data
        bufferStart.value = offset
        totalRecords.value = total
    }
}

const updateScrollPosition = (newPosition: number): void => {
    const clampedPosition = Math.max(0, Math.min(1, newPosition))
    const scrollingDown = clampedPosition >= prevScrollPosition.value
    prevScrollPosition.value = clampedPosition
    scrollPosition.value = clampedPosition

    if (hasAllRecords.value) return

    const targetIndex = firstIndex.value
    const bufferEnd = bufferStart.value + filteredSortedData.value.length

    // Reload when the viewport has outright gaps, or as prefetch when it nears
    // either edge of the buffered range.
    const distanceFromStart = targetIndex - bufferStart.value
    const distanceFromEnd = bufferEnd - targetIndex
    const needsReload = hasVisibleGaps.value || distanceFromStart < renderCount.value * 0.5 || distanceFromEnd < renderCount.value * 0.5

    if (!needsReload) return

    if (debounceTimer.value) {
        clearTimeout(debounceTimer.value)
    }

    debounceTimer.value = setTimeout(() => {
        debounceTimer.value = null
        loadBuffer(firstIndex.value, scrollingDown)
    }, DEBOUNCE_MS)
}

// Scrollbar interaction
const scrollbarRef = ref<HTMLElement | null>(null)

const handleScrollbarClick = (event: MouseEvent): void => {
    if (event.target === scrollbarRef.value) {
        const rect = scrollbarRef.value!.getBoundingClientRect()
        const clickY = event.clientY - rect.top
        const newPosition = clickY / scrollbarHeight.value
        updateScrollPosition(newPosition)
    }
}

const onHandlePointerDown = (event: PointerEvent): void => {
    event.preventDefault()
    capturePointer(event)
    isDragging.value = true
    const pointerY = event.clientY - scrollbarRef.value!.getBoundingClientRect().top
    dragStartOffset.value = pointerY - scrollHandleTop.value
}

const onHandlePointerMove = (event: PointerEvent): void => {
    if (!isDragging.value) return

    const pointerY = event.clientY - scrollbarRef.value!.getBoundingClientRect().top
    const handleTop = pointerY - dragStartOffset.value
    const maxTop = scrollbarHeight.value - scrollHandleHeight.value

    updateScrollPosition(maxTop > 0 ? handleTop / maxTop : 0)
}

const onHandlePointerUp = (): void => {
    isDragging.value = false
    dragStartOffset.value = 0
}

// TOUCH PANNING over the rows — the table has no native scrollable overflow
// (rows are virtualized), so single-finger pans are translated to scroll
// offset here. Mouse is excluded: it scrolls via the wheel handler and keeps
// its native drag semantics (text selection, row drag-and-drop). Gesture state
// is non-reactive on purpose — nothing renders from it.
let touchPan: { pointerId: number; startY: number; lastY: number; panned: boolean; samples: { t: number; y: number }[] } | null = null
// A pan gesture may still produce a trailing click (which would activate the
// row link under the finger); onWrapperClickCapture swallows it. Also reset on
// pointerdown because browsers don't fire a click after every gesture.
let suppressNextClick = false

// INERTIAL SCROLLING (fling) — after a pan releases with velocity, scrolling
// continues on a rAF loop with exponential decay. Plain (non-reactive) state.
let flingFrame: number | null = null
let flingVelocity = 0 // scroll-offset px/ms; positive reveals further-down records
let flingLastTime = 0

// Stops an active fling; reports whether one was running so a pointerdown that
// "catches" the moving content can suppress its trailing click.
const stopFling = (): boolean => {
    if (flingFrame === null) return false
    cancelAnimationFrame(flingFrame)
    flingFrame = null
    return true
}

const flingStep = (now: number): void => {
    flingFrame = null
    const dt = now - flingLastTime
    flingLastTime = now
    const target = pixelOffset.value + flingVelocity * dt
    const clamped = Math.min(maxScrollOffset.value, Math.max(0, target))
    updateScrollPosition(maxScrollOffset.value > 0 ? clamped / maxScrollOffset.value : 0)
    flingVelocity *= Math.exp(-dt / FLING_DECAY_TAU_MS)
    // Keep going until the velocity floor or a scroll bound is reached.
    if (clamped === target && Math.abs(flingVelocity) > FLING_MIN_VELOCITY_PX_MS) {
        flingFrame = requestAnimationFrame(flingStep)
    }
}

const startFling = (velocity: number): void => {
    flingVelocity = velocity
    flingLastTime = performance.now()
    flingFrame = requestAnimationFrame(flingStep)
}

const onWrapperPointerDown = (event: PointerEvent): void => {
    // A pointer landing during a fling catches the content: the fling stops
    // and the gesture's trailing click must not activate the row under it.
    suppressNextClick = stopFling()
    if (event.pointerType === 'mouse' || maxScrollOffset.value <= 0) return
    // Touches on the scrollbar belong to the handle drag / track tap-to-jump,
    // not to content panning (which moves in the opposite direction).
    if (scrollbarRef.value?.contains(event.target as Node)) return
    touchPan = {
        pointerId: event.pointerId,
        startY: event.clientY,
        lastY: event.clientY,
        panned: false,
        samples: [{ t: event.timeStamp, y: event.clientY }]
    }
}

const onWrapperPointerMove = (event: PointerEvent): void => {
    if (!touchPan || event.pointerId !== touchPan.pointerId || maxScrollOffset.value <= 0) return
    touchPan.samples.push({ t: event.timeStamp, y: event.clientY })
    while (touchPan.samples.length > 1 && event.timeStamp - touchPan.samples[0].t > FLING_SAMPLE_WINDOW_MS) {
        touchPan.samples.shift()
    }
    if (!touchPan.panned) {
        if (Math.abs(event.clientY - touchPan.startY) < TOUCH_PAN_SLOP_PX) return
        touchPan.panned = true
        // Capturing retargets the rest of the gesture (incl. pointerup) to the
        // wrapper, keeping the pan alive outside the table and away from rows.
        capturePointer(event)
    }
    // Content follows the finger: moving up reveals further-down records.
    const newOffset = Math.min(maxScrollOffset.value, Math.max(0, pixelOffset.value - (event.clientY - touchPan.lastY)))
    touchPan.lastY = event.clientY
    updateScrollPosition(newOffset / maxScrollOffset.value)
}

const onWrapperPointerEnd = (event: PointerEvent): void => {
    if (!touchPan || event.pointerId !== touchPan.pointerId) return
    suppressNextClick = touchPan.panned
    if (touchPan.panned && event.type !== 'pointercancel') {
        // Release velocity over the trailing sample window. A finger held
        // still before lifting leaves no fresh samples — no fling.
        const samples = touchPan.samples.filter(s => event.timeStamp - s.t <= FLING_SAMPLE_WINDOW_MS)
        const dt = samples.length > 1 ? samples[samples.length - 1].t - samples[0].t : 0
        if (dt > 0) {
            // Content moves opposite to the finger, hence the negation.
            const velocity = -(samples[samples.length - 1].y - samples[0].y) / dt
            if (Math.abs(velocity) > FLING_MIN_VELOCITY_PX_MS) {
                startFling(velocity)
            }
        }
    }
    touchPan = null
}

const onWrapperClickCapture = (event: MouseEvent): void => {
    if (!suppressNextClick) return
    suppressNextClick = false
    event.preventDefault()
    event.stopPropagation()
}

const handleWheel = (event: WheelEvent): void => {
    if (scrollHandleHeight.value === 0) return
    event.preventDefault()
    stopFling()
    if (maxScrollOffset.value <= 0) return

    // Use the wheel's own pixel delta for natural, smooth scrolling; convert
    // line- and page-mode deltas (deltaMode 1/2) to pixels.
    const unit = event.deltaMode === 1 ? props.rowHeight : event.deltaMode === 2 ? availableBodyHeight.value : 1
    const newOffset = Math.min(maxScrollOffset.value, Math.max(0, pixelOffset.value + event.deltaY * unit))
    updateScrollPosition(newOffset / maxScrollOffset.value)
}

// Height calculation
const updateHeight = (): void => {
    if (containerRef.value && !props.visibleHeight) {
        const rect = containerRef.value.getBoundingClientRect()
        calculatedHeight.value = rect.height
    }
    if (theadRef.value) {
        theadHeight.value = theadRef.value.offsetHeight
    }
}

let resizeObserver: ResizeObserver | null = null

// Lifecycle
onMounted(() => {
    updateHeight()
    loadBuffer(0)

    if (containerRef.value && !props.visibleHeight) {
        resizeObserver = new ResizeObserver(() => {
            updateHeight()
        })
        resizeObserver.observe(containerRef.value)
    }
})

onUnmounted(() => {
    stopFling()
    if (debounceTimer.value) {
        clearTimeout(debounceTimer.value)
    }
    if (resizeObserver) {
        resizeObserver.disconnect()
    }
})

// Watch filters
watch(
    () => props.filters,
    newFilters => {
        stopFling()
        if (canOperateLocally(newFilters)) {
            applyLocalOperations()
        } else {
            hasAllRecords.value = false
            baselineFilters.value = null
            scrollPosition.value = 0
            lastRequestOffset.value = null
            loadBuffer(0)
        }
    },
    { deep: true }
)

// Emit range changes
watch(
    visibleRange,
    newRange => {
        emit('range-changed', newRange)
    },
    { immediate: true }
)

// Expose methods for parent components
// `background` marks a re-read nobody asked for (a live binding answering a
// push or a reconnect): it reaches the loader, which decides what a quiet
// request looks like. The promise is returned so such a binding can tell when
// the read is done and what it did — a foreground press ignores it.
const refresh = (background = false): Promise<void> => {
    hasAllRecords.value = false
    baselineFilters.value = null
    lastRequestOffset.value = null
    return loadBuffer(firstIndex.value, true, background)
}

/**
 * Surgically update a single row by key.
 * More efficient than refresh() when WebSocket pushes individual updates.
 */
const patchRow = (keyValue: string | number, data: Partial<T>): boolean => {
    // Update in fullDataset if we have all records
    if (hasAllRecords.value) {
        const fullIndex = fullDataset.value.findIndex(r => keyOf(r) === keyValue)
        if (fullIndex !== -1) {
            fullDataset.value[fullIndex] = {
                ...fullDataset.value[fullIndex],
                ...data
            }
        }
    }

    // Update in filteredSortedData (the working display array)
    const filteredIndex = filteredSortedData.value.findIndex(r => keyOf(r) === keyValue)
    if (filteredIndex !== -1) {
        filteredSortedData.value[filteredIndex] = {
            ...filteredSortedData.value[filteredIndex],
            ...data
        }
        return true
    }

    return false
}

/**
 * Add a new row to the dataset.
 * Position 'start' adds at beginning (useful for newest-first sort).
 * Position 'end' adds at end.
 * Default is 'start' for typical createdAt desc sorting.
 */
const addRow = (data: T, position: RowPosition = 'start'): void => {
    const insertInto = (arr: T[]) => {
        if (position === 'start') {
            arr.unshift(data)
        } else if (position === 'end') {
            arr.push(data)
        } else {
            const refKey = 'after' in position ? position.after : position.before
            const idx = arr.findIndex(r => keyOf(r) === refKey)
            if (idx === -1) {
                arr.push(data)
            } else {
                arr.splice('after' in position ? idx + 1 : idx, 0, data)
            }
        }
    }
    if (hasAllRecords.value) {
        insertInto(fullDataset.value)
        applyLocalOperations()
    } else {
        insertInto(filteredSortedData.value)
        totalRecords.value++
    }
}

/**
 * Remove rows from the dataset by key(s).
 */
const removeRows = (keyValues: (string | number)[]): void => {
    const toRemove = new Set(keyValues)

    if (hasAllRecords.value) {
        fullDataset.value = fullDataset.value.filter(r => !toRemove.has(keyOf(r)))
        applyLocalOperations()
    } else {
        const before = filteredSortedData.value.length
        filteredSortedData.value = filteredSortedData.value.filter(r => !toRemove.has(keyOf(r)))
        totalRecords.value -= before - filteredSortedData.value.length
    }
}

const removeRow = (keyValue: string | number): void => removeRows([keyValue])

/**
 * Re-apply current filter/sort to the displayed data without resetting scroll.
 * Useful after patchRow changes values in the current sort column.
 */
const resort = (): void => {
    if (!hasAllRecords.value) return
    applyLocalOperations(false)
}

// Drag-and-drop target tracking
const onTableDragOver = (e: DragEvent): void => {
    if (!props.rowDraggable) return
    e.preventDefault()
    const wrapper = e.currentTarget as HTMLElement
    const rect = wrapper.getBoundingClientRect()
    // Pointer Y in the painted block's own coordinates: the block is shifted up by
    // displayRowPixelOffset, so add it back to map the pointer to a displayRecords index.
    const blockY = e.clientY - rect.top - theadHeight.value + displayRowPixelOffset.value
    const rowIndex = Math.floor(blockY / props.rowHeight)
    const record = displayRecords.value[rowIndex]
    if (record) {
        const rowOffset = blockY - rowIndex * props.rowHeight
        dropTarget.value = { record, position: rowOffset < props.rowHeight / 2 ? 'before' : 'after' }
    } else {
        dropTarget.value = null
    }
}

const onTableDrop = (e: DragEvent): void => {
    if (!props.rowDraggable) return
    emit('rowdrop', e, dropTarget.value)
    dropTarget.value = null
}

const onTableDragLeave = (e: DragEvent): void => {
    if (!props.rowDraggable) return
    const wrapper = e.currentTarget as HTMLElement
    if (!wrapper.contains(e.relatedTarget as Node)) {
        dropTarget.value = null
    }
}

const getRow = (keyValue: string | number): T | undefined => {
    return filteredSortedData.value.find(r => keyOf(r) === keyValue) ?? (hasAllRecords.value ? fullDataset.value.find(r => keyOf(r) === keyValue) : undefined)
}

// Whether changing the text filter to `value` would be served from the in-memory
// dataset rather than a remote fetch — same predicate the filter watcher uses to
// route the change. Lets a consumer (e.g. its SearchField) debounce a purely local
// filter more eagerly than one that hits the network.
const willFilterLocally = (value: string | undefined): boolean => canOperateLocally({ ...props.filters, [props.textFilterKey]: value })

const exposed: InfiniteScrollTableExposed<T> = {
    refresh,
    patchRow,
    addRow,
    removeRow,
    removeRows,
    resort,
    getRow,
    willFilterLocally,
    selected: selection?.selected
}
defineExpose(exposed)
</script>

<template>
    <div ref="containerRef" class="infinite-scroll-table" :class="{ spinner: loading || hasVisibleGaps, 'col-resizing': resizeCol }">
        <div class="table-wrapper" :tabindex="selectable ? 0 : undefined" :style="{ touchAction: scrollHandleHeight > 0 ? 'pinch-zoom' : undefined }" @wheel="handleWheel" @keydown="onTableKeyDown" @pointerdown="onWrapperPointerDown" @pointermove="onWrapperPointerMove" @pointerup="onWrapperPointerEnd" @pointercancel="onWrapperPointerEnd" @click.capture="onWrapperClickCapture" @dragover="onTableDragOver" @drop="onTableDrop" @dragleave="onTableDragLeave">
            <table class="data-table">
                <thead v-if="!hideHeader" ref="theadRef">
                    <tr>
                        <th v-for="(column, i) in columns" :key="column.field" :class="[column.className, { sortable: column.sortable }]" :style="colWidths[i] != null ? { width: colWidths[i] + 'px' } : undefined" @click="column.onHeaderClick">
                            {{ column.label }}
                            <span v-if="column.sortIcon" class="sort-icon">{{ column.sortIcon }}</span>
                            <div class="col-resize-handle" @pointerdown="onColResizeStart($event, i)" @pointermove="onColResizeMove" @pointerup="onColResizeEnd" @pointercancel="onColResizeEnd" />
                        </th>
                    </tr>
                </thead>
                <tbody :style="{ transform: `translateY(${-displayRowPixelOffset}px)` }">
                    <tr v-if="!loading && totalRecords === 0">
                        <td :colspan="columns.length" class="empty-cell">
                            <slot name="empty">{{ messages.nothingFound }}</slot>
                        </td>
                    </tr>
                    <template v-else>
                        <tr v-for="record in displayRecords" :key="keyOf(record)" :style="{ height: `${rowHeight}px` }" :class="{ 'clickable-row': onRowClick, 'selected-row': selection?.selected.has(keyOf(record)), 'drop-before': dropTarget?.record === record && dropTarget.position === 'before', 'drop-after': dropTarget?.record === record && dropTarget.position === 'after' }" :draggable="rowDraggable || undefined" @click="onRowClick?.(record)" @pointerdown="onRowPointerDown(record, $event)" @pointerup="onRowPointerUp(record, $event)" @dblclick="emit('rowactivate', record)" @dragstart="rowDraggable && emit('rowdragstart', record, $event)">
                            <td v-for="(column, i) in columns" :key="column.field" :class="[column.className, column.cellClass?.(record[column.field], record), { clickable: column.onCellClick }]" :style="{ height: `${rowHeight}px`, width: colWidths[i] != null ? colWidths[i] + 'px' : undefined }" @click="column.onCellClick?.(record[column.field], record)">
                                <div :class="['cell-content', { 'cell-wrap': column.wrap }]" :style="{ height: `${rowHeight}px`, lineHeight: column.wrap ? undefined : `${rowHeight}px` }">
                                    <component :is="column.component" v-if="column.component" v-bind="column.componentProps?.(record)" />
                                    <template v-else>
                                        {{ column.formatter ? column.formatter(record[column.field], record) : record[column.field] }}
                                    </template>
                                </div>
                            </td>
                        </tr>
                    </template>
                </tbody>
            </table>

            <div v-if="scrollHandleHeight > 0" ref="scrollbarRef" class="custom-scrollbar" :style="{ height: `${scrollbarHeight}px`, marginTop: `${theadHeight}px` }" @click="handleScrollbarClick">
                <div
                    class="scroll-handle"
                    :class="{ dragging: isDragging }"
                    :style="{
                        height: `${scrollHandleHeight}px`,
                        top: `${scrollHandleTop}px`
                    }"
                    @pointerdown="onHandlePointerDown"
                    @pointermove="onHandlePointerMove"
                    @pointerup="onHandlePointerUp"
                    @pointercancel="onHandlePointerUp"
                />
            </div>
        </div>
    </div>
</template>

<!--
    FLEX HEIGHT CONSTRAINT — READ BEFORE MODIFYING STYLES

    This component uses a ResizeObserver to measure its own container height and
    derive the rendered row count from it. If the container's height is driven by
    its content (i.e. by the rendered rows), a feedback loop occurs:

        more height → more rows rendered → taller content → ResizeObserver fires
        → even more height → … → all records rendered, scrollbar disappears

    To prevent this, the flex layout must ensure that heights flow TOP-DOWN
    (from the viewport/parent allocation) and never BOTTOM-UP (from table
    content). The critical rules:

    1. `.infinite-scroll-table` uses `flex: 1; min-height: 0` so it fills the
       parent's allocated space without growing beyond it.

    2. `.table-wrapper` uses `flex: 1 1 0px` — the absolute `0px` flex-basis is
       essential. A percentage basis (`0%`) resolves relative to the parent's
       height; when ancestor heights aren't definite (common in deeply nested
       flex layouts), `0%` can fall back to content-based sizing, re-enabling
       the feedback loop. The absolute `0px` always starts at zero regardless
       of content.

    3. `.data-table` has `display: block; overflow: visible`. Its content height
       must NOT inflate the wrapper. This is safe only because rules 1–2 above
       prevent the wrapper from growing based on content.

    If you change any of these flex/overflow properties, verify at viewport
    heights where all records would fit if unconstrained (e.g. 100 records at
    28px each = 2800px content vs. a 1400px viewport). The scrollbar must remain
    visible and the container height must stay stable after initial render.
-->
<style lang="scss" scoped>
@use '@greendrake/scss-kit' as *;
.infinite-scroll-table {
    position: relative;
    overflow: hidden;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;

    &.spinner {
        &::before {
            width: 60px;
            height: 60px;
            margin-top: -30px;
            margin-left: -30px;
        }
    }

    &.col-resizing {
        user-select: none;
        * {
            cursor: col-resize !important;
        }
    }
}

.table-wrapper {
    position: relative;
    display: flex;
    flex: 1 1 0px;
    min-height: 0;
    overflow: hidden;
    padding-bottom: 1px;
    outline: none;

    &[tabindex] {
        user-select: none;
    }
}

.data-table {
    width: 100%;
    border-collapse: collapse;
    display: block;
    overflow: visible;

    thead {
        display: block;
        background: var(--form-background-hover);
        // Paint above the body: the pixel-scrolled row block shifts up behind the
        // header, and the header's opaque background must cover that clipped sliver.
        position: relative;
        z-index: 1;

        tr {
            display: table;
            width: 100%;
            table-layout: fixed;
        }

        th {
            position: relative;
            padding: var(--cell-padding, var(--space-2) var(--space-3));
            text-align: start;
            font-weight: 600;
            font-size: var(--font-md);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            background: var(--form-background-hover);

            &.sortable {
                cursor: default;
                user-select: none;

                &:hover {
                    background: var(--form-background);
                }
            }

            .sort-icon {
                margin-inline-start: var(--space-1);
                font-size: var(--font-xs);
                opacity: 0.7;
            }

            .col-resize-handle {
                position: absolute;
                inset-inline-end: 0;
                top: 0;
                bottom: 0;
                width: 5px;
                cursor: col-resize;
                z-index: z('raised');
                border-inline-end: 1px solid var(--border-color-light);
                // The drag is handled via pointer events; without this the
                // browser claims touch drags for panning and cancels them.
                touch-action: none;

                &:hover {
                    border-inline-end-color: var(--border-color-hover);
                }
            }
        }
    }

    tbody {
        display: block;
        overflow: visible;
        padding-bottom: 2px;

        tr {
            display: table;
            width: 100%;
            table-layout: fixed;
            border-bottom: var(--row-border-bottom, 0);

            &:last-child {
                border-bottom: none;
            }

            &.clickable-row {
                cursor: default;
            }

            &.selected-row {
                background: var(--row-select-color, rgba(0, 123, 255, 0.12));
                &:hover {
                    background: var(--row-select-color-hover, rgba(0, 123, 255, 0.18));
                }
            }

            &.drop-before {
                box-shadow: inset 0 2px 0 0 var(--accent-color);
            }

            &.drop-after {
                box-shadow: inset 0 -2px 0 0 var(--accent-color);
            }

            td {
                font-size: var(--font-md);
                padding: var(--cell-padding, 0);
                overflow: hidden;

                .cell-content {
                    overflow: hidden;
                    text-overflow: ellipsis;
                    white-space: nowrap;

                    &.cell-wrap {
                        white-space: pre-line;
                        text-overflow: clip;
                        display: grid;
                        align-content: center;
                    }
                }

                &.clickable {
                    cursor: default;

                    &:hover {
                        background: var(--form-background-hover);
                    }
                }
            }
        }

        .loading-cell,
        .empty-cell {
            text-align: center;
            padding: 40px;
            font-size: var(--font-md);
            color: var(--font-color);
        }
    }
}

.custom-scrollbar {
    position: relative;
    width: 16px;
    // Height + top offset are set inline to match the rendered rows region (see
    // scrollbarHeight); flex-start keeps it from stretching to the full wrapper.
    align-self: flex-start;
    cursor: default;
    flex-shrink: 0;

    .scroll-handle {
        position: absolute;
        width: 100%;
        // The drag is handled via pointer events; without this the browser
        // claims touch drags for panning and cancels them.
        touch-action: none;
        background: #888;
        border-top-right-radius: var(--border-radius);
        border-bottom-right-radius: var(--border-radius);
        cursor: grab;
        transition: background 0.2s;

        &:hover {
            background: #666;
        }

        &.dragging {
            background: #555;
            cursor: grabbing;
        }
    }
}
</style>
