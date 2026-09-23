import type { Ref } from 'vue'
import type { LoaderParams, LoaderResponse } from './InfiniteScrollTable.types'
import type { SortState } from './useSortState'

// The standard admin list-method argument shape (q, limit, offset, sort_by,
// sort_dir) — the wire counterpart of LoaderParams + SortState.
export type PageArgs = {
    q?: string
    offset?: number
    limit?: number
    sort_by?: string
    sort_dir?: string
}

// Adapts InfiniteScrollTable's LoaderParams to the admin list-method argument
// shape. Passing `sort` includes the search box's `q` and the sort filters —
// exactly the methods with server-side search/sort support; the rest get the
// bare offset/limit window. Extra filter fields ride the `fetch` closure, and
// so does what a background read costs — the flag is handed on verbatim:
//     listLoader((args, background) => api.call('admin.thread.list', [{ ...args, type }], backgroundCall(background)))
export const listLoader =
    <T>(fetch: (args: PageArgs, background: boolean) => Promise<LoaderResponse<T>>, sort?: SortState) =>
    (params: LoaderParams, _loading: Ref<boolean>, background: boolean): Promise<LoaderResponse<T>> =>
        fetch(
            {
                offset: params.offset,
                limit: params.limit,
                ...(sort ? { q: typeof params.q === 'string' ? params.q : undefined, ...sort.sortFilters.value } : {})
            },
            background
        )
