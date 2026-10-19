import type { Component, Ref } from 'vue'

export interface ColumnDefinition<T = Record<string, unknown>> {
    field: string
    label: string
    className?: string
    cellClass?: (value: unknown, record: T) => string | undefined
    sortable?: boolean
    sortType?: 'string' | 'number' | 'date'
    sortIcon?: string
    onHeaderClick?: () => void
    onCellClick?: (value: unknown, record: T) => void
    formatter?: (value: unknown, record: T) => string
    wrap?: boolean
    component?: Component
    componentProps?: (record: T) => Record<string, unknown>
}

export type SortDir = 'asc' | 'desc'

// The order a table asks its rows in: a field, and which way.
export interface Sort {
    by: string
    dir: SortDir
}

// What a table holds its rows to beyond the search text and the order: each
// filter by name, an empty one (null, undefined, '') meaning none.
export type Filters = Record<string, string | null | undefined>

// One window of rows a table asks its loader for, in no backend's terms: what
// the loader's own API calls these is the loader's to map.
export interface LoaderParams {
    offset: number
    limit: number
    // The search text, when there is any.
    text?: string
    // The order, when the table was given one.
    sort?: Sort
    // The filters that are not empty.
    filters: Record<string, string>
}

export interface LoaderResponse<T> {
    data: T[]
    total: number
}

// `background` is true when the load was not asked for by the person looking
// at the table — a live binding re-reading after a server push or a dropped
// connection. What that means for the request is the caller's: typically a
// quiet call that shows no spinner and raises no error prompt.
export type LoaderFunction<T> = (params: LoaderParams, loading: Ref<boolean>, background: boolean) => Promise<LoaderResponse<T>>

// A row's identity: either the name of the field holding it, or a function
// deriving it (for composite keys — e.g. an email+name pair joined into one
// string — where no single field is unique).
export type RowKey<T = Record<string, unknown>> = string | ((row: T) => string | number)

export const rowKeyOf = <T>(row: T, key: RowKey<T>): string | number => (typeof key === 'function' ? key(row) : ((row as Record<string, unknown>)[key] as string | number))

export interface Props<T = Record<string, unknown>> {
    loader: LoaderFunction<T>
    columns: ColumnDefinition<T>[]
    rowHeight?: number
    visibleHeight?: number | null
    filters?: Filters
    // The search text, empty meaning none: served from memory, when the whole
    // dataset is there, as long as it narrows the text the dataset was
    // fetched under.
    text?: string
    // The order: served from memory when the whole dataset is there.
    sort?: Sort
    // Fields the local text filter matches against when the whole dataset is
    // in memory. `false` opts out of local filter routing entirely — every
    // text or order change goes to the server (for search semantics the client
    // cannot mirror, e.g. full-text over fields absent from the rows).
    localSearchFields?: string[] | false
    rowKey?: RowKey<T>
    selectable?: boolean
    rowDraggable?: boolean
    hideHeader?: boolean
    onRowClick?: (record: T) => void
    /** Rows fetched per remote request — the offset/limit window held in memory. */
    bufferSize?: number
    /**
     * Testability affordance: render every buffered row instead of only the
     * viewport window, so E2E locators can reach rows below the fold without a
     * tall viewport. Only meaningful for datasets that fit one buffer
     * (`total <= bufferSize`); not for production use.
     */
    renderAll?: boolean
}

export interface DropTarget<T = Record<string, unknown>> {
    record: T
    position: 'before' | 'after'
}

export type RowPosition = 'start' | 'end' | { after: string | number } | { before: string | number }

// The 1-based fully-visible row window, as reported by the `range-changed`
// emit and CrudPanel's range readout / `range` expose.
export interface VisibleRange {
    start: number
    end: number
    total: number
}

// The imperative row-surgery surface InfiniteScrollTable exposes. CrudPanel
// consumes it internally; consumers type their template refs with it to keep
// row types flowing (`ref<InfiniteScrollTableExposed<Host>>()`).
export interface InfiniteScrollTableExposed<T = Record<string, unknown>> {
    refresh: (background?: boolean) => Promise<void>
    patchRow: (keyValue: string | number, data: Partial<T>) => boolean
    addRow: (data: T, position?: RowPosition) => void
    removeRow: (keyValue: string | number) => void
    removeRows: (keyValues: (string | number)[]) => void
    resort: () => void
    getRow: (keyValue: string | number) => T | undefined
    willFilterLocally: (value: string | undefined) => boolean
    selected: Set<string | number> | undefined
}
