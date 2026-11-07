import { useExceptionState } from './exceptionState'
import { configureApiUX } from './uxConfig'

// The error-UX pipeline's half of an app's boot, for a bootstrap that offers
// these hooks. The router is typed by the one method used, so this package
// takes no vue-router dependency.
export const apiUXHooks = {
    // The not-found toast's "To the home page" navigates through the app's
    // router. Wired where the router is created rather than in an app's
    // main.ts, so non-component modules (stores) can navigate home via
    // getGoHome() without importing the app module — an import back-edge that
    // would put main.ts inside a circular import and break HMR.
    onRouter: (router: { push(to: string): unknown }): void => {
        configureApiUX({
            goHome: () => {
                void router.push('/')
            }
        })
    },
    // A failed boot, or anything Vue catches, surfaces through the same toast
    // as any other failure; Try again reloads the app it left broken. An error
    // already raised — a cancelled call's, bubbling up from its caller — was
    // shown already.
    onError: (error: Error): void => {
        const exceptionState = useExceptionState()
        if (!exceptionState.raised(error)) {
            exceptionState.fail(error, { retry: () => location.reload() })
        }
    },
    // A route's data is fetched through the client, whose toast has already
    // shown the failure — Retry re-runs the fetch, acknowledging a not-found
    // goes home — so the navigation is only abandoned, quietly.
    onRouteFetchError: (): false => false
}
