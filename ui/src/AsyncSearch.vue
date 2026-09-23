<script lang="ts">
import type { Ref } from 'vue'
import type { SearchResultItem } from './AsyncSearchResults.vue'

// isSearching is handed to the search function so it can drive the spinner
// for exactly the duration of its backend call (e.g. via an api pendingRef).
export type SearchFunction = (query: string, isSearching: Ref<boolean>) => Promise<SearchResultItem[]>
</script>
<script setup lang="ts">
import { h, inject, ref, watch, onBeforeUnmount, useSlots, type Component } from 'vue'
import { useTippy } from 'vue-tippy'
import { randomString } from '@greendrake/util'
import SearchField from './SearchField.vue'
import AsyncSearchResults from './AsyncSearchResults.vue'
import { inModalKey } from './inModal'

const slots = useSlots()
const query = ref('')
const emit = defineEmits<{
    result: [data: SearchResultItem]
}>()
const props = withDefaults(
    defineProps<{
        search: SearchFunction
        placeholder?: string
        autofocus?: boolean
        itemComponent?: Component | null
        searchKey?: unknown
        // Minimum query length before a search fires. Lower it for vocabularies
        // with legitimately short entries (e.g. two-letter brand names).
        minQueryLength?: number
    }>(),
    {
        itemComponent: undefined,
        searchKey: undefined,
        minQueryLength: 3
    }
)
let searchID: string | undefined
const isSearching = ref(false)
// Combobox state for AT: expanded while the results surface is up, with
// aria-controls tying the input to the (teleported) listbox.
const expanded = ref(false)
const listboxId = `async-search-${randomString()}`
const cmp = ref<InstanceType<typeof SearchField> | null>(null)

// Narrow viewports swap the anchored popper (which clips against small
// screens and on-screen keyboards) for a full-width results panel. Tracked
// live so a viewport change mid-session picks the right surface on the NEXT
// search (the open one is torn down on any change anyway).
const narrowQuery = window.matchMedia('(max-width: 600px)')
const isNarrow = ref(narrowQuery.matches)
const onNarrowChange = (e: MediaQueryListEvent): void => {
    isNarrow.value = e.matches
}
narrowQuery.addEventListener('change', onNarrowChange)
onBeforeUnmount(() => narrowQuery.removeEventListener('change', onNarrowChange))

// In the page the panel takes the viewport over, pinned under the input. In a
// modal it stays in flow: the sheet is already the narrow-viewport takeover
// surface, it paints over anything pinned to the viewport, and when it sits on
// the bottom edge there is no room under the input to pin to.
const inModal = inject(inModalKey, false)

// The panel's dataset (null = closed) and, when pinned, its measured top edge.
const panelResults = ref<SearchResultItem[] | null>(null)
const panelTop = ref(0)

let destroyTippy: (() => void) | null = null
let hideTippy: (() => void) | null = null
const closeResults = () => {
    if (destroyTippy) {
        destroyTippy()
        destroyTippy = null
        hideTippy = null
    }
    panelResults.value = null
    expanded.value = false
}
const clear = () => {
    query.value = ''
    closeResults()
}
onBeforeUnmount(closeResults)
const isValidQuery = (q: string): boolean => !!q && q.length >= props.minQueryLength

const onPick = (data: SearchResultItem) => {
    emit('result', data)
}

const executeSearch = async () => {
    closeResults()
    if (!isValidQuery(query.value)) {
        return
    }
    searchID = randomString()
    const currentSearchId = searchID
    const data = await props.search(query.value, isSearching)
    // Race guard: if a new search started while this one was in flight, its
    // results are stale — drop them and let the newer search present its own.
    closeResults()
    if (currentSearchId === searchID && !!query.value) {
        if (isNarrow.value) {
            // Full-width panel under the input (F9/F14) — rendered in this
            // component's template, so the footer slot and pick handling are
            // shared with the popper path.
            if (!inModal) {
                panelTop.value = (cmp.value?.$el as HTMLElement | undefined)?.getBoundingClientRect().bottom ?? 0
            }
            panelResults.value = data
            expanded.value = true
            return
        }
        const { destroy, hide: tippyHide } = useTippy(cmp, {
            theme: 'asyncSearchResults',
            trigger: 'manual',
            // The popper is a listbox surface, not a tooltip; keep tippy from
            // stamping tooltip semantics/aria onto it or the input.
            role: 'presentation',
            aria: { content: null, expanded: false },
            maxWidth: 'none',
            placement: 'bottom-start',
            offset: [0, -1],
            arrow: false,
            showOnCreate: true,
            interactive: true,
            content: h(
                AsyncSearchResults,
                {
                    data,
                    listboxId,
                    itemComponent: props.itemComponent,
                    // Only surface the pick — don't clear here. What happens next is the
                    // consumer's call (navigate, close, or clear via the exposed clear());
                    // the results stay put until the query changes or this component
                    // unmounts (e.g. when a navigation the pick triggered finally commits).
                    onPick
                },
                // The popper renders outside the app's render tree, so the host's
                // slots are handed to the vnode explicitly: default = the footer
                // strip, empty = the host's own zero-result surface.
                {
                    ...(slots['resultsFooter'] ? { default: slots['resultsFooter'] } : {}),
                    ...(slots['empty'] ? { empty: slots['empty'] } : {})
                }
            )
        })
        destroyTippy = destroy
        hideTippy = tippyHide
        expanded.value = true
    }
}

// Enter forces an immediate search on the current input. flush() commits the
// un-debounced value and cancels any pending debounced update; if that changed the
// query, the watcher below runs the search — so only re-trigger here when it did
// not (the re-summon-after-dismissal case), avoiding a double search.
const onEnter = () => {
    if (!cmp.value?.flush()) {
        executeSearch()
    }
}

watch(query, async v => {
    closeResults()
    if (isValidQuery(v)) {
        await executeSearch()
    }
})

// Re-run search when searchKey changes (if there's a valid query)
watch(
    () => props.searchKey,
    async () => {
        if (isValidQuery(query.value)) {
            await executeSearch()
        }
    }
)
// Dismiss the results popper WITHOUT clearing the query (unlike clear) — keeps the
// query intact so a footer action that reads it (e.g. "Save this search") still works,
// and stops the popper from covering any modal that action opens.
const hide = () => hideTippy?.()
defineExpose({
    clear,
    hide
})
</script>
<template>
    <div class="AsyncSearch" @keydown.enter.prevent="onEnter">
        <!-- Enter searches the current input immediately and can re-summon the results
             box after it was dismissed (e.g. by refocusing the field), which otherwise
             needs editing the query. See onEnter; .prevent stops a stray form submit.
             Listener on the root div (not the SearchField component) so it fires once,
             free of attribute fallthrough. A root-level comment would make this a
             fragment in dev builds (breaking $el consumers), hence it sits inside. -->
        <SearchField v-bind="{ ...$attrs, role: 'combobox', 'aria-autocomplete': 'list', 'aria-expanded': expanded ? 'true' : 'false', 'aria-controls': listboxId }" ref="cmp" v-model="query" :placeholder="placeholder" :autofocus="autofocus" :class="{ spinner: isSearching }" :debounce="true" />
        <!-- Teleported only when pinned to the viewport; in a modal the panel is
             left in flow, inside the sheet (and inside its focus/inert scope). -->
        <Teleport to="body" :disabled="inModal">
            <div v-if="panelResults" class="AsyncSearch__panel" :class="{ 'AsyncSearch__panel--pinned': !inModal }" :style="inModal ? undefined : { top: `${panelTop}px` }" @keydown.esc="closeResults">
                <AsyncSearchResults :data="panelResults" :listbox-id="listboxId" :item-component="itemComponent" @pick="onPick">
                    <!-- Forwarded only when the host actually supplies it, so an
                         empty pass-through can't shadow the default message. -->
                    <template v-if="$slots.empty" #empty><slot name="empty" /></template>
                    <slot name="resultsFooter" />
                </AsyncSearchResults>
            </div>
        </Teleport>
    </div>
</template>
<style lang="scss">
@use './shared' as *;
@use '@greendrake/scss-kit' as *;
.AsyncSearch {
    position: relative;

    &__panel {
        background: var(--background-color);
        border-top: 1px solid var(--border-color);

        // Pinned variant: takes the viewport over from the input's bottom edge
        // down to the bottom nav, so the whole remaining screen is results.
        &--pinned {
            position: fixed;
            left: 0;
            right: 0;
            bottom: calc(var(--bottomnav-height, 0px) + var(--safe-area-inset-bottom, 0px));
            z-index: z('sticky');
            overflow-y: auto;
            .AsyncSearchResults {
                max-height: none;
            }
        }
    }
    .SearchField {
        position: relative;
        &.spinner::before {
            @include spinner-inline-right(28px);
        }
    }
    div[data-tippy-root] {
        width: 100%;
        cursor: default;
    }
    .tippy-box[data-theme~='asyncSearchResults'] {
        background-color: var(--background-color);
        border: 1px solid var(--border-color);
        color: var(--font-color);
        // The popper reads as the field's continuation, so it takes the field's
        // corner — and clips to it: the result rows and the host's footer paint
        // their own opaque backgrounds out to the edge and would square the
        // corners off again. Nothing to lose by clipping (this theme has no
        // arrow, and the result list scrolls inside its own box).
        border-radius: var(--border-radius);
        overflow: hidden;
        .tippy-content {
            padding: 0;
        }
    }
}
</style>
