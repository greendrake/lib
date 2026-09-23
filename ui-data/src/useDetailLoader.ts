import { ref, watch, toValue, type Ref, type WatchSource } from 'vue'
import { errorMessage } from './errorMessage'

export interface DetailLoader<T> {
    data: Ref<T | null>
    loading: Ref<boolean>
    error: Ref<string | null>
    reload: () => Promise<void>
    // Patch the loaded value in place (a no-op before the first load). Lets a
    // caller fold a known local change — an appended item, a status flip — into
    // the pane without a full re-fetch, while the loader stays the sole writer.
    mutate: (fn: (cur: T) => void) => void
}

/**
 * The detail-pane fetch skeleton: load on mount, re-load whenever the id
 * changes, expose data/loading/error. Error handling stays local — the
 * fetcher's rejection message lands in the `error` ref for in-pane display,
 * never in a global toast — so pass a silent api call as the fetcher.
 * Responses that arrive after the id has moved on are discarded.
 */
export const useDetailLoader = <T, Id>(id: WatchSource<Id>, fetcher: (id: Id) => Promise<T>): DetailLoader<T> => {
    const data = ref(null) as Ref<T | null>
    const loading = ref(false)
    const error = ref<string | null>(null)
    let seq = 0

    const reload = async (): Promise<void> => {
        const token = ++seq
        loading.value = true
        error.value = null
        try {
            const result = await fetcher(toValue(id))
            if (token !== seq) return
            data.value = result
        } catch (e) {
            if (token !== seq) return
            error.value = errorMessage(e)
        } finally {
            if (token === seq) loading.value = false
        }
    }

    const mutate = (fn: (cur: T) => void): void => {
        if (data.value !== null) fn(data.value)
    }

    watch(id, reload, { immediate: true })

    return {
        data,
        loading,
        error,
        reload,
        mutate
    }
}
