import { computed, type ComputedRef, type Ref } from 'vue'
import type { Filters, LoaderFunction, LoaderParams, LoaderResponse, SortDir } from '@greendrake/ui-data'

// The arguments of every paged list method the Go rpckit's PageArgs parses:
// the search text as q, the window, and the order as sort_by / sort_dir. A
// method honouring only some of them ignores the rest.
export type PageArgs = {
    q?: string
    offset?: number
    limit?: number
    sort_by?: string
    sort_dir?: SortDir
}

// A table's LoaderFunction over a paged list call: its params in the list
// arguments' terms — the order only when the table has one. A method's filters
// of its own ride the `fetch` closure, and so does what a background read
// costs, the flag handed on verbatim:
//     listLoader((args, background) => listOrders({ ...args, status }, background))
// The table's loading ref is not touched: the table shows its spinner itself
// while the viewport overlaps rows not yet loaded.
export const listLoader =
    <T>(fetch: (args: PageArgs, background: boolean) => Promise<LoaderResponse<T>>) =>
    (params: LoaderParams, _loading: Ref<boolean>, background: boolean): Promise<LoaderResponse<T>> =>
        fetch(
            {
                offset: params.offset,
                limit: params.limit,
                ...(params.text ? { q: params.text } : {}),
                ...(params.sort ? { sort_by: params.sort.by, sort_dir: params.sort.dir } : {})
            },
            background
        )

// A paged list call narrowed by arguments of the method's own, as a table
// takes it: the filters, which are those arguments — so a change of any
// re-fetches — and the loader asking the call with them as they then stand.
// `narrowing` reads them, undefined for one not given:
//     const { filters, loader } = narrowedList(() => ({ status: status.value || undefined }), (args, background) => listOrders(args, background))
export const narrowedList = <N extends Filters, T>(narrowing: () => N, fetch: (args: PageArgs & N, background: boolean) => Promise<LoaderResponse<T>>): { filters: ComputedRef<N>; loader: LoaderFunction<T> } => ({
    filters: computed(narrowing),
    loader: listLoader((args, background) => fetch({ ...args, ...narrowing() }, background))
})
