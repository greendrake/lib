<script lang="ts">
export interface SelectBoxOption<T extends string | number = string | number> {
    value: T
    label: string
    disabled?: boolean
    // Tree depth for hierarchical vocabularies: indents the option in the MENU
    // only — the closed control always shows the plain label.
    depth?: number
}
</script>
<script setup lang="ts" generic="T extends string | number">
import { computed, ref, watch, nextTick, onUnmounted } from 'vue'
import { hasFinePointer } from '@greendrake/util/browser'
import { messages } from './messages'

// Single-select dropdown: string|number values, optional clear button,
// optional type-to-filter, full keyboard support (arrows/Enter/Escape).
// Themable via the --vs-* CSS custom properties (the same contract app skins
// already target).
const props = withDefaults(
    defineProps<{
        options: SelectBoxOption<T>[]
        placeholder?: string
        // Accessible name for the combobox; defaults to the placeholder.
        ariaLabel?: string
        clearable?: boolean
        // Show a text input in the control while the menu is open, filtering
        // the options by label (and stringified value) as the user types.
        searchable?: boolean
        disabled?: boolean
        // Render option labels via v-html instead of text — explicitly named
        // because it is an HTML injection surface; enable only for labels
        // built from trusted/escaped content.
        htmlLabel?: boolean
    }>(),
    {
        clearable: false,
        searchable: false,
        disabled: false,
        htmlLabel: false
    }
)

// null ⇔ no selection (shows the placeholder).
const model = defineModel<T | null>({ default: null })

// Fired on every menu selection, including re-picking the already-selected
// option (which produces no update:modelValue). Hosts that commit an action on
// pick (e.g. the location modal's instant-apply) listen to this, not the model.
const emit = defineEmits<{ pick: [value: T] }>()

const root = ref<HTMLElement | null>(null)
const menu = ref<HTMLElement | null>(null)
const searchInput = ref<HTMLInputElement | null>(null)
const menuOpen = ref(false)
const search = ref('')
const focusedIndex = ref(-1)
// Inline placement overrides (flip/clamp) computed per open — see placeMenu.
const menuStyle = ref<Record<string, string> | null>(null)

const selectedOption = computed(() => props.options.find(o => o.value === model.value))

// Matching is done on strings on both sides, so numeric values can never blow
// up a .toLowerCase() call.
const filteredOptions = computed(() => {
    if (!props.searchable || !search.value) {
        return props.options
    }
    const needle = search.value.toLowerCase()
    return props.options.filter(o => o.label.toLowerCase().includes(needle) || String(o.value).toLowerCase().includes(needle))
})

const firstSelectableIndex = (): number => filteredOptions.value.findIndex(o => !o.disabled)

// How far the menu clears the control: it overlaps the shared border by a pixel,
// so the two read as one box. Both anchor directions use it.
const MENU_OVERLAP = 1
const MARGIN = 8

// Where the menu would sit, anchored to the control in viewport coordinates.
// The menu is positioned against the viewport rather than the control (see the
// `position: fixed` note in the style block), so every open sets its box here —
// CSS cannot follow a control it is not a descendant of.
const anchorStyle = (rootRect: DOMRect, up: boolean): Record<string, string> => ({
    left: `${rootRect.left}px`,
    width: `${rootRect.width}px`,
    // Fixed offsets resolve against the LAYOUT viewport, so innerHeight is the
    // right bottom edge here — not the visual viewport the floor is measured in.
    ...(up ? { bottom: `${window.innerHeight - rootRect.top - MENU_OVERLAP}px` } : { top: `${rootRect.bottom - MENU_OVERLAP}px` })
})

// The menu's height with nothing clamping it, read from the scroll extent rather
// than by dropping the clamp and measuring again: re-measuring would paint the
// unclamped menu for a frame, which on a scroll-driven re-place is a flicker
// every frame.
const naturalHeight = (m: HTMLElement): number => m.scrollHeight + (m.offsetHeight - m.clientHeight)

// The menu defaults to opening downward, but fixed chrome (e.g. a bottom nav)
// can paint over it, hiding the very options scroll-to-selected parks at the
// bottom edge. Probe the rendered menu's bottom edge with a hit-test: anything
// on top of it that isn't ours marks the usable floor; clamp the menu's height
// to the space above that floor, or flip upward when the space above the
// control is more generous. Self-contained — no app-side configuration.
//
// Anchored first and probed after: the hit-test only means anything once the
// menu is where it will actually be.
const placeMenu = async (): Promise<void> => {
    const m = menu.value
    const r = root.value
    if (!m || !r) return
    menuStyle.value = anchorStyle(r.getBoundingClientRect(), false)
    await nextTick()
    const height = naturalHeight(m)
    const rootRect = r.getBoundingClientRect()
    // The floor is the bottom of what is actually on screen, which the on-screen
    // keyboard moves up: it shrinks the VISUAL viewport while the layout one —
    // and so window.innerHeight — is unchanged, and it is not in the DOM either,
    // so the hit-test below cannot see it. offsetTop covers the browser scrolling
    // the visual viewport to hold a focused field clear of the keyboard.
    const vv = window.visualViewport
    const viewportBottom = vv ? vv.offsetTop + vv.height : window.innerHeight
    const menuRect = m.getBoundingClientRect()
    let floor = Math.min(viewportBottom, menuRect.bottom + MARGIN)
    const probe = document.elementFromPoint(Math.round(menuRect.x + menuRect.width / 2), Math.round(Math.min(menuRect.bottom, viewportBottom) - 4))
    if (probe && probe !== m && !m.contains(probe)) {
        floor = Math.min(floor, probe.getBoundingClientRect().top)
    }
    const below = floor - rootRect.bottom - MARGIN
    const above = rootRect.top - MARGIN
    if (height > below && above > below) {
        menuStyle.value = { ...anchorStyle(rootRect, true), maxHeight: `${Math.min(height, above)}px` }
    } else if (height > below) {
        menuStyle.value = { ...anchorStyle(rootRect, false), maxHeight: `${Math.max(0, below)}px` }
    }
    // Re-anchor the focused option inside the resized/flipped scroller.
    nextTick(() => m.querySelector('.SelectBox__option--focused')?.scrollIntoView({ block: 'nearest' }))
}

const open = () => {
    if (props.disabled || menuOpen.value) return
    // Freeze the control's rendered width while open: the searchable state
    // swaps the value span for an <input>, whose intrinsic width differs from
    // the label's — without the pin the whole control jumps as the menu
    // toggles.
    if (root.value) {
        root.value.style.width = `${root.value.getBoundingClientRect().width}px`
    }
    menuOpen.value = true
    const selectedIdx = filteredOptions.value.findIndex(o => o.value === model.value && !o.disabled)
    focusedIndex.value = selectedIdx === -1 ? firstSelectableIndex() : selectedIdx
    nextTick(() => {
        placeMenu()
        // Pointer devices only: there, landing in the filter is a free head
        // start. On touch it summons the on-screen keyboard over the very
        // options the menu just opened to show — a cost paid by every user
        // picking from a short list with no intention of typing. The input is
        // still rendered there and still filters: its @click.stop keeps a tap
        // on it from closing the menu, so it focuses natively and the keyboard
        // arrives only for someone who asked to type.
        if (props.searchable && hasFinePointer()) searchInput.value?.focus()
    })
}

const close = () => {
    menuOpen.value = false
    search.value = ''
    menuStyle.value = null
    if (root.value) {
        root.value.style.width = ''
    }
}

const onControlClick = () => {
    if (props.disabled) return
    if (menuOpen.value) {
        close()
    } else {
        open()
    }
}

const select = (option: SelectBoxOption<T>) => {
    if (option.disabled) return
    model.value = option.value
    emit('pick', option.value)
    close()
}

const clearSelection = () => {
    model.value = null
    close()
}

const moveFocus = (dir: 1 | -1) => {
    const opts = filteredOptions.value
    if (!opts.length) return
    let i = focusedIndex.value
    for (let step = 0; step < opts.length; step++) {
        i = (i + dir + opts.length) % opts.length
        if (!opts[i].disabled) {
            focusedIndex.value = i
            return
        }
    }
}

// Escape must close only this dropdown, not any modal hosting it. The modal
// stack listens for keyup on document, so consuming the keydown alone is not
// enough — the matching keyup is swallowed too.
let swallowEscapeUp = false

const onKeyDown = (e: KeyboardEvent) => {
    switch (e.key) {
        case 'ArrowDown':
        case 'ArrowUp':
            e.preventDefault()
            if (menuOpen.value) {
                moveFocus(e.key === 'ArrowDown' ? 1 : -1)
            } else {
                open()
            }
            break
        case 'Enter':
            e.preventDefault()
            if (menuOpen.value) {
                const option = filteredOptions.value[focusedIndex.value]
                if (option) {
                    select(option)
                }
            } else {
                open()
            }
            break
        case 'Escape':
            if (menuOpen.value) {
                e.stopPropagation()
                swallowEscapeUp = true
                close()
            }
            break
    }
}

const onKeyUp = (e: KeyboardEvent) => {
    if (e.key === 'Escape' && swallowEscapeUp) {
        e.stopPropagation()
        swallowEscapeUp = false
    }
}

// Keep the focused option visible while stepping through a scrolled menu.
watch(focusedIndex, () => {
    nextTick(() => menu.value?.querySelector('.SelectBox__option--focused')?.scrollIntoView({ block: 'nearest' }))
})

// Typing changes the option list — restart focus from its top.
watch(search, () => {
    focusedIndex.value = firstSelectableIndex()
})

const onDocumentPointerDown = (e: PointerEvent) => {
    if (!root.value?.contains(e.target as Node)) {
        close()
    }
}

// The visible area can change under an open menu — the on-screen keyboard
// arriving or leaving, a pinch-zoom, the browser scrolling the visual viewport
// to clear a focused field — and each of those moves the floor placeMenu
// measured against. Coalesced to one re-place per frame: a viewport scroll
// fires far more often than the screen updates, and each pass reads layout.
let frame = 0
const onViewportChange = (): void => {
    if (frame) return
    frame = requestAnimationFrame(() => {
        frame = 0
        void placeMenu()
    })
}

const watchViewport = (on: boolean): void => {
    const vv = window.visualViewport
    if (on) {
        document.addEventListener('pointerdown', onDocumentPointerDown)
        // Capture, so a scroll in ANY ancestor container is heard: a
        // viewport-positioned menu does not travel with the control on its own,
        // and scroll events from a nested scroller do not bubble to the window.
        document.addEventListener('scroll', onViewportChange, true)
        vv?.addEventListener('resize', onViewportChange)
        vv?.addEventListener('scroll', onViewportChange)
        return
    }
    document.removeEventListener('pointerdown', onDocumentPointerDown)
    document.removeEventListener('scroll', onViewportChange, true)
    vv?.removeEventListener('resize', onViewportChange)
    vv?.removeEventListener('scroll', onViewportChange)
    if (frame) {
        cancelAnimationFrame(frame)
        frame = 0
    }
}

watch(menuOpen, watchViewport)
onUnmounted(() => watchViewport(false))

// Programmatic open, for hosts that summon the menu from outside the control
// (e.g. the location modal dropping the list when its option row is chosen).
defineExpose({ open })
</script>
<template>
    <div ref="root" class="SelectBox" :class="{ 'SelectBox--open': menuOpen, 'SelectBox--disabled': disabled }" @keydown="onKeyDown" @keyup="onKeyUp">
        <div class="SelectBox__control" role="combobox" :aria-label="ariaLabel ?? placeholder" :aria-expanded="menuOpen" :aria-disabled="disabled || undefined" :tabindex="disabled ? undefined : 0" @click="onControlClick">
            <!-- The committed value must stay evident while open: echo it as the
                 filter input's placeholder until the user types. -->
            <input v-if="searchable && menuOpen" ref="searchInput" v-model="search" class="SelectBox__search" type="text" :placeholder="selectedOption?.label ?? placeholder" @click.stop />
            <template v-else>
                <span v-if="selectedOption && htmlLabel" class="SelectBox__value" v-html="selectedOption.label" />
                <span v-else-if="selectedOption" class="SelectBox__value">{{ selectedOption.label }}</span>
                <span v-else class="SelectBox__placeholder">{{ placeholder ?? messages.selectOption }}</span>
            </template>
            <button v-if="clearable && selectedOption && !disabled" type="button" class="SelectBox__clear" :aria-label="messages.clear" @click.stop="clearSelection">&times;</button>
            <span class="SelectBox__indicator" aria-hidden="true" />
        </div>
        <div v-if="menuOpen" ref="menu" class="SelectBox__menu" role="listbox" :style="menuStyle">
            <div v-for="(option, i) in filteredOptions" :key="option.value" class="SelectBox__option" :class="{ 'SelectBox__option--focused': i === focusedIndex, 'SelectBox__option--selected': option.value === model, 'SelectBox__option--disabled': option.disabled }" role="option" :aria-selected="option.value === model" @click="select(option)" @pointerenter="!option.disabled && (focusedIndex = i)">
                <span v-if="option.depth" class="SelectBox__depth" :style="{ width: `${option.depth * 14}px` }" aria-hidden="true" />
                <span v-if="htmlLabel" v-html="option.label" />
                <template v-else>{{ option.label }}</template>
            </div>
            <div v-if="!filteredOptions.length" class="SelectBox__no-options">{{ messages.nothingFound }}</div>
        </div>
    </div>
</template>
<style lang="scss">
@use '@greendrake/scss-kit' as *;

.SelectBox {
    position: relative;
    // Never force a flex ancestor wider than its container: without this the
    // control's min-content width (a long, non-wrapping selected value) would
    // push the box past its container instead of ellipsising.
    min-width: 0;
    &__control {
        display: flex;
        align-items: center;
        gap: 4px;
        padding: var(--vs-padding, 4px 8px);
        // Match a text input's height from a single source: consumers point
        // --vs-height (and --vs-padding/--vs-border) at the same values their
        // inputs use. An explicit height (rather than a line-height-derived one)
        // keeps the control identical across engines — Firefox's line-height
        // clamp on <input> would otherwise size the searchable input taller than
        // the closed value. --vs-line-height is inherited by the value/search
        // descendants so their text line matches too.
        box-sizing: border-box;
        height: var(--vs-height);
        line-height: var(--vs-line-height, normal);
        border: var(--vs-border, 1px solid #e4e4e7);
        border-radius: var(--vs-border-radius, 4px);
        background-color: var(--vs-input-bg, #fff);
        color: var(--vs-text-color, inherit);
        cursor: default;
    }

    &__value,
    &__placeholder {
        flex-grow: 1;
        // min-width:0 lets the flex item shrink below its content so the
        // ellipsis actually engages (flex items default to min-width:auto).
        min-width: 0;
        overflow: hidden;
        white-space: nowrap;
        text-overflow: ellipsis;
    }

    &__placeholder {
        // Muted by colour rather than by transparency: a placeholder is text a
        // reader has to read, and 0.6 of the control's own colour lands below
        // the contrast bar on every surface this control sits on. The app's
        // muted token is a colour its theme has already had to defend.
        color: var(--font-color-muted, var(--vs-text-color, inherit));
    }

    // Scoped under __control (0,2,0) so app-level input skins — e.g. a global
    // `input:focus { background: … }` at (0,1,1) — can never paint a foreign
    // surface over the open control; the focus state re-declares for (0,2,1).
    &__control &__search {
        flex-grow: 1;
        min-width: 0;
        border: none;
        padding: 0;
        background: transparent;
        color: inherit;
        font: inherit;
        &:focus {
            outline: none;
            background: transparent;
        }
    }

    &__clear {
        @include control-clear;
    }

    &__indicator {
        @include control-chevron;
    }

    &--open &__indicator {
        transform: rotate(225deg);
        margin-bottom: -3px;
    }

    &--disabled &__control {
        opacity: 0.5;
        pointer-events: none;
    }

    &__menu {
        // Against the viewport, not the control: an ancestor with a scroll
        // container (a bottom-sheet modal, a scrollable panel) clips an
        // absolutely-positioned descendant, and a dropdown is exactly the thing
        // that has to reach past its container's edge. placeMenu supplies the
        // box, and re-supplies it whenever anything scrolls under it.
        position: fixed;
        z-index: z('raised');
        max-height: var(--vs-menu-height, 200px);
        overflow-y: auto;
        border: var(--vs-border, 1px solid #e4e4e7);
        border-radius: var(--vs-border-radius, 4px);
        background-color: var(--vs-menu-bg, #fff);
    }

    &__option {
        // Flex + min-height let a themed menu match its options to the
        // control's height exactly (zero the vertical padding and the row
        // centres within --vs-height); unthemed menus keep the padded default.
        display: flex;
        align-items: center;
        min-height: var(--vs-height, auto);
        padding: var(--vs-option-padding, 8px 12px);
        color: var(--vs-option-text-color, var(--vs-text-color, inherit));
        cursor: default;

        &--focused {
            background-color: var(--vs-option-hover-color, #dbeafe);
        }

        &--selected {
            background-color: var(--vs-option-selected-color, #93c5fd);
            color: var(--vs-option-selected-text-color, inherit);
        }

        &--disabled {
            opacity: 0.5;
        }
    }

    &__depth {
        flex-shrink: 0;
    }

    &__no-options {
        padding: var(--vs-option-padding, 8px 12px);
        opacity: 0.6;
    }
}
</style>
