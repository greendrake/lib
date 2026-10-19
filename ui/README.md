# @greendrake/ui

Vue 3 component library: form controls with blur-time validation, a focus-confining modal stack, async search, tabs, toasts, a page shell, a lazy tree, a paged thumbnail grid and flat multi-selection. Ships as TypeScript + SFC source; the consuming Vite app compiles it.

## Install

```sh
bun add @greendrake/ui vue
```

`vue` is the peer dependency. `@greendrake/util`, `@greendrake/domains`, `@greendrake/scss-kit` and `vue-tippy` come in as dependencies.

There is no build step and no `.d.ts`: `exports` points at `src/main.ts`, so the consumer's Vite + `vue-tsc` toolchain compiles and type-checks the SFCs directly. They are written against the `vueCompilerOptions` of `@greendrake/dev-config/tsconfig/vue.json` (`strictTemplates`, `data-*` attributes allowed); a consumer's tsconfig extends it:

```json
{
    "extends": "@greendrake/dev-config/tsconfig/vue.json",
    "include": ["src"]
}
```

## Styling contract

Component styles are SCSS inside the SFCs and resolve `@greendrake/scss-kit` (breakpoints, z-layers, icon masks, control mixins) at compile time; the custom properties those mixins read are listed in its README. Colours, spacing and typography come from CSS custom properties the consuming app defines. `AsyncAction`, `AsyncSearch`, `TreeView`, `ThumbnailGrid` and `Gallery` show progress by toggling a `spinner` class, which the app styles globally.

Read with no fallback, so the app defines them:

| Property | Read by |
| --- | --- |
| `--font-color` | text throughout: `AsyncSearch`, `BottomNav`, `Checkbox`, `ContextMenu`, `Gallery`, `Modal`, `Password`, `SearchField`, `SwitchRow`, `TreeView` |
| `--background-color` | `AsyncSearch` narrow panel |
| `--form-background`, `--form-background-hover` | control surfaces: `AsyncSearchResults`, `BottomNav`, `ContextMenu`, `SegmentedControl`, `TabBar`, `ThumbnailGrid`, `TreeView` |
| `--input-background` | `Checkbox` box |
| `--border-color`, `--border-color-light`, `--border-color-hover` | control and container borders; `--border-color-light` is also the `SplitPane` handle |
| `--border-radius` | `AsyncSearch`, `AsyncSearchResults`, `Checkbox`, `ThumbnailGrid` (`FieldTip` falls back to 4px) |
| `--border-radius-pill` | `SwitchRow` track |
| `--accent-color` | active and checked states: `BottomNav`, `Checkbox`, `SegmentedControl`, `SwitchRow`, `TabBar`, toasts; the hovered `SplitPane` handle; `TreeView` drop targets |
| `--space-1` … `--space-5` | spacing |
| `--font-xs`, `--font-md`, `--font-lg` | `BottomNav` and `ThumbnailGrid` labels, `FieldTip`, `Modal` title |
| `--line-height-normal` | `Password` |
| `--modal-background`, `--modal-border-radius` | `Modal` window |

Read with a local default:

| Property | Read by | Default |
| --- | --- | --- |
| `--checkbox-size` | `Checkbox` box | 24px, set on `.Checkbox` |
| `--switch-width`, `--switch-height`, `--switch-knob` | `Checkbox` with `switch` | 40px / 24px / `calc(var(--switch-height) - 4px)`, set on `.Checkbox--switch` |
| `--modal-box-shadow`, `--modal-backdrop-filter`, `--modal-window-backdrop-filter`, `--modal-button-min-width` | `Modal` | per property |
| `--circular-progress-arc`, `--circular-progress-label`, `--circular-progress-track` | `CircularProgress` | per property |
| `--row-select-color`, `--tree-line-color` | `TreeView` | per property |
| `--accent-contrast` | `SegmentedControl` active segment text | none |
| `--font-color-muted` | `SelectBox` placeholder, `BottomNav` inactive tabs | `--vs-text-color` / `--font-color` |
| `--bottomnav-height` | `BottomNav` bar height; bottom edge of the `AsyncSearch` narrow panel | none / 0px |
| `--safe-area-inset-top`, `--safe-area-inset-bottom` | toast layer; `Modal` sheet and `BottomNav` padding | 0px |
| `--max-content-width` | `TheWrap` width cap | none |
| `--tippy-background-color` | `FieldTip` validation tips, `InfoTip` tooltips | `#5335e8` |
| `--toast-color`, `--toast-background` | toasts | `#fff`; `--accent-color` (`--warning-color` for the warning variant) |
| `--color-attention-fill`, `--color-attention-contrast` | `BottomNav` badge | `--accent-color` / `#fff` |
| `--vs-*` | `SelectBox` (see the component) | per property |

Breakpoint: "narrow" is `@greendrake/scss-kit`'s 600px boundary; `Modal`, `TabBar` and `AsyncSearch` switch presentation below it.

Tooltips: `FieldTip`, `InfoTip`, `MenuButton`, `ContextMenu` and `AsyncSearch` use `vue-tippy`. Each ships the styles of the tippy theme it uses (`tip` for the first two, `HMenu`, `ContextMenu`, `asyncSearchResults`); no global tippy.js stylesheet is loaded or required.

## Messages

Every user-facing string lives in one reactive `messages` object (`UiMessages`). `configureUiMessages(overrides: Partial<UiMessages>)` merges overrides in; call it at boot and again on locale change.

```ts
import { configureUiMessages } from '@greendrake/ui'

configureUiMessages({ cancel: 'Abbrechen', nothingFound: 'Nichts gefunden' })
```

Keys and defaults: `loading` "Loading", `ok`, `cancel`, `close`, `clear`, `retryCountdown` "Error. Retrying in {seconds}" (`{seconds}` is substituted at render time), `nothingFound`, `searchResults`, `search`, `create`, `refresh`, `delete`, `deleting`, `newTab`, `copied`, `clickToCopy`, `invalidEmail` "Not a valid email address", `selectOption` "Select an option", `aboutThis` "About this".

`configurableMessages(defaults)` is the same arrangement for a package of its own: `{ messages, configure }`, `messages` reactive and starting as `defaults`. The `@greendrake/ui/messages` entry carries this module alone, for a module that has to load without the components — one under `bun test`, which compiles no SFC.

## Form controls

### TextField

`v-model: string` — always a string, `''` when blank. Renders an `<input>` (`type` is passed through), a `<textarea>` for `type="textarea"`, or the `richEditor` component for `type="rich"`.

Props: `type` (default `'text'`), `placeholder`, `ariaLabel` (defaults to the placeholder), `id` (lands on the input, so `focusIn(id)` and `<label for>` reach it), `autofocus` (pointer devices only, and only when nothing else holds focus), `validator?: (v: string) => true | string`, `validateOnBlur` (default `true`), `richEditor?: Component`. Exposes `focus()`.

Validation: while the typed value fails `validator` the model is `''`. On blur of a non-empty invalid value the returned message shows as a tooltip on the input (`FieldTip`); typing dismisses it. A form that provides `validationScopeKey` (see below) suppresses blur validation when focus leaves the form entirely.

`richEditor` is an injection point only: any component that honours the same string `v-model` contract (and accepts `ariaLabel`) is rendered in place of the input when `type === 'rich'`. The prop is read once at setup; `type="rich"` without it throws.

```vue
<script setup lang="ts">
import { ref } from 'vue'
import { Field, TextField } from '@greendrake/ui'

const slug = ref('')
const slugRule = (v: string): true | string => /^[a-z0-9-]+$/.test(v) || 'Lowercase letters, digits and dashes only'
</script>
<template>
    <Field label="Slug">
        <TextField v-model="slug" :validator="slugRule" placeholder="my-page" />
    </Field>
</template>
```

### Password

`v-model: string`. Password input with a show/hide toggle that preserves the caret. Props: `autofocus`, `minLength` (default 1), `id` (lands on the wrapper, so `FieldTip` can anchor on `#<id>`; the inputs are `#<id> input`).

### Email

`v-model: string`. `TextField` with `type="email"` and a validator backed by `isEmailValid` from `@greendrake/domains`; the failure message is `messages.invalidEmail`. Props: `placeholder`, `autofocus`, `id`, `validateOnBlur`. Other attributes fall through to the `TextField`.

### Checkbox

`v-model: boolean`. Props: `label`, `switch` (iOS-style sliding switch instead of a tick box), `disabled`, `indeterminate` (a tick box shown neither ticked nor not, a dash, whatever the value — a folder's tick over some of its files). Sizing via `--checkbox-size` / `--switch-*` (see Styling contract).

### CurrencyInput

`v-model: string | null` — the normalised amount (no thousands separators, `precision` decimals) or `null` when empty; the input itself shows separators as the user types. Props: `symbol` (rendered before the amount), `hideSymbol`, `precision` (default 2), `forceDecimals`, `doNotShowZeroDecimals`, `min` (input rejects values below it), `placeholder`, `ariaLabel`, `autofocus`, `disabled`.

### Field

Caption + control column. Props: `label`; `group` wraps the slot in a `role="group"` named by the label (for a set of controls); `unlabelable` leaves the caption as plain text (for a control a `<label>` cannot bind to, e.g. a contenteditable). Without either, the slot renders inside a `<label>` so the caption is the control's accessible name. Default slot.

### FieldTip

`FieldTip(source: Ref<string | false>, target: string | Ref<HTMLElement | ComponentPublicInstance | null>): void` — binds a manually-triggered tippy tooltip (theme `tip`) to an element or element id: setting `source` to a string shows it as text, `false` hides it; user dismissal resets `source` to `false`. Call it from `setup`; it cleans up on unmount.

`<InfoTip text="…" />` — a small "?" button whose `text` shows in a tooltip (theme `tip`) on hover, on focus, and on a tap where there is no hover; its accessible name is `messages.aboutThis`. Place it beside a field's `<label>`, not inside: within one it would become the control the label names.

### validationScopeKey

`InjectionKey<Ref<HTMLElement | null>>`. A form component provides a ref to its root element under this key; descendant `TextField`s then skip blur validation when focus leaves that element (e.g. the user clicks elsewhere without finishing the form).

## Form submission

### FormC

A `<form>` whose submit button doubles as the status display, driven by a state machine: rest states `invalid` / `dirty` / `saved` derive from `refs.valid` / `refs.dirty` (a missing ref counts as `true`); a submission dictates `confirm` → `inprogress` → `success` | `error`, each dictated state expiring back to rest after 2000ms. The root carries `form form-<state>` and the button `button button-<state>`; the button is disabled unless the state is `dirty` or `confirm`.

Props: `action?: () => Promise<unknown> | unknown`, `askToConfirm` (first submit enters `confirm`, the next one submits), `refs?: { dirty?: Ref<boolean>; valid?: Ref<boolean> }`, per-state button labels `savedText`, `dirtyText`, `invalidText`, `confirmText`, `inprogressText`, `successText`, `errorText` (a missing label shows the raw state name), `noContent` (empty button, state via classes only), `tryPersistently` (retries `action` until it succeeds via `Retrier` from `@greendrake/util`, showing `messages.retryCountdown` in the button between attempts), `successTimeout` (`true`/unset → 2000ms; a number → that many ms; `false` → skip the success state). Default slot holds the fields.

### AsyncAction

Button (or `tag="a"`) for a one-shot async action: label switches between `labelDirty` / `labelDone` / `labelInProgress`, the element disables itself while the promise is pending, with a `spinner` class meanwhile. Props: `promiseMaker: () => Promise<void>` (required), `labelDirty` (required), `labelDone`, `labelInProgress`, `isDirty`, `isValid`, `showSpinner` (default `true`), `type` (default `'button'`), `tag` (default `'button'`). Exposes `run()`.

### useAsyncAction, errorMessage

`useAsyncAction(fn)` is the bookkeeping around an action button of the page's own markup: `{ acting, error, notice, run }` (`AsyncActionState<Args>`). `run(...args)` is ignored while `acting`; it clears `error` and `notice`, awaits `fn(...args)`, takes a string it resolves to as the `notice`, and catches a rejection into `error` as its message — the failure is shown where the action was taken, so `fn` makes a silent call. Buttons sharing one in-flight state parameterise `fn` and pass the variant to `run`.

```vue
<script setup lang="ts">
import { useAsyncAction } from '@greendrake/ui'

const save = useAsyncAction(async () => {
    await saveThing(draft.value) // reports its failure to nobody else
    return 'Saved'
})
</script>
<template>
    <button :disabled="save.acting.value" @click="save.run()">Save</button>
    <span v-if="save.notice.value">{{ save.notice.value }}</span>
    <span v-if="save.error.value" class="error">{{ save.error.value }}</span>
</template>
```

`errorMessage(e)` is a caught value as text: an `Error`'s message, anything else stringified.

### OptionSwitch

`v-model: T` (`T extends string | number | boolean`). Radio-style switch: clicking an option runs `saveHandler` through the same state machine as `FormC` (optionally with a confirm step) and commits the model on success. Props: `options: OptionsArray<T> | OptionsMap<T>` (required; array entries are bare values or `[value, title]` tuples, the map form is `Map<value, title>`), `label`, `requireConfirmation`, `disabledOptions: T[]`, `saveHandler?: (value: T) => Promise<void> | void`. Emits `click(event)`. A slot named after `String(value)` adds markup to that option. Classes: `.OptionSwitch` gets `saving` / `error`; options get `current`, `confirm`, `disabled`. A model value outside `options` throws at setup.

### SegmentedControl

`v-model: string` (required). Pill-shaped radio group backed by real radio inputs. Props: `options: SegmentedOption[]` (`{ value, label }`), `ariaLabel` (names the group; makes the container an explicit `radiogroup`).

## Search

### SearchField

`v-model: string`. Text input with search glyph, clear button and Escape-to-clear. Prop `debounce?: boolean | number | ((value: string) => number)` delays model updates: `true` → 600ms, a number → that many ms, a function → decided per keystroke from the prospective value. Also `placeholder`, `autofocus`. Exposes `flush(): boolean` — commits the un-debounced value immediately, dropping any pending update; returns whether the model changed. `class`/`style` land on the root, every other attribute on the inner input.

### AsyncSearch

`SearchField` plus a results surface. Props: `search: SearchFunction` (required), `placeholder`, `autofocus`, `itemComponent?: Component | null` (rendered per result with a `data` prop; the default renderer shows `htmlTitle` via `v-html`), `searchKey?: unknown` (re-runs the current query when it changes), `minQueryLength` (default 3). Emits `result(data: SearchResultItem)` on pick — nothing else happens on pick; the host navigates, or calls the exposed `clear()` (empties the query) or `hide()` (dismisses the results, query kept). Slots: `resultsFooter` (strip under the list), `empty` (replaces the "nothing found" row). Enter searches the current input immediately; stale responses of superseded queries are dropped.

```ts
type SearchFunction = (query: string, isSearching: Ref<boolean>) => Promise<SearchResultItem[]>
interface SearchResultItem {
    id: string
    htmlTitle?: string // trusted/escaped markup; used by the default renderer only
    [key: string]: unknown
}
```

`isSearching` is handed to the function so it can drive the field's spinner for exactly the duration of its call. Wide viewports show the results in a tippy popper anchored under the field (theme `asyncSearchResults`); below the narrow breakpoint they take over the viewport from the field's bottom edge down to `--bottomnav-height`, unless the field sits inside a `Modal`, where the panel stays in flow.

## Modals

### Modal

Centred dialog, or a bottom sheet with drag-to-dismiss below the narrow breakpoint. Teleported to `body`; attributes (e.g. `class`) land on the teleported root. The consumer owns visibility (`v-if`): `ok` and `cancel` are emits only.

Props: `title` (titled header with an explicit close), `closable` (X in the toolbar), `expandable` (full-viewport toggle), `buttons` (default `true`), `okLabel` / `cancelLabel` (default `messages.ok` / `messages.cancel`), `okDisabled`, `hideOk`, `hideCancel`, `mainClass` (class on the content area), `initialFocus: 'field' | 'dialog'` (default `'field'`; see the stack). Emits `ok`, `cancel`. Slots: default (content), `buttons` (replaces the button row), `ok` and `cancel` (button contents). Exposes `rootEl`.

The modal stack is module-level and automatic: every mounted `Modal` registers. Escape and `popstate` close the top-most modal only (through its `cancel` emit); everything outside the top-most modal's root is made `inert`, Tab wraps inside it, stray focus is pulled back in; initial focus goes to the first field (to the dialog itself for `initialFocus="dialog"` or on touch devices, where a focused field would summon the keyboard); closing restores focus to the invoker; body scroll is locked while any modal is open.

```vue
<script setup lang="ts">
import { ref } from 'vue'
import { Modal, TextField } from '@greendrake/ui'

const props = defineProps<{ rename: (title: string) => Promise<void> }>()
const open = ref(false)
const title = ref('')
const save = async (): Promise<void> => {
    await props.rename(title.value)
    open.value = false
}
</script>
<template>
    <button type="button" @click="open = true">Rename</button>
    <Modal v-if="open" title="Rename" :ok-disabled="!title" @ok="save" @cancel="open = false">
        <TextField v-model="title" placeholder="New title" />
    </Modal>
</template>
```

### ModalOnClick

Trigger element plus a `Modal` around the `content` slot. The default slot is the trigger (`tag`, default `'a'`; `id`, `type`, `ariaLabel` and other attributes land on it); `modalClass` and `modalTestid` (its `data-testid`) go to the modal. Modal chrome props `title`, `okLabel`, `cancelLabel`, `okDisabled`, `hideOk`, `closable`, `expandable`, `buttons` are forwarded as given, unset ones keeping `Modal`'s defaults. Emits `click(event)` and `open` on trigger, `ok` (re-emitted without closing — an async submitter keeps the modal open on failure and closes via the exposed `onCancel()` on success), `cancel` (closes, then re-emits). Exposes `open(event)`, `onCancel()`.

### Gallery

Full-screen image viewer in an expanded `Modal`. Props: `items: GalleryItem[]` (`{ url, alt?, caption? }`), `startIndex` (default 0). Emits `cancel`. Arrow keys and horizontal swipes navigate; a swipe past either end, a tap outside the image, or the image click closes.

## Menus and selects

### MenuButton

Hover/click dropdown built on `vue-tippy`'s `Tippy` (theme `HMenu`, placement `bottom-end`), appended to `body`, or to the element filling the screen while one does; any click inside the menu closes it. Slots: `main` (trigger; slot prop `open`, whether the menu shows, for the trigger's `aria-expanded`), `menuitems` (menu content, wrapped in `.Menubutton-Menu`). Props: `menuClass` (on the wrapper), `boxClass` (on the tippy box, applied once on create), `offset` (default `[0, -1]`), `matchTriggerWidth` (pins the popper width to the trigger's).

### ContextMenu

A menu at the pointer in place of the browser's own, built on `vue-tippy`'s `Tippy` (theme `ContextMenu`), appended to `body`. Prop `items: ContextMenuItem[]` (`{ label, run: () => unknown }`), rendered as `role="menuitem"` buttons. Exposes `open(event: MouseEvent)`: call it from a `contextmenu` listener — it cancels the browser's menu and shows this one at the event's point. Choosing an item closes the menu and runs the item; a press outside the menu, or any key, closes it. Place it inside the element it serves: a press there closes it too.

### SelectBox

`v-model: T | null` (`T extends string | number`; `null` = no selection, shows the placeholder). Single-select dropdown with keyboard support (arrows, Enter, Escape closes the menu without reaching a hosting modal). Props: `options: SelectBoxOption<T>[]` (`{ value, label, disabled?, depth? }` — `depth` indents the option in the menu), `placeholder` (default `messages.selectOption`), `ariaLabel`, `clearable`, `searchable` (type-to-filter input while open), `disabled`, `htmlLabel` (labels via `v-html`; trusted content only). Emits `pick(value)` on every menu selection, including re-picking the current one. Exposes `open()`. The menu is positioned against the viewport, flips upward or clamps its height when fixed chrome would cover it, and follows scroll and on-screen-keyboard changes.

Skin via `--vs-*`, each with a fallback: `--vs-height` (unset → auto), `--vs-padding` (`4px 8px`), `--vs-line-height` (`normal`), `--vs-border` (`1px solid #e4e4e7`), `--vs-border-radius` (`4px`), `--vs-input-bg` (`#fff`), `--vs-text-color` (`inherit`), `--vs-menu-height` (`200px`), `--vs-menu-bg` (`#fff`), `--vs-option-padding` (`8px 12px`), `--vs-option-text-color`, `--vs-option-hover-color` (`#dbeafe`), `--vs-option-selected-color` (`#93c5fd`), `--vs-option-selected-text-color`.

## Tabs

### TabBar

Props: `tabs: TabDef[]` (`{ id, label, closable? }`), `modelValue: string | null`, `closable` (default `false`; per-tab `closable` overrides). Emits `update:modelValue(id)`, `close(id)`. The default slot renders a panel under the strip; below the narrow breakpoint the strip becomes a vertical accordion with the panel directly under the active tab.

```vue
<script setup lang="ts">
import { ref } from 'vue'
import { TabBar, type TabDef } from '@greendrake/ui'

const tabs: TabDef[] = [
    { id: 'general', label: 'General' },
    { id: 'security', label: 'Security' }
]
const active = ref<string | null>('general')
</script>
<template>
    <TabBar v-model="active" :tabs="tabs">
        <GeneralPane v-if="active === 'general'" />
        <SecurityPane v-else-if="active === 'security'" />
    </TabBar>
</template>
```

## Toasts

`showToast(options: ToastOptions): ToastHandle` — imperative top-centre toast, rendered without an app context (callable from stores and plain modules). Toasts raised together stack in order.

```ts
interface ToastOptions {
    content: string | VNode // text, or a VNode with its handlers already bound
    variant?: 'accent' | 'warning' // default 'accent'
    hideOnClick?: boolean // default true: dismissed by the next key press / pointer down outside the toast layer
    blocking?: boolean // must be answered: an overlay holds the page until the toast goes, and no interaction elsewhere dismisses it
    onHidden?: () => void
}
```

```ts
import { showToast } from '@greendrake/ui'

showToast({ content: 'Saved' })
const failure = showToast({ content: 'Upload failed', variant: 'warning', hideOnClick: false })
failure.destroy()
```

## Layout

### TheWrap

Page shell: `top` slot and default (routed content) stretch, the `bottom` slot is pushed to the viewport bottom on short pages; a lone child (e.g. a splash) is centred vertically. Width capped by `--max-content-width`.

### BottomNav

Fixed bottom tab bar for narrow viewports; the app hides it on wide ones. Props `tabs: BottomNavTab[]` and `link: Component`, what a navigating tab renders as, handed the tab's `to` — the app's router link (`RouterLink` from `vue-router`, or any component taking a `to`). Emits `select(id)`. Default slot is a trailing cell for page actions.

```ts
interface BottomNavTab {
    id: string
    label: string
    to?: string // the link's destination; omit for a tab that toggles a shell overlay (rendered as a button, reports via `select` + aria-expanded)
    active: boolean // the app owns the route → tab mapping
    badge?: number
}
```

Per-tab icons are the app's: style `.BottomNav__icon--<id>` with a `mask-image`. The bar's height is `--bottomnav-height` plus `--safe-area-inset-bottom`.

### SplitPane

Two panes side by side, the start one resized by dragging the handle between them. Props: `initialWidth` (default 150) and `minWidth` (default 100), the start pane's. Slots: `start`, `end`; each pane is a flex column whose children grow to fill it, the start one scrolling its overflow. Without an `end` slot (e.g. a conditional `<template v-if="…" #end>`) the start pane spans the whole width and no handle renders.

### Splash, Loading

`Splash` centres its default slot (logo) above a `Loading` indicator, covering its positioned ancestor. `Loading` renders `messages.loading` with animated dots.

## Tree

### TreeView

Lazy tree rendered as one flat list of visible nodes. Props: `loader: TreeLoader` (required), `idField` (default `'key'`), `labelField` (default `'title'`), `editField` (the field a rename edits; default `labelField`), `multiSelect` (default `true`), `draggable` (default `false`), `onRename?: (node: TreeNodeId, value: string) => Promise<void>`, `onAddChild?: (parent: TreeNodeId, value: string) => Promise<void>`. `v-model:selected: TreeNodeId[]`. Children load on first expand and are cached per node. Selection runs on `useSelection`: click selects, Shift extends a range over the visible list, Ctrl/Cmd toggles; the list is focusable, the arrows move the cursor (Shift extending the range) and Ctrl/Cmd+A selects all. A right-click focuses its row — selected alone unless it is selected already — and emits `contextmenu(event)`; the consumer opens its menu.

A node may have several parents and then shows under each: rows are keyed by their path from the root, while the children cache, expansion and selection stay per node. A node's loaded children decide whether its row expands, over the `expandable` its parent's load carried. The cursor's row is the focused occurrence (`aria-current`).

In-place editing: with `onRename`, double-clicking a label turns it into an input holding the node's `editField`; with `onAddChild`, `addChild()` opens an empty input last under the focused row. Enter hands a changed or non-empty, trimmed value to the callback and closes the input once it resolves — a rejection leaves it open — and Escape or leaving the input drops it. Keys typed there stay in the input.

Exposed: `reload(nodeId | null)` re-fetches a node's children (`null`: the root level), keeping what is expanded; `expand(nodeId)` expands a node wherever it shows, loading its children first; `levelsOf(nodeId)` lists the loaded levels holding a node — the parents it shows under, `null` for the root level — the levels to reload when it changes or goes; `focused` is the focused occurrence while its row is visible, else `null`; `addChild()`, above.

With `draggable`, rows drag onto rows: the upper third of the target row drops before it, the lower third after it, the middle inside it. A row whose path passes through the dragged node — any occurrence of it, or any row under one — is no target. The target row carries `data-drop` (`before` | `inside` | `after`) while hovered; the drop emits `move`, which the consumer carries out and reloads after.

```ts
interface TreeNode {
    expandable: boolean
    [key: string]: unknown // must include [idField] and [labelField]
}
type TreeNodeId = string | number
type TreeLoader = (parentId: TreeNodeId | null) => Promise<TreeNode[]> // null → root level
interface TreeOccurrence {
    node: TreeNodeId
    parent: TreeNodeId | null // null: at the root level
}
type TreeMove = { node: TreeNodeId; from: TreeNodeId | null } & (
    | { position: 'inside'; to: TreeNodeId }
    | { position: 'before' | 'after'; to: TreeNodeId | null; relativeTo: TreeNodeId }
)
```

### Accordion

Independently collapsible sections, all open initially. Prop `sections: AccordionSection[]` (`{ id, title, ...extra }`). Slots: default and `header`, both receiving `{ section }`; `header` holds per-section controls that do not toggle.

## Thumbnails

### ThumbnailGrid

Paged multi-select grid of thumbnails. Props: `loader: ThumbnailGridLoader` (required), `filters` (reloads from the first page when it changes), `thumbnailField` (default `'thumbnail'`), `titleField` (default `'title'`), `idField` (default `'id'`), `pageSize` (default 24). `v-model:selected: Array<string | number>`; clicking a tile toggles it. Exposes `refresh()`.

```ts
interface ThumbnailGridLoaderParams {
    offset: number
    limit: number
    [key: string]: unknown // the filters, spread in
}
type ThumbnailGridLoader = (params: ThumbnailGridLoaderParams, loading: Ref<boolean>) => Promise<{ data: ThumbnailGridItem[]; total: number }>
```

## Selection

### useSelection

`useSelection<Id extends string | number>(items: Ref<Record<string, unknown>[]> | ComputedRef<...>, idField: string, { keyField?: string, multi?: MaybeRefOrGetter<boolean> })` — flat, index-addressable multi-selection over an ordered list. The selection holds ids (`items[i][idField]`); the cursor and the anchor ranges extend from are rows, addressed by `items[i][keyField]` (default `idField`) — distinct when an item shows in several rows, as a tree node under two parents does. Without `multi` (default `true`), every pick and move selects one row. Returns:

- `selected: Set<Id>` (shallowReactive);
- `cursor: ShallowRef<string | number | null>`: the cursor's row key; setting it moves the cursor alone;
- `getId(index)`;
- `replace(key)`: selects the row alone, cursor and anchor on it;
- `pick(key, event: MouseEvent)`: a pointer's pick — Shift selects the range from the anchor (with no anchor in the list, it toggles), Ctrl/Cmd toggles the row, a plain pick replaces;
- `onKeyDown(event: KeyboardEvent): number`: Ctrl/Cmd+A selects all; ArrowUp/ArrowDown move the cursor, selecting its row alone, or with Shift the range from the anchor to it. Returns the cursor's new index, for the host to scroll into view, or -1.

`InfiniteScrollTable`, `RowSelect` and `TreeView` run on it.

### RowSelect

`RowSelect<Id>(items, idField, onReorder?: (orderedIds: Id[]) => void)` — pointer-driven selection for a `<table>` host: click / Ctrl / Shift row selection via `useSelection`, rubber-band drag over empty space, and, when `onReorder` is given, drag-to-reorder of rows. Returns `{ selected, containerRef, band, reorder, onPointerDown, onKeyDown }`: bind `containerRef` to the table's scroll container and wire `onPointerDown` / `onKeyDown` to it; `band` (`{ active, left, top, width, height }`) and `reorder` (`{ active, fromIndex, toIndex }`) are reactive state to render the band rectangle and the drop indicator from. Interactive descendants (`a`, `button`, inputs, `[role="button"]`) are left alone.

## Misc

- `InfoBlock` — a notice set apart by `--infoblock-line-color` and the `info` glyph; an `actions` slot stacks controls beneath the message.
- `SwitchRow` — an on/off switch shaped as a row (`v-model`, `label`, `title`): an `<a role="switch">`, so it sits among menu items, its layout the host class's.
- `CircularProgress` — SVG ring, prop `progress: number` (0–100, clamped), `role="progressbar"`; colours via `--circular-progress-track` / `--circular-progress-arc` / `--circular-progress-label`.
- `Copy` — click-to-copy icon for `value: string`; `showValue` renders the value beside it; a `copied` class marks success for 2s. Title is `messages.clickToCopy`.
- `copyTextToClipboard(text: string): Promise<void>` — `navigator.clipboard.writeText`; requires a secure context.
- `downsizeImage(file: File, opts?: { maxDimension?: number; maxSizeKB?: number; minQuality?: number }): Promise<File>` — re-encodes an oversized image as JPEG, scaling to `maxDimension` (2000) and stepping quality down until under `maxSizeKB` (1024) or `minQuality` (0.2). Non-images, files within both limits, and undecodable files pass through untouched.
- `scrollToTop()` — `window.scroll({ top: 0 })`.
- `focusIn(el: string | HTMLElement | null, selector?: string)` — focuses the element with that id, or the first `selector` match inside `el`, unless it already has focus.
- `TOUCH_PAN_SLOP_PX` — 10; the travel a touch must cover before it counts as a directional gesture; exported so other gesture-arbitrating components share it.
- `capturePointer(event: PointerEvent)` — captures the pointer to `event.currentTarget`, so a drag keeps tracking it outside the element; a pointer already released is a no-op.
- `useWidthDrag(minWidth: MaybeRefOrGetter<number>): WidthDrag` — a width dragged by a handle at its end edge, never below `minWidth`. Returns `{ active, start(event, width, set), move(event), end() }`: wire `start` to the handle's `pointerdown` (it captures the pointer and drags from `width`, handing each new width to `set`), `move` to `pointermove`, `end` to `pointerup` and `pointercancel`; `active` is true while a drag is in progress. `SplitPane` uses it.
