# @greendrake/ui-data

Data-heavy dashboard components on top of `@greendrake/ui`: a virtualised `InfiniteScrollTable`, the `MasterDetail` split and the `CrudPanel` shell built on it, the `useRecordEditor` diff-and-patch editing convention, and an admin-CRUD kit of sort state, detail composables, formatters, the `NoteActions` shell and `DescriptionList`. Ships as TypeScript + SFC source; the consuming Vite app compiles it.

## Install

```sh
bun add @greendrake/ui-data vue
```

`vue` is the peer dependency; `@greendrake/ui` and `@greendrake/scss-kit` come in as dependencies.

No build step, no `.d.ts`: `exports` points at `src/main.ts` and the consumer's Vite + `vue-tsc` toolchain compiles the SFCs, whose generics (`InfiniteScrollTable<T>`, `CrudPanel<T>`) are written against the `vueCompilerOptions` of `@greendrake/dev-config/tsconfig/vue.json`. A consumer's tsconfig extends it:

```json
{
    "extends": "@greendrake/dev-config/tsconfig/vue.json",
    "include": ["src"]
}
```

## Styling contract

As for `@greendrake/ui`: the app defines the custom properties that package's README lists as read with no fallback — this one reads a subset of them. The table and panel additionally read `--cell-padding`, `--list-row-padding`, `--row-border-bottom`, `--row-select-color` and `--row-select-color-hover`, each with a fallback.

Layout: `InfiniteScrollTable` measures its own container to decide how many rows to render, so its height must flow top-down. Place it in a flex column whose items have `min-height: 0`, or pass `visibleHeight`; a content-driven height makes the measurement loop until every row is rendered.

## InfiniteScrollTable

Virtualised table: only the rows that fit the measured viewport (plus two clipped ones) are in the DOM, scrolling is pixel-continuous through a custom scrollbar, the wheel, touch panning with inertia, and the keyboard when `selectable`. Rows arrive through the loader in `bufferSize` windows around the viewport; when a single response holds the whole dataset (`data.length >= total`) the table keeps it in memory and serves text-filter refinements and sorting locally without further calls. Columns are resizable by dragging header edges.

### Loader contract

```ts
interface LoaderParams {
    offset: number
    limit: number
    text?: string // the `text` prop, when there is any
    sort?: Sort // the `sort` prop, when there is one
    filters: Record<string, string> // the `filters` entries that are not empty
}
interface Sort {
    by: string
    dir: SortDir // 'asc' | 'desc'
}
interface LoaderResponse<T> {
    data: T[]
    total: number
}
type LoaderFunction<T> = (params: LoaderParams, loading: Ref<boolean>, background: boolean) => Promise<LoaderResponse<T>>
```

The params are in no backend's terms: what the loader's API calls the text, the order and the filters is the loader's to map.

`loading` is the table's spinner flag, handed to the loader to set around its call. The spinner also shows while the viewport overlaps rows not yet buffered, so a loader that never touches `loading` still gets one for gaps.

`background` is true when nobody asked for the load — a live binding re-reading after a server push or a dropped connection (`refresh(true)`). What that means for the request is the loader's to decide — typically a call that shows no spinner and raises no error prompt.

`T` must satisfy `Record<string, unknown>`, so declare row types as type aliases (`type User = { … }`), not interfaces: an interface carries no implicit index signature and is rejected where the table's generic is inferred.

```ts
type Filters = Record<string, string | null | undefined> // an empty value (null, undefined, '') is no filter
```

A change to `text`, `sort` or `filters` re-fetches from offset 0, unless the whole dataset is in memory and the filters are as it was fetched under: then a new order, or a text containing the one it was fetched under (only narrowing it), is applied locally — the text over `localSearchFields`.

### Props

| Prop | Type | Default |
| --- | --- | --- |
| `loader` | `LoaderFunction<T>` | required |
| `columns` | `ColumnDefinition<T>[]` | required |
| `rowKey` | `RowKey<T>` — field name, or `(row) => string \| number` for composite keys | `'id'` |
| `rowHeight` | px | 45 |
| `visibleHeight` | px, or `null` to measure the container | `null` |
| `filters` | `Filters` | `{}` |
| `text` | the search text; empty is none | — |
| `sort` | `Sort`, the order | — |
| `localSearchFields` | fields the in-memory text filter matches; `false` sends every text and order change to the loader | `['title']` |
| `selectable` | pointer/keyboard row selection (wrapper gets `tabindex="0"`) | `false` |
| `rowDraggable` | rows are `draggable`, drop targets are tracked | `false` |
| `hideHeader` | | `false` |
| `onRowClick` | `(record: T) => void` | — |
| `bufferSize` | rows fetched per request | 1000 |
| `renderAll` | render every buffered row (test affordance for datasets within one buffer) | `false` |

```ts
interface ColumnDefinition<T> {
    field: string
    label: string
    className?: string // on th and td
    cellClass?: (value: unknown, record: T) => string | undefined
    sortable?: boolean
    sortType?: 'string' | 'number' | 'date' // for local sorting; inferred from the data otherwise
    sortIcon?: string // rendered after the label
    onHeaderClick?: () => void
    onCellClick?: (value: unknown, record: T) => void
    formatter?: (value: unknown, record: T) => string
    wrap?: boolean // multi-line cell instead of single-line ellipsis
    component?: Component // custom cell renderer
    componentProps?: (record: T) => Record<string, unknown>
}
```

Selection: mouse click selects, Shift extends a range, Ctrl/Cmd toggles; touch taps select; Arrow keys move the cursor (Shift extends), Ctrl/Cmd+A selects all.

### Emits

- `range-changed(range: VisibleRange)` — `{ start, end, total }`, the 1-based window of fully visible rows; emitted immediately and on every change.
- `rowactivate(record: T)` — double-click.
- `rowdragstart(record: T, event: DragEvent)`, `rowdrop(event: DragEvent, target: DropTarget<T> | null)` — with `rowDraggable`; `DropTarget` is `{ record, position: 'before' | 'after' }`, the hovered row carrying a `drop-before` / `drop-after` class meanwhile.

Slot `empty` replaces the `messages.nothingFound` row.

### Exposed

Type a template ref as `ref<InfiniteScrollTableExposed<Row>>()`; a generic SFC instance has no nameable `InstanceType`.

```ts
interface InfiniteScrollTableExposed<T> {
    refresh: (background?: boolean) => Promise<void> // re-fetch around the current position; the promise settles with the load
    patchRow: (keyValue: string | number, data: Partial<T>) => boolean
    addRow: (data: T, position?: RowPosition) => void // 'start' (default) | 'end' | { after: key } | { before: key }
    removeRow: (keyValue: string | number) => void
    removeRows: (keyValues: (string | number)[]) => void
    resort: () => void // re-apply local filter/sort in place (after patching a sorted field)
    getRow: (keyValue: string | number) => T | undefined
    willFilterLocally: (value: string | undefined) => boolean // whether a text-filter change to `value` stays in memory
    selected: Set<string | number> | undefined // the selection when `selectable`
}
```

Row surgery (`patchRow`, `addRow`, `removeRows`) is the way to reflect a single-record change; `refresh()` after one is a wasted round trip.

### Example

```vue
<script setup lang="ts">
import { ref, computed } from 'vue'
import { InfiniteScrollTable, useSortState, fmtDate, type LoaderFunction, type InfiniteScrollTableExposed } from '@greendrake/ui-data'

type User = { id: string; email: string; created_at: string }

const sort = useSortState('created_at')
const columns = computed(() => [sort.column<User>('email', 'Email'), sort.column<User>('created_at', 'Created', { formatter: fmtDate })])
const search = ref('')

// listUsers is the app's own call, resolving to { data, total }
const loader: LoaderFunction<User> = async (params, loading) => {
    loading.value = true
    try {
        return await listUsers({ from: params.offset, count: params.limit, email: params.text, order: params.sort })
    } finally {
        loading.value = false
    }
}
const table = ref<InfiniteScrollTableExposed<User>>()
</script>
<template>
    <input v-model="search" />
    <InfiniteScrollTable ref="table" :loader="loader" :columns="columns" :text="search" :sort="sort.sort.value" row-key="id" selectable />
</template>
```

## MasterDetail

A `SplitPane` from `@greendrake/ui`: the resizable master pane on the left, tabbed detail panes on the right (a `TabBar` with closable tabs). Props: `initialWidth` and `minWidth` (the master's; `SplitPane`'s defaults, 150 and 100), `fullWidthUntilTab` (default `false`; the master spans the whole width until a tab opens). Emits `tab-open`, `tab-close`, `tab-activate`, each with the `TabDef`. Slots: `master` receives `{ openTab }`; `detail` receives `{ tab, isActive }` and is rendered once per open tab, inactive panes kept alive with `v-show`. Exposes `openTab(id, label?)` (opens or activates), `closeTab(id)`, `activateTab(id)`, `updateTab(id, newId?, newLabel?)`, `tabs`, `activeTab`, `activeTabId`.

## CrudPanel

Master/detail CRUD shell: a controls bar (search field, optional Create button, refresh, visible-range readout, optional Delete button) over an `InfiniteScrollTable` in the master pane, and detail tabs opened by double-clicking a row. A panel without a `detail` slot is table-only: rows select (for deletion) and never open a pane.

| Prop | Type | Default |
| --- | --- | --- |
| `columns` | `ColumnDefinition<T>[]` | required |
| `loader` | `LoaderFunction<T>` | required |
| `rowKey` | `RowKey<T>` | `'id'` |
| `rowHeight` | px | 28 |
| `filters` | `Filters`, forwarded to the table, which takes its `text` from the search box | `{}` |
| `sort` | `Sort`, forwarded to the table | — |
| `initialWidth`, `minWidth`, `fullWidthUntilTab` | forwarded to `MasterDetail` | 200, 120, `true` |
| `showRange` | render the `start-end/total` readout | `true` |
| `localSearchFields` | `string[] \| false \| null`; `null` keeps the table's default | `null` |
| `rowLabel` | `(record: T) => string`, a row's detail tab label; `null` labels it with the row key | `null` |
| `creatable` | Create button, opens a `new:<n>` tab | `false` |
| `newTabLabel` | label of that tab | `messages.newTab` |
| `onDelete` | `(keys: Key[]) => Promise<unknown>`; enables the Delete button for the selection | `null` |
| `deleteConfirmMessage` | `string \| ((keys: Key[]) => Promise<string>)`; when set, deletion asks first in a `Modal` with this text, or the text the function composes for the selected keys | `null` |
| `keyExists` | `(key: Key) => boolean`, backs the `isDupe` slot prop | `null` |

The search field's debounce is `localFilterDebounce` over the table: swift when the change resolves in memory, longer when it triggers a fetch. A single column hides the table header.

Slots: `controls-left` (after the search field and Create), `controls-middle` (`{ range: VisibleRange }`), `controls-right` (after Delete; `{ selected: Set<Key> | undefined }`), `detail` (`{ tab: TabDef, isNew: boolean, record: T | undefined, isDupe: (original: Key, candidate: Key) => boolean }`). `record` is the live table row for the tab's key (`undefined` for a `new:` tab or a row the table has not seen); `isDupe` flags a candidate key that differs from `original` and already exists, for rename and create forms.

Exposed (`CrudPanelExposed<T>`; type the ref as `ref<CrudPanelExposed<Row>>()`): `openTab`, `closeTab`, `updateTab`, `removeRows(keys)` (also clears the selection), `addRow`, `patchRow`, `getRow`, `refresh`, `applySaved(key, record, tabId?)`, and readonly `selected`, `range`, `deleting`.

`applySaved` is the post-save hook of the record-edit convention below: it patches the row in place when the key exists, otherwise adds the row and, given the originating `new:` tab's id, renames that tab to the persisted key so the `record` slot prop resolves and the next save updates instead of creates. With a `rowLabel`, the record's tab is relabelled from the saved record.

```vue
<script setup lang="ts">
import { ref, computed } from 'vue'
import { CrudPanel, useSortState, type CrudPanelExposed, type LoaderParams } from '@greendrake/ui-data'
import HostEditor from './HostEditor.vue'

type Host = { name: string; address: string; enabled: boolean }

const sort = useSortState('name', 'asc')
const columns = computed(() => [sort.column<Host>('name', 'Name')])
// listHosts / deleteHosts are the app's own calls; listHosts resolves to { data, total }
const loader = (params: LoaderParams) => listHosts(params)
const crud = ref<CrudPanelExposed<Host>>()
</script>
<template>
    <CrudPanel ref="crud" :columns="columns" :loader="loader" :sort="sort.sort.value" row-key="name" creatable :on-delete="keys => deleteHosts(keys)" delete-confirm-message="Delete the selected hosts?">
        <template #detail="{ tab, isNew, record, isDupe }">
            <HostEditor :record="record" :is-new="isNew" :is-dupe="isDupe" @saved="(key, host) => crud?.applySaved(key, host, tab.id)" />
        </template>
    </CrudPanel>
</template>
```

## Record editing

`useRecordEditor<T>(options: RecordEditorOptions<T>): RecordEditor<T>` implements the convention `CrudPanel` and `patchRow` expect:

1. The editor receives the full record (from a table row, a fetch, etc.).
2. It keeps `data` (the state the backend has acknowledged) and `workData` (a reactive working copy bound to inputs).
3. On save only the fields that differ between the two are sent.
4. `save(patch)` resolves to the full updated record, which overwrites both objects — server-side changes (timestamps, derived fields) are acknowledged and the next diff is correct.
5. The editor emits `('saved', key, record)`; the host wires it to `CrudPanel.applySaved`.

```ts
interface RecordEditorOptions<T> {
    initial: T // deep-cloned into data and workData
    save: (patch: Partial<T>) => Promise<T>
    fields?: readonly (keyof T)[] // editable/diffed keys; default: the keys of `initial`
    normalise?: (record: T) => T // canonicalise initial and every response (e.g. undefined vs null)
}
interface RecordEditor<T> {
    data: T
    workData: T
    changes: ComputedRef<Partial<T> | null>
    dirty: ComputedRef<boolean>
    saving: Ref<boolean>
    submit: () => Promise<T | null> // null when nothing changed
    reset: () => void // workData ← data
    applyResponse: (updated: T) => void // acknowledge a server update outside submit()
}
```

Equality is by canonical JSON (strings, numbers, booleans, arrays, plain objects). Validation, HTTP and multi-step flows stay with the caller: gate `submit()` behind checks, or build around `changes` / `applyResponse`.

```vue
<script setup lang="ts">
import { useRecordEditor } from '@greendrake/ui-data'
import { Checkbox, Field, TextField, AsyncAction } from '@greendrake/ui'

type Host = { name: string; address: string; enabled: boolean }

const props = defineProps<{ record: Host }>()
const emit = defineEmits<{ saved: [key: string, record: Host] }>()

// updateHost is the app's own call, resolving to the full updated Host
const { workData, dirty, submit } = useRecordEditor<Host>({
    initial: props.record,
    fields: ['address', 'enabled'],
    save: patch => updateHost(props.record.name, patch)
})
const save = async (): Promise<void> => {
    const updated = await submit()
    if (updated) emit('saved', updated.name, updated)
}
</script>
<template>
    <Field label="Address">
        <TextField v-model="workData.address" />
    </Field>
    <Checkbox v-model="workData.enabled" label="Enabled" />
    <AsyncAction :promise-maker="save" :is-dirty="dirty" is-valid label-dirty="Save" label-done="Saved" />
</template>
```

## Admin-CRUD kit

### useSortState

`useSortState(initialBy: string, initialDir: SortDir = 'desc', defaultDir: SortDir | ((field: string) => SortDir) = 'asc'): SortState`

```ts
interface SortState {
    sortBy: Ref<string>
    sortDir: Ref<SortDir> // 'asc' | 'desc'
    toggleSort: (field: string) => void // same field flips; a new field takes over with defaultDir
    sortIcon: (field: string) => string | undefined // '▲' / '▼' for the active field
    sort: ComputedRef<Sort> // what a table's or panel's `sort` takes
    column: <T>(field: string, label: string, options?: ColumnOptions<T>) => ColumnDefinition<T>
}
```

`column` builds a `ColumnDefinition` whose header click toggles the sort and whose icon reflects it. `ColumnOptions<T>` is `ColumnDefinition<T>` minus `field`/`label`/`sortable`/`sortIcon`/`onHeaderClick`, plus `sortKey?: string | false` — the field sent to the backend when it differs from `field`, or `false` for an unsortable column. Build the columns array inside a `computed` so icons track state.

### localFilterDebounce

`localFilterDebounce(table: Ref<LocalFilterable | undefined>): (value: string | undefined) => number` — a `SearchField` `debounce` resolver: 250ms when the table reports it can serve the change from memory (`willFilterLocally`), 600ms otherwise (and before the table mounts). `LocalFilterable` is the `{ willFilterLocally }` slice of `InfiniteScrollTableExposed`.

### useDetailLoader

`useDetailLoader<T, Id>(id: WatchSource<Id>, fetcher: (id: Id) => Promise<T>): DetailLoader<T>` — the detail-pane fetch skeleton: loads immediately, reloads whenever `id` changes, discards responses that arrive after the id moved on. Returns `{ data: Ref<T | null>, loading, error: Ref<string | null>, reload(), mutate(fn: (cur: T) => void) }`. A rejection's message lands in `error` for in-pane display, so pass a fetcher that does not report errors globally itself. `mutate` folds a known local change into the loaded value without a re-fetch.

### DetailPane

`<DetailPane :loader="detail" v-slot="{ data }">` — a detail pane's record as `detail` (a `useDetailLoader` result) loads: the loading line, the load's error, or the default slot given the loaded record as `data`.

### Formatters

Beside `fmtBool` and `fmtYesNo`, which read any value as a boolean, the formatters render `null`, `undefined` and `''` as `''`, and throw on a value of a type they do not format. `Intl` output is in the browser's locale. They are also `@greendrake/ui-data/formatters`, which imports no component, for code a test runner loads without a Vue compiler.

- `parseInstant(v: unknown): Date` — an instant from an ISO/`'YYYY-MM-DD HH:MM:SS'` string (a zoneless string is taken as UTC), a number or a `Date`.
- `fmtDate(v: unknown): string` — `'YYYY-MM-DD HH:MM:SSZ'`: the instant (`parseInstant`) in UTC.
- `fmtDateTime(style: DateStyle)` — a formatter of instants (`parseInstant`) at an `Intl` date and time style (`'full' | 'long' | 'medium' | 'short'`), in the browser's time zone; one formatter per style.
- `fmtDay(style: DateStyle)` — a formatter of calendar dates (`'YYYY-MM-DD'`) at an `Intl` date style; the day never shifts with the time zone. One formatter per style.
- `fmtNumber(v: unknown): string` — a number or numeric string in the locale's notation, never rounded; a string formats exactly, digit for digit.
- `fmtBytes(v: unknown): string` — a byte count in binary units (B, KiB, MiB, GiB, TiB, PiB, EiB): the largest unit the amount reads under 1024 in once rounded to a tenth, at most one fraction digit.
- `fmtMarkupText(v: unknown): string` — markup as the one line of text it reads: `inlineMarkup`'s text, whitespace collapsed.
- `fmtBool(v: unknown): string` — `'✓'` or `''`, for table cells.
- `fmtYesNo(v: unknown): string` — `uiDataMessages.yes` / `uiDataMessages.no`, for detail prose: "yes" / "no" until `configureUiDataMessages({ yes, no })` says otherwise. `@greendrake/ui`'s `configurableMessages` keeps them (`UiDataMessages`), so an app with i18n calls it at boot and again on a locale change.

### inlineMarkup, InlineMarkup

`inlineMarkup(markup: string): DocumentFragment` rebuilds markup as one line of inline content, safe to render whatever its source: text, and the inline formatting elements `b`, `strong`, `i`, `em`, `u`, `s`, `del`, `ins`, `sub`, `sup`, `code`, `mark`, `small`, recreated without attributes. Other elements are unwrapped; block-level ones and `<br>` part runs of text with one space; scripts, styles, templates, `noscript`, embedded and media content are dropped with everything inside, as are comments. It parses with `DOMParser`, whose document is inert. The `InlineMarkup` component (prop `markup: string`) renders the fragment on one line, cut with an ellipsis — a table cell's `component` for rich text.

### NoteActions

An operator's decisions on a record, recorded with a note. Props: `actions: NoteAction[]` and `placeholder: string`. `NoteAction` is `{ label, fn(note) => Promise, note?: 'optional' | 'required', confirm? }`: a button per action; the note field shows when any action takes a note, one requiring it stays disabled until there is one, and one with `confirm` asks in a modal first. The actions share one in-flight guard and an error line; `done` is emitted once one succeeded, so the parent re-reads.

### DescriptionList

Label/value grid for detail panes. Prop `items: (DescriptionItem | false | null | undefined)[]` — falsy entries are skipped, so conditional rows inline as `cond && { … }`. `DescriptionItem` is `{ label: string; value: string | number | null | undefined; code?: boolean }`; `code` renders the value in `<code>`. Values arrive display-ready.
