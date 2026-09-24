// Single source of truth for where the API lives. Pure and environment-free:
// build-time inputs (env) and runtime inputs (the page's location) are
// explicit arguments, so the same rules serve the Node side (vite configs,
// inline bootstrap generation) and the browser bundle — import via the
// ./api-target subpath there; the package's main entry is Node-only.
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
    // Undefined as readily as absent: each is an environment variable, which
    // is either set or not.
    apiPort?: string | undefined
    publicFeHost?: string | undefined
    publicApiHost?: string | undefined
    // Endpoint paths appended to the resolved origin (defaults /v1/api and
    // /v1/ws). A backend that routes POST at the host root sets apiPath: ''.
    apiPath?: string
    wsPath?: string
}

export interface ApiTargetRuntime {
    // The page's location.host / location.protocol === 'https:'. Only
    // consulted in prod — dev targets are fully determined by env.
    host: string
    https: boolean
}

export interface ApiTarget {
    apiUrl: string
    wsUrl: string
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
        apiUrl: `${secure ? 'https' : 'http'}://${host}${env.apiPath ?? '/v1/api'}`,
        wsUrl: `${secure ? 'wss' : 'ws'}://${host}${env.wsPath ?? '/v1/ws'}`
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
