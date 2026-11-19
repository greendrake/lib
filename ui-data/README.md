# @greendrake/ui-data

Vue 3 components for a dashboard that lists records and edits them: a table over a list of any length, a list-and-detail panel around it, and a way to edit a record that saves only what changed. Built on `@greendrake/ui`.

```sh
bun add @greendrake/ui-data vue
```

## Why

An admin dashboard shows the same screen many times over: a list of records, the picked record beside it, and search, sort, create and delete around them. Its obvious implementation has three costs. Rendering every row slows the page when the list is long, and page numbers make the user click through pages to reach a row. Fetching the list again after each save costs a round trip. Sending the whole form back overwrites fields the user never touched, whatever happened to them on the server meanwhile.

This package is that screen, written once. Its table keeps only the rows on screen in the DOM, a saved record updates its one row, and a save sends only the fields that changed.

## Trade-offs

- **The table does its own layout and scrolling.** Every row has the same height, `rowHeight`, and a row cannot grow with its content. Scrolling is the table's own, not the browser's. Rows that are off screen are not in the DOM, so the browser's find-in-page does not find them.
- **Paging is by offset, with a total.** The loader is asked for an `offset` and a `limit`, and has to answer with the `total`. There is no cursor paging. A backend that cannot count a list, or seek into it by offset, cannot be used with the table.
- **The backend has to take a patch.** `useRecordEditor` needs a `save` that takes a partial record and answers with the full one. It sends no version of the record, so it cannot tell that someone else changed the same field meanwhile. The backend has to make that check.
- **For a wide screen.** `MasterDetail` and `CrudPanel` put the list and the open detail side by side, however narrow the viewport. There is no single-column layout for a phone.

## Overview

- **A table for a list of any length.** `InfiniteScrollTable` keeps only the rows on screen in the DOM, and fetches rows in batches as the user scrolls. When one response holds the whole list, the table sorts it in memory. It narrows a search there too, when told which fields the search matches.
- **No backend built in.** The table asks a `loader` function for a batch of rows. The loader maps the request to what its API calls the offset, the search text and the order.
- **One record changes one row.** A change to one record is applied to its row with `patchRow`, `addRow` or `removeRows`. The list is not fetched again.
- **A save sends what changed.** `useRecordEditor` sends only the fields that differ. The server answers with the full record, which becomes the editor's new baseline.
- **The screen assembled.** `CrudPanel` puts a search field, Create and Delete, the table and a tab per opened record together.

Around these are the small parts of a detail pane: a loader, formatters for dates, numbers and money, a label/value list.

## Setup

`vue` is the peer dependency. `@greendrake/ui`, `@greendrake/scss-kit` and `@greendrake/util` come in as dependencies.

There is no build step and no `.d.ts`: `exports` points at `src/main.ts`, and the consuming app's Vite and `vue-tsc` compile the SFCs. The SFCs and their generics (`InfiniteScrollTable<T>`, `CrudPanel<T>`) are written against the `vueCompilerOptions` of `@greendrake/dev-config/tsconfig/vue.json`. A consumer's tsconfig extends it:

```json
{
    "extends": "@greendrake/dev-config/tsconfig/vue.json",
    "include": ["src"]
}
```

Like `@greendrake/ui`, the package is for the browser only: it is not written for server-side rendering.

## Styling contract

The contract is that of `@greendrake/ui`. The app defines the custom properties which that package's README lists as read with no fallback; this package reads a subset of them. The table and the panel also read `--cell-padding`, `--list-row-padding`, `--row-border-bottom`, `--row-select-color` and `--row-select-color-hover`, each with a fallback.

Layout: `InfiniteScrollTable` measures its own container to decide how many rows to render, so its height must come from the layout above it, not from its content. Place it in a flex column whose items have `min-height: 0`, or pass `visibleHeight`. A height driven by content makes the measurement loop until every row is rendered.

## InfiniteScrollTable

Virtualised table. Only the rows that fit the measured viewport are in the DOM, plus two more: one for a row partly visible at the top, and one for a row partly visible at the bottom. Scrolling moves by pixels, not by whole rows. It is the table's own: a custom scrollbar, the wheel, touch panning with inertia, and the keyboard when `selectable`. The loader supplies the rows, in batches of `bufferSize` around the viewport. When a single response holds the whole dataset (`data.length >= total`), the table keeps it in memory and does two things there, without further calls. One is sorting, with an unset value first. The other is a text filter that narrows the current one, matched over the fields the table is given (`localSearchFields`). Columns are resizable by dragging header edges.

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

The params follow no particular backend's naming. The loader maps them to whatever its API calls the text, the order and the filters.

`loading` is the table's spinner flag, given to the loader to set around its call. The spinner also shows while the viewport overlaps rows that are not loaded yet, so a loader that never sets `loading` still gets a spinner for those.

`background` is true when the person looking at the table did not ask for the load: the app is re-reading after a server push or a dropped connection, with `refresh(true)`. The loader decides what that means for the request. Typically it is a call that shows no spinner and raises no error prompt.

`T` must satisfy `Record<string, unknown>`, so declare row types as type aliases (`type User = { … }`), not interfaces: an interface carries no implicit index signature and is rejected where the table's generic is inferred.

```ts
type Filters = Record<string, string | null | undefined> // an empty value (null, undefined, '') is no filter
```

A change to `text`, `sort` or `filters` re-fetches from offset 0, with one exception: the whole dataset is in memory, and the filters are the ones it was fetched under. Then a new order is applied in memory. So is a new text, on two conditions. It contains the text the dataset was fetched under, so that it only narrows it. And `localSearchFields` says which fields to match it against. Without `localSearchFields`, every other text goes to the loader: only the loader knows what its search matches.

In memory the table orders by its own rules (strings by `localeCompare`), and matches a search as a case-insensitive substring of the fields it is given. Where the backend orders or searches differently, the two disagree. `localSearchFields: false` sends every change to the loader instead.

Only the latest load supplies rows. The result of a load is dropped when a later load (a new query, a new batch) has started since.

### Props

| Prop                | Type                                                                                                                                                                                                   | Default  |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------- |
| `loader`            | `LoaderFunction<T>`                                                                                                                                                                                    | required |
| `columns`           | `ColumnDefinition<T>[]`                                                                                                                                                                                | required |
| `rowKey`            | `RowKey<T>` — field name, or `(row) => string \| number` for composite keys                                                                                                                            | `'id'`   |
| `rowHeight`         | px                                                                                                                                                                                                     | 45       |
| `visibleHeight`     | px, or `null` to measure the container                                                                                                                                                                 | `null`   |
| `filters`           | `Filters`                                                                                                                                                                                              | `{}`     |
| `text`              | the search text; empty is none                                                                                                                                                                         | —        |
| `sort`              | `Sort`, the order                                                                                                                                                                                      | —        |
| `localSearchFields` | the fields the loader's search matches (a case-insensitive substring), for the in-memory text filter to mirror it; absent, a text change goes to the loader; `false`, every text and order change does | —        |
| `selectable`        | pointer/keyboard row selection (wrapper gets `tabindex="0"`)                                                                                                                                           | `false`  |
| `rowDraggable`      | rows are `draggable`                                                                                                                                                                                   | `false`  |
| `hideHeader`        |                                                                                                                                                                                                        | `false`  |
| `onRowClick`        | `(record: T) => void`                                                                                                                                                                                  | —        |
| `onRowdrop`         | `(event: DragEvent, target: DropTarget<T> \| null) => void` — a drag dropped on the table; bound (`@rowdrop`), the table tracks drop targets                                                           | —        |
| `bufferSize`        | rows fetched per request                                                                                                                                                                               | 1000     |
| `renderAll`         | render every buffered row (for tests, where the dataset fits one buffer)                                                                                                                               | `false`  |

```ts
interface ColumnDefinition<T> {
    field: string // a dotted field reads a path into the row: `original.size`
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

Selection: mouse click selects, Shift extends a range, Ctrl/Cmd toggles; a right-click selects its row alone unless it is selected already; touch taps select; Arrow keys move the cursor (Shift extends), Ctrl/Cmd+A selects all. A removed row leaves the selection. With `rowDraggable`, a plain press on a selected row selects it alone only when the click completes, so that a drag starting from it carries the whole selection.

### Emits

- `range-changed(range: VisibleRange)` — `{ start, end, total }`, the 1-based range of fully visible rows; emitted immediately and on every change.
- `rowactivate(record: T)` — double-click.
- `rowcontextmenu(records: T[], event: MouseEvent)` — right-click, for a context menu to open at the event (`ContextMenu.open`). `records` is what the menu acts on: the selected rows held in memory, in order, when the row is selected, else the row alone.
- `rowdragstart(records: T[], event: DragEvent)` — with `rowDraggable`, a row's drag begun, for the consumer to fill in the drag's `dataTransfer`. `records` are what the drag carries, as for `rowcontextmenu`.
- `rowdrop` — the `onRowdrop` prop above; `DropTarget` is `{ record, position: 'before' | 'after' }`, the hovered row carrying a `drop-before` / `drop-after` class meanwhile.

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

To show a change to a single record, change its row with `patchRow`, `addRow` or `removeRows`; `refresh()` after one is a wasted round trip.

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

Master/detail CRUD screen: a bar of controls (search field where the loader searches, optional Create button, refresh, a display of the visible range, optional Delete button) over an `InfiniteScrollTable` in the master pane, and detail tabs opened by double-clicking a row. A panel without a `detail` slot is table-only: rows can be selected (for deletion) and never open a pane.

| Prop                                            | Type                                                                                                                                                                            | Default           |
| ----------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------- |
| `columns`                                       | `ColumnDefinition<T>[]`                                                                                                                                                         | required          |
| `loader`                                        | `LoaderFunction<T>`                                                                                                                                                             | required          |
| `rowKey`                                        | `RowKey<T>`                                                                                                                                                                     | `'id'`            |
| `rowHeight`                                     | px                                                                                                                                                                              | 28                |
| `filters`                                       | `Filters`, forwarded to the table, which takes its `text` from the search box                                                                                                   | `{}`              |
| `sort`                                          | `Sort`, forwarded to the table                                                                                                                                                  | —                 |
| `initialWidth`, `minWidth`, `fullWidthUntilTab` | forwarded to `MasterDetail`                                                                                                                                                     | 200, 120, `true`  |
| `showRange`                                     | show the visible range as `start-end/total`                                                                                                                                     | `true`            |
| `searchable`                                    | the loader searches; `false` shows no search field, for a list whose API takes no search text                                                                                   | `true`            |
| `localSearchFields`                             | `string[] \| false`, forwarded to the table                                                                                                                                     | —                 |
| `rowLabel`                                      | `(record: T) => string`, a row's detail tab label; `null` labels it with the tab's id                                                                                           | `null`            |
| `rowTab`                                        | `(record: T) => string`, the id of the detail tab a row opens, for rows that open the tab of something they refer to; `null` opens the row's own tab, whose id is the row's key | `null`            |
| `creatable`                                     | Create button, opens a `new:<n>` tab                                                                                                                                            | `false`           |
| `newTabLabel`                                   | label of that tab                                                                                                                                                               | `messages.newTab` |
| `onDelete`                                      | `(keys: Key[]) => Promise<unknown>`; enables the Delete button for the selection                                                                                                | `null`            |
| `deleteConfirmMessage`                          | `string \| ((keys: Key[]) => Promise<string>)`; when set, deletion asks first in a `Modal` with this text, or the text the function composes for the selected keys              | `null`            |
| `keyExists`                                     | `(key: Key) => boolean`, backs the `isDupe` slot prop                                                                                                                           | `null`            |

The search field takes its debounce from `localFilterDebounce`: short when the change is resolved in memory, longer when it triggers a fetch. A panel with a single column hides the table header.

Slots: `controls-left` (after the search field and Create), `controls-middle` (`{ range: VisibleRange }`), `controls-right` (after Delete; `{ selected: Set<Key> | undefined }`), `detail` (`{ tab: TabDef, isNew: boolean, record: T | undefined, isDupe: (original: Key, candidate: Key) => boolean }`). `record` is the row the table holds for the tab's key (`undefined` for a `new:` tab, a row the table has not seen, or with `rowTab`); `isDupe` flags a candidate key that differs from `original` and already exists, for rename and create forms.

By default a row opens its own tab, whose id is the row's key. The tab's `record` is the row for as long as the table holds it. Deleting the row closes the tab, and `applySaved` renames and relabels it. With `rowTab`, a row instead opens the tab of something it refers to, under the id that `rowTab` gives. There is one tab per id, however many rows refer to it: in a list of disputes, each row opens its payment. Such a tab belongs to no row. Its detail is given `tab.id` and no `record`, so the tab stays as it is when a filter or the search removes the rows that opened it. Neither deletion nor `applySaved` changes it.

Exposed (`CrudPanelExposed<T>`; type the ref as `ref<CrudPanelExposed<Row>>()`): `openTab`, `closeTab`, `updateTab`, `removeRows(keys)` (also clears the selection), `addRow`, `patchRow`, `getRow`, `refresh`, `applySaved(key, record, tabId?)`, and readonly `selected`, `range`, `deleting`.

`applySaved` is what a detail calls after a save, under the record-editing convention below. When the key exists, it patches the row in place. Otherwise it adds the row. Given the id of the `new:` tab the record came from, it also renames that tab to the saved record's key. The `record` slot prop then holds the row, and the next save updates instead of creating. With a `rowLabel`, the record's tab is relabelled from the saved record.

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
2. It keeps `data` (the record as the backend last returned it) and `workData` (a reactive working copy bound to inputs).
3. On save only the fields that differ between the two are sent.
4. `save(patch)` resolves to the full updated record, which overwrites both objects. Server-side changes (timestamps, derived fields) are taken in that way, and the next diff is correct.
5. The editor emits `('saved', key, record)`, and its parent passes that to `CrudPanel.applySaved`.

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

Equality is by `JSON.stringify` (strings, numbers, booleans, arrays, plain objects), so two objects with the same keys in a different order count as different. Validation, HTTP and multi-step flows stay with the caller: call `submit()` only once its checks pass, or build around `changes` / `applyResponse`.

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

`column` builds a `ColumnDefinition` whose header click toggles the sort and whose icon reflects it. `ColumnOptions<T>` is `ColumnDefinition<T>` minus `field`/`label`/`sortable`/`sortIcon`/`onHeaderClick`, plus `sortKey?: string | false` — the field sent to the backend when it differs from `field`, or `false` for an unsortable column. Build the columns array inside a `computed` so that the icons follow the sort state.

### localFilterDebounce

`localFilterDebounce(table: Ref<LocalFilterable | undefined>): (value: string | undefined) => number` — a function for `SearchField`'s `debounce` prop: 250ms when the table reports it can apply the change in memory (`willFilterLocally`), 600ms otherwise (and before the table mounts). `LocalFilterable` is the `{ willFilterLocally }` part of `InfiniteScrollTableExposed`.

### useDetailLoader

`useDetailLoader<T, Id>(id: WatchSource<Id>, fetcher: (id: Id) => Promise<T>): DetailLoader<T>` — the fetch logic of a detail pane: loads immediately, reloads whenever `id` changes, discards a response that arrives after the id has changed. Returns `{ data: Ref<T | null>, loading, error: Ref<string | null>, reload(), mutate(fn: (cur: T) => void) }`. A rejection's message is put in `error`, to be shown in the pane, so pass a fetcher that does not report errors globally itself. `mutate` applies a known local change to the loaded value without a re-fetch.

### DetailPane

`<DetailPane :loader="detail" v-slot="{ data }">` shows a detail pane's record as it loads. `detail` is a `useDetailLoader` result. The pane renders the loading line, the load's error, or the default slot, which is given the loaded record as `data`.

### Formatters

`fmtBool` and `fmtYesNo` read any value as a boolean. The other formatters render `null`, `undefined` and `''` as `''`, and throw on a value of a type they do not format. `Intl` output is in the browser's locale. The formatters are also exported from `@greendrake/ui-data/formatters`. That entry imports no component, so a test runner can load it without a Vue compiler.

- `parseInstant(v: unknown): Date` — an instant from an ISO/`'YYYY-MM-DD HH:MM:SS'` string (a zoneless string is taken as UTC), a number or a `Date`.
- `fmtDate(v: unknown): string` — `'YYYY-MM-DD HH:MM:SSZ'`: the instant (`parseInstant`) in UTC.
- `fmtDateTime(style: DateStyle)` — a formatter of instants (`parseInstant`) at an `Intl` date and time style (`'full' | 'long' | 'medium' | 'short'`), in the browser's time zone; one formatter per style.
- `fmtDay(style: DateStyle)` — a formatter of calendar dates (`'YYYY-MM-DD'`) at an `Intl` date style; the day never shifts with the time zone. One formatter per style.
- `fmtNumber(v: unknown): string` — a number or numeric string in the locale's notation, never rounded; a string formats exactly, digit for digit.
- `fmtBytes(v: unknown): string` — a byte count in binary units (B, KiB, MiB, GiB, TiB, PiB, EiB): the smallest unit in which the amount reads under 1024 once rounded to a tenth, at most one fraction digit.
- `fmtDuration(v: unknown): string` — a count of seconds (a number or numeric string) as a clock reads a span: `H:MM:SS`, or `M:SS` under an hour, rounded to the second.
- `fmtMarkupText(v: unknown): string` — markup as one line of plain text: the text of `inlineMarkup`'s result, with whitespace collapsed.
- `fmtBool(v: unknown): string` — `'✓'` or `''`, for table cells.
- `fmtYesNo(v: unknown): string` — `uiDataMessages.yes` / `uiDataMessages.no`, for the text of a detail pane: "yes" / "no" until `configureUiDataMessages({ yes, no })` says otherwise. `@greendrake/ui`'s `configurableMessages` keeps them (`UiDataMessages`), so an app with i18n calls `configureUiDataMessages` at boot and again on a locale change.

### Money

Amounts in the form a payment API sends them: an integer count of a currency's minor units, beside its `Denomination`. A `Denomination` is `{ currency, scale }`: the ISO 4217 code, and the number of decimals of its minor unit. The scale is the one the server uses, never `Intl`'s: `Intl` takes its scales from CLDR, and for some currencies they differ from the server's. Amounts are read and written through decimal strings, never a float. A payment as the API sends it carries `currency` and `scale`, so it serves as its own `Denomination`. These functions come from `@greendrake/ui-data/money`, which imports no component. Each denomination's formatter is made once.

- `fmtMoney(minor: number, of: Denomination): string` — the amount with the currency's symbol and the scale's decimals, in the browser's locale. It throws when `minor` is not a non-negative safe integer.
- `toMinor(amount: string, scale: number): number | null` — a decimal amount as minor units. The amount is what a `CurrencyInput`'s model holds, and the scale is that input's `precision`. The result is null for a string that is not an amount at the scale: it is not a decimal, its fraction needs more decimals than the scale has, or its count of minor units is beyond a safe integer. The form has to reject such a value.
- `toDecimal(minor: number, scale: number): string` — the reverse of `toMinor`: minor units as a decimal string, digit for digit (`1250` at 2 is `'12.50'`). It throws when `minor` is not a non-negative safe integer.

### inlineMarkup, InlineMarkup

`inlineMarkup(markup: string): DocumentFragment` rebuilds markup as one line of inline content, safe to render whatever its source. The result holds text and the inline formatting elements `b`, `strong`, `i`, `em`, `u`, `s`, `del`, `ins`, `sub`, `sup`, `code`, `mark`, `small`, recreated without attributes. Other elements are unwrapped. Block-level elements and `<br>` separate runs of text with one space. Scripts, styles, templates, `noscript`, embedded and media content are dropped with everything inside them, and so are comments. Parsing is by `DOMParser`, whose document is inert. The `InlineMarkup` component (prop `markup: string`) renders the fragment on one line, cut with an ellipsis. It serves as a table cell's `component` for rich text.

### NoteActions

An operator's decisions on a record, recorded with a note. Props: `actions: NoteAction[]`, `placeholder: string` and `choices?: readonly string[]`. `NoteAction` is `{ label, fn(note) => Promise, note?: 'optional' | 'required', confirm? }`. Each action gets a button. The note field shows when any action takes a note. An action that requires a note stays disabled until there is one. An action with `confirm` asks in a modal first. Given `choices`, the note is one of them: a reason code rather than free text, picked from a select whose empty option is `placeholder`. Only one action runs at a time, and the actions share one error line. `done` is emitted once an action has succeeded, so that the parent can read the record again.

### FilterSelect

A filter of a list by one of a fixed set of values, for a `controls-left` slot: `v-model` is the value picked, `''` for any. Props: `options: readonly S[]` (shown as they are), `label` (the accessible name) and `anyLabel` (the empty option's text). Pass the model to the table's `filters` and to the loader's arguments.

### RecordsTable

The records of one kind that belong to a record, such as a payment attempt's refunds or a certificate's access log. They show as a static table, every row at once. With no rows, nothing renders. Props: `rows: T[]`; `columns: RecordColumn<T>[]` (`ColumnDefinition`'s `field`, `label`, `formatter` and `cellClass`); `rowKey: (row: T, index: number) => string` (records with no id of their own, such as a log's, are keyed by their index); and `testid`. Each row carries `testid` as `data-testid`, and its `status`, where it has one, as `data-status`.

### DescriptionList

Label/value grid for detail panes. Prop `items: (DescriptionItem | false | null | undefined)[]` — falsy entries are skipped, so a conditional row is written inline as `cond && { … }`. `DescriptionItem` is `{ label: string; value: string | number | null | undefined; code?: boolean }`; `code` renders the value in `<code>`. Values are shown as given: format them first.

### RecordDetails

A record shown through table columns, as a label/value grid laid out as `DescriptionList`'s. Props: `columns: ColumnDefinition<T>[]` and `record: T`. Each column shows its `label` beside what an `InfiniteScrollTable` cell of it would show of the record: its `component`, bound to `componentProps(record)`, else its value (`valueAt`) through its `formatter` where it has one. A column whose value the record leaves unset (`null`, `undefined` or `''`) is left out. Each value's `<dd>` carries its column's `field` as `data-field`.

## E2E helpers

`@greendrake/ui-data/e2e` finds a table's rows for an app's Playwright suite. `cells(page, text)` is the cells showing `text`. `openRow(page, text)` opens the first row showing `text` in its detail tab. It does so by a double click, which is how the table opens a row.
