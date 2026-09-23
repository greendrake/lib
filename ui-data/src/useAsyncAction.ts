import { ref, type Ref } from 'vue'
import { errorMessage } from './errorMessage'

export interface AsyncAction<Args extends unknown[]> {
    /** True while the action runs; `run` is re-entry-guarded on it. */
    acting: Ref<boolean>
    error: Ref<string | null>
    /** Success notice — the string the action resolved to, if any. */
    notice: Ref<string | null>
    run: (...args: Args) => Promise<void>
}

/**
 * In-flight/error/notice bookkeeping around an async action — the repeated
 * action-button skeleton. `run` ignores re-entry while acting, clears
 * error/notice, and captures a rejection's message locally (no global
 * handling), so pass a silent api call inside `fn`. When `fn` resolves to a
 * string, that string becomes the success notice. Buttons that share one
 * in-flight state parameterise `fn` and pass the variant to `run`.
 */
export const useAsyncAction = <Args extends unknown[]>(fn: (...args: Args) => Promise<string | void>): AsyncAction<Args> => {
    const acting = ref(false)
    const error = ref<string | null>(null)
    const notice = ref<string | null>(null)

    const run = async (...args: Args): Promise<void> => {
        if (acting.value) return
        acting.value = true
        error.value = null
        notice.value = null
        try {
            const result = await fn(...args)
            if (typeof result === 'string') notice.value = result
        } catch (e) {
            error.value = errorMessage(e)
        } finally {
            acting.value = false
        }
    }

    return {
        acting,
        error,
        notice,
        run
    }
}
