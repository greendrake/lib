// Single source of truth for where the API lives. Pure and environment-free:
// build-time inputs (env) and runtime inputs (the page's location) are
// explicit arguments, so the same rules serve the Node side (vite configs,
// inline bootstrap generation) and the browser bundle — import via the
// ./api-target subpath there; the package's main entry is Node-only.
//
// It resolves origins only: which paths the API answers on is the app's.
//
// Host resolution:
//   Prod — the same bundle is deployed to two hosts. On the public FE host
//   (e.g. example.com) a CDN may not proxy WS, so the API is on its own host
//   (e.g. api.example.com). On any other host (e.g. an internal staging host) the API is
//   proxied same-origin, so use the page's host. Proto follows the page's
//   (http→ws, https→wss).
//   Dev — the local backend on apiPort over http/ws; unless publicApiHost is
//   set (point the dev server at a remote API, e.g. prod), in which case that
//   host over https/wss.

export interface ApiTargetEnv {
    // True under `vite dev` (command === 'serve'); false in production builds.
    dev: boolean
    // Undefined as readily as absent: each typically comes from an environment
    // variable, which is either set or not.
    apiPort?: string | undefined
    publicFeHost?: string | undefined
    publicApiHost?: string | undefined
}

export interface ApiTargetRuntime {
    // The page's location.host / location.protocol === 'https:'. Only
    // consulted in prod — dev targets are fully determined by env.
    host: string
    https: boolean
}

// The API's origins, without a trailing slash: the app appends its paths.
export interface ApiTarget {
    httpOrigin: string
    wsOrigin: string
}

export const resolveApiTarget = (env: ApiTargetEnv, runtime?: ApiTargetRuntime): ApiTarget => {
    let host: string
    let secure: boolean
    if (env.dev) {
        if (env.publicApiHost) {
            host = env.publicApiHost
            secure = true
        } else {
            if (!env.apiPort) {
                throw new Error('resolveApiTarget: apiPort is required in local dev (no publicApiHost)')
            }
            host = `localhost:${env.apiPort}`
            secure = false
        }
    } else {
        if (!runtime) {
            throw new Error('resolveApiTarget: runtime location is required in prod (the dual-host rule resolves against it)')
        }
        if (env.publicFeHost && runtime.host === env.publicFeHost) {
            if (!env.publicApiHost) {
                throw new Error('resolveApiTarget: publicApiHost is required when serving on publicFeHost')
            }
            host = env.publicApiHost
        } else {
            host = runtime.host
        }
        secure = runtime.https
    }
    return {
        httpOrigin: `${secure ? 'https' : 'http'}://${host}`,
        wsOrigin: `${secure ? 'wss' : 'ws'}://${host}`
    }
}

// browserRuntime executes only in browser bundles, but this package's
// tsconfig is Node-typed (no DOM lib) — declare the two location fields read.
declare const location: { host: string; protocol: string }

// The runtime argument as a browser bundle supplies it. Apps with no special
// runtime (native shells etc.) pass this directly.
export const browserRuntime = (): ApiTargetRuntime => ({
    host: location.host,
    https: location.protocol === 'https:'
})
