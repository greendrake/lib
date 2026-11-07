import { h, ref } from 'vue'
import { defineStore } from 'pinia'
import { NetworkError } from '@greendrake/util'
import { NotFoundError, WebSocketClosedError } from '@greendrake/rpc'
import { showToast } from '@greendrake/ui'
import { useAppState } from '@greendrake/vue-kit'
import ExceptionToast from './ExceptionToast.vue'
import { getErrorSink } from './uxConfig'

// What the toast makes of a failure: a not-found is only acknowledged, the rest
// may be tried again.
export type ExceptionKind = 'network' | 'notfound' | 'other' | false

export const classifyException = (e: Error): Exclude<ExceptionKind, false> => {
    if (e instanceof NotFoundError) return 'notfound'
    if (e instanceof NetworkError) return 'network'
    return 'other'
}

export interface FailOptions {
    // What Try again does for the failure; a promise keeps the toast on
    // "Trying…" until it settles.
    retry?: () => Promise<void> | void
    cancel?: () => void
    // Runs when the user acknowledges a not-found's toast.
    onDismiss?: () => void
    // Attached to the error report (e.g. the failed RPC's method name).
    reportContext?: Record<string, unknown>
}

// A failure the toast stands for: its kind, and what answering the toast does
// for it.
interface Registration extends Pick<FailOptions, 'retry' | 'cancel' | 'onDismiss'> {
    kind: Exclude<ExceptionKind, false>
}

// Global exception surface: one blocking toast at a time, standing for every
// failure registered while it shows, of the latest one's kind. Try again
// retries them all; Cancel — or acknowledging a not-found — cancels them all;
// a failure's raiser withdraws it once it no longer needs the user (its call
// went through, or waits out an outage), and the toast goes with the last.
export const useExceptionState = defineStore('exceptionstate', () => {
    // The kind the toast on screen shows; false for none.
    const exception = ref<ExceptionKind>(false)

    // Wiring, not state: the failures the toast stands for, in the order
    // raised; every error ever registered, live or long answered (raised());
    // and the toast's teardown.
    const registrations: Registration[] = []
    const registered = new WeakSet<Error>()
    let hideToast: (() => void) | undefined

    // Brings the toast in line with the registrations: none without any, else
    // one of the latest's kind.
    const reconcile = (): void => {
        const kind = registrations.at(-1)?.kind ?? false
        if (exception.value === kind) {
            return
        }
        hideToast?.()
        hideToast = undefined
        exception.value = kind
        if (!kind) {
            return
        }
        hideToast = showToast({
            content: h(ExceptionToast, {
                kind,
                // Whether there is a page to stay on. A committed navigation
                // is exactly that: something usable is already on screen, so a
                // not-found has somewhere to leave the visitor and needs offer
                // them nothing. Read here rather than in the toast — pinia is
                // certainly active, and the answer cannot change while the
                // toast blocks the app anyway.
                canStayPut: useAppState().hasNavigated,
                retry: () => {
                    const retried = registrations.map(r => r.retry?.()).filter((r): r is Promise<void> => r instanceof Promise)
                    return retried.length ? Promise.allSettled(retried).then(() => undefined) : undefined
                },
                cancel: () => cancelAll()
            }),
            blocking: true,
            variant: 'warning'
        }).destroy
    }

    // Whether the error has been registered: shown to the user, its raiser
    // holding its answer — so a catch-all it bubbles to leaves it be.
    const raised = (e: Error): boolean => registered.has(e)

    // Registers the failure, answering its withdrawal. Each call registers its
    // own: two raisers failing with one error object — every awaiter of a
    // socket attempt that never opened is rejected with the same — each hold
    // an answer.
    const fail = (e: Error, options: FailOptions = {}): (() => void) => {
        registered.add(e)
        const kind = classifyException(e)
        const { reportContext, ...answers } = options
        // Report genuine unexpected failures. A not-found and ordinary network
        // blips are expected operational conditions and would only be noise —
        // but an *abnormal* WebSocket close is reported so server-side or
        // idle-timeout drops stay diagnosable. Every failure reports, whatever
        // toast is showing.
        const sink = getErrorSink()
        if (sink && (kind === 'other' || (e instanceof WebSocketClosedError && e.isAbnormal))) {
            sink(e, reportContext)
        }
        // Leave a trace whether or not a sink is configured, and in shipped
        // builds as well as dev. This toast is the one surface saying a call
        // failed unexpectedly, and on a device there are no devtools to open —
        // but `logcat` and the Web Inspector both carry the console, so without
        // this line an on-device failure is undiagnosable. `warn` for the
        // reason the RPC layer picks it: the condition is handled and
        // surfaced, and an error would fail the E2E fixtures' teardown guard on
        // specs that provoke this toast deliberately. One preformatted string,
        // not several arguments: the native WebView console bridge does not
        // serialise them, so anything structured reaches `logcat` as a bare
        // "[object Object]".
        if (kind === 'other') {
            const where = reportContext ? ` ${JSON.stringify(reportContext)}` : ''
            console.warn(`[exception]${where} ${e.name}: ${e.message}`)
        }
        const registration: Registration = { kind, ...answers }
        registrations.push(registration)
        reconcile()
        return () => {
            const i = registrations.indexOf(registration)
            if (i >= 0) {
                registrations.splice(i, 1)
                reconcile()
            }
        }
    }

    // Cancels every failure the toast stands for, as its Cancel does.
    const cancelAll = (): void => {
        const answered = registrations.splice(0)
        reconcile()
        answered.forEach(r => {
            r.cancel?.()
            r.onDismiss?.()
        })
    }

    return {
        exception,
        raised,
        fail,
        cancelAll
    }
})
