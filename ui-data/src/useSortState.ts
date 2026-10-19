import { ref, computed, type Ref, type ComputedRef } from 'vue'
import type { ColumnDefinition, Sort, SortDir } from './InfiniteScrollTable.types'

export type ColumnOptions<T = Record<string, unknown>> = Omit<ColumnDefinition<T>, 'field' | 'label' | 'sortable' | 'sortIcon' | 'onHeaderClick'> & {
    /**
     * Sort field sent to the backend when it differs from `field`
     * (e.g. field 'display_name' sorting by 'name'); `false` makes the
     * column unsortable. Defaults to `field`.
     */
    sortKey?: string | false
}

export interface SortState {
    sortBy: Ref<string>
    sortDir: Ref<SortDir>
    /** Same field flips direction; a new field takes over with its default direction. */
    toggleSort: (field: string) => void
    sortIcon: (field: string) => string | undefined
    /** The order as a table's `sort` takes it, and its loader is handed. */
    sort: ComputedRef<Sort>
    /**
     * ColumnDefinition factory wired to this sort state (header click toggles,
     * icon reflects). Build the columns array inside a `computed` so the icons
     * track state changes.
     */
    column: <T = Record<string, unknown>>(field: string, label: string, options?: ColumnOptions<T>) => ColumnDefinition<T>
}

export const useSortState = (initialBy: string, initialDir: SortDir = 'desc', defaultDir: SortDir | ((field: string) => SortDir) = 'asc'): SortState => {
    const sortBy = ref(initialBy)
    const sortDir = ref<SortDir>(initialDir)

    const toggleSort = (field: string): void => {
        if (sortBy.value === field) {
            sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
        } else {
            sortBy.value = field
            sortDir.value = typeof defaultDir === 'function' ? defaultDir(field) : defaultDir
        }
    }

    const sortIcon = (field: string): string | undefined => (sortBy.value === field ? (sortDir.value === 'asc' ? '▲' : '▼') : undefined)

    const sort = computed(() => ({ by: sortBy.value, dir: sortDir.value }))

    const column = <T = Record<string, unknown>>(field: string, label: string, options: ColumnOptions<T> = {}): ColumnDefinition<T> => {
        const { sortKey = field, ...rest } = options
        return {
            field,
            label,
            sortable: sortKey !== false,
            ...(sortKey === false ? {} : { onHeaderClick: () => toggleSort(sortKey), sortIcon: sortIcon(sortKey) }),
            ...rest
        }
    }

    return {
        sortBy,
        sortDir,
        toggleSort,
        sortIcon,
        sort,
        column
    }
}
