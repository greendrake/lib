import { useExceptionState } from './exceptionState'
import { configureApiUX } from './uxConfig'

// The error-UX pipeline's half of an app's boot, for a bootstrap that offers
// these two hooks. The router is typed by the one method used, so this package
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
    // A failed boot surfaces through the same toast as any other failure.
    onBootError: (error: Error): void => {
        useExceptionState().fail(error)
    }
}
