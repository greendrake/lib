import { h } from 'vue'
import { defineStore } from 'pinia'
import { NetworkError } from '@greendrake/util'
import { NotFoundError, OneOutcomeError, WebSocketClosedError } from '@greendrake/rpc'
import { showToast } from '@greendrake/ui'
import { useAppState } from '@greendrake/vue-kit'
import ExceptionToast from './ExceptionToast.vue'
import { getErrorSink } from './uxConfig'

export type ExceptionKind = 'network' | 'notfound' | 'oneoutcome' | 'other' | false

export const classifyException = (e: Error): Exclude<ExceptionKind, false> => {
    if (e instanceof NotFoundError) return 'notfound'
    if (e instanceof OneOutcomeError) return 'oneoutcome'
    if (e instanceof NetworkError) return 'network'
    return 'other'
}

interface ExceptionState {
    exception: ExceptionKind
}

export interface FailOptions {
    retry?: () => Promise<void>
    cancel?: () => void
    // Runs when the user acknowledges an acknowledge-only (notfound/
    // oneoutcome) toast — the UI-side successor of the dismiss callback the
    // old transport error type carried.
    onDismiss?: () => void
    // Attached to the error report (e.g. the failed RPC's method name).
    reportContext?: Record<string, unknown>
}

// Non-reactive callback slots: wiring, not state. Refreshed on every fail()
// so a repeated same-kind failure re-drives the existing toast.
const handlers: {
    hideToast?: () => void
    retry?: () => Promise<void>
    cancel?: () => void
    onDismiss?: () => void
} = {}

// Global exception surface: one blocking toast at a time, with Retry/Cancel
// wired back to the failed call's deferred (see ApiClient). fail(false)
// clears it.
export const useExceptionState = defineStore('exceptionstate', {
    state: (): ExceptionState => ({
        exception: false
    }),
    actions: {
        fail(e: Error | false, options?: FailOptions): void {
            if (!e) {
                if (this.exception !== false) {
                    this.exception = false
                    handlers.hideToast?.()
                    delete handlers.hideToast
                }
                return
            }
            const kind = classifyException(e)
            // Report genuine unexpected failures. notfound/oneoutcome and
            // ordinary network blips are expected operational conditions and
            // would only be noise — but an *abnormal* WebSocket close is
            // reported so server-side or idle-timeout drops stay diagnosable.
            // Done before the same-kind dedup below so every distinct report
            // fires even while a toast of the same kind is showing.
            const sink = getErrorSink()
            if (sink && (kind === 'other' || (e instanceof WebSocketClosedError && e.isAbnormal))) {
                sink(e, options?.reportContext)
            }
            // Leave a trace whether or not a sink is configured, and in shipped
            // builds as well as dev. This toast is the one surface saying a call
            // failed unexpectedly, and on a device there are no devtools to open
            // — but `logcat` and the Web Inspector both carry the console, so
            // without this line an on-device failure is undiagnosable. `warn`
            // for the reason the RPC layer picks it: the condition is handled
            // and surfaced, and an error would fail the E2E fixtures' teardown
            // guard on specs that provoke this toast deliberately.
            // One preformatted string, not several arguments: the native
            // WebView console bridge does not serialise them, so anything
            // structured reaches `logcat` as a bare "[object Object]".
            if (kind === 'other') {
                const where = options?.reportContext ? ` ${JSON.stringify(options.reportContext)}` : ''
                console.warn(`[exception]${where} ${e.name}: ${e.message}`)
            }
            handlers.retry = options?.retry
            handlers.cancel = options?.cancel
            handlers.onDismiss = options?.onDismiss
            if (this.exception === kind) {
                return
            }
            handlers.hideToast?.()
            this.exception = kind
            const { destroy } = showToast({
                content: h(ExceptionToast, {
                    error: e,
                    kind,
                    // Whether there is a page to stay on. A committed
                    // navigation is exactly that: something usable is already
                    // on screen, so a not-found has somewhere to leave the
                    // visitor and needs offer them nothing. Read here rather
                    // than in the toast — this is a store action, so pinia is
                    // certainly active, and the answer cannot change while the
                    // toast blocks the app anyway.
                    canStayPut: useAppState().hasNavigated,
                    retry: () => handlers.retry?.(),
                    hide: () => destroy(),
                    cancel: () => {
                        destroy()
                        this.exception = false
                        handlers.cancel?.()
                        handlers.onDismiss?.()
                    }
                }),
                hideOnClick: false,
                variant: 'warning'
            })
            handlers.hideToast = destroy
        }
    }
})
