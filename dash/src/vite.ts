import { vueSpa, loadAppEnv, type ConfigEnv, type UserConfig, type UserConfigFnObject } from '@greendrake/vite-preset'

export interface DashAppOptions {
    // The app root (import.meta.dirname of the vite config): anchors both the
    // @ alias and the .env.<mode> lookup.
    dirname: string
    // Compile-time globals, built from the app's resolved env.
    define?: (env: Record<string, string>) => Record<string, string | number | boolean | object | null | undefined>
    // Dependencies that ship source rather than a build — see
    // @greendrake/vite-preset, whose default covers the @greendrake scope. A
    // dashboard with source packages of its own names them here.
    sourcePackages?: string[]
    // Deep-merged last over the preset. Takes the resolved env (so an app can
    // wire e.g. a dev proxy target from its env file) and the config env, so a
    // dev-only input is only demanded when there is a dev server to demand it
    // for.
    overrides?: (env: Record<string, string>, configEnv: ConfigEnv) => UserConfig
}

/** A build input the app cannot run without: absent is a failed build, not a default. */
export const requireEnv = (env: Record<string, string>, key: string): string => {
    const value = env[key]
    if (!value) {
        throw new Error(`${key} must be set in the app's .env file (or the process env of whatever launched this build)`)
    }
    return value
}

// The Vite config every dashboard shares.
//
// Instances run vite with an explicit `--mode dev` / `--mode prod` and name
// their env files to match. The short names are load-bearing: bun injects
// .env.development / .env.production into process.env at startup, and a
// process.env value outranks the file in loadAppEnv — so with the long names a
// production build silently bakes in the development API URL and credentials.
// Names bun ignores keep the two loaders from ever reading different files, and
// an omitted --mode fails loudly (there is no .env.development to find). The
// mode name does not weaken the build: vite derives isProduction from NODE_ENV,
// which it sets per command.
//
// process.env still wins over the file, which is what lets the dev-stack
// launchers and deploy pipelines (mcp/*, up.py, build-artifacts.sh) inject
// their own ports, hosts and keys into an otherwise unchanged app.
export const dashApp =
    (options: DashAppOptions): UserConfigFnObject =>
    (configEnv: ConfigEnv): UserConfig => {
        const env = loadAppEnv(configEnv.mode, options.dirname)
        return vueSpa({
            dirname: options.dirname,
            define: options.define?.(env),
            sourcePackages: options.sourcePackages,
            // Only the dev server and preview pin a port; builds run without one.
            port: configEnv.command === 'serve' ? parseInt(requireEnv(env, 'VITE_PORT'), 10) : false,
            overrides: options.overrides?.(env, configEnv)
        })(configEnv)
    }
