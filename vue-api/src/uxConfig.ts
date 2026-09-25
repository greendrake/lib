// App-level wiring for the error-UX pipeline, injected at boot. Inverted
// dependencies: this package never knows where error reports go or how the
// app navigates home.

export type ErrorSink = (error: Error, context?: Record<string, unknown>) => void

// The connectivity oracle useConnectivity reads. Supplied by the app because
// only it knows which transport carries its calls — @greendrake/rpc's
// WsTransport satisfies this shape as-is. An app that wires none falls back to
// navigator.onLine, which is all an HTTP-only client has to go on.
export interface ConnectionSource {
    // Whether calls can currently succeed.
    readonly connected: boolean
    // Fires per underlying transition, NOT deduplicated: repeated `false`
    // reports are consecutive failed attempts, which is what distinguishes a
    // momentary drop from a sustained outage.
    onConnectionChange(listener: (connected: boolean) => void): void
}

interface ApiUXConfig {
    // Receives genuine unexpected failures (and abnormal WebSocket drops) for
    // reporting — e.g. the visit subsystem's error channel.
    errorSink?: ErrorSink
    // Drives the not-found toast's "To the home page" action; typically
    // router.push('/'). apiUXHooks wires it to a @greendrake/vue-app router.
    goHome?: () => void
    // Read once, when useConnectivity is first instantiated.
    connectionSource?: ConnectionSource
}

const config: ApiUXConfig = {}

export const configureApiUX = (overrides: ApiUXConfig): void => {
    Object.assign(config, overrides)
}

export const getErrorSink = (): ErrorSink | undefined => config.errorSink

export const getConnectionSource = (): ConnectionSource | undefined => config.connectionSource

export const getGoHome = (): (() => void) => config.goHome ?? (() => location.assign('/'))
