import { readFileSync } from 'node:fs'
import path from 'node:path'
import vue from '@vitejs/plugin-vue'
import { mergeConfig, type ConfigEnv, type UserConfig, type UserConfigFnObject } from 'vite'

export { envBool, deriveImageBaseUrl, loadAppEnv, requirePort, apiTargetEnv, buildHash } from './env'
// Re-exported so config factories building on vueSpa (e.g.
// @greendrake/dash/vite) speak this package's vite types: a second vite
// copy resolved downstream is a structurally unrelated set of the same names.
export type { ConfigEnv, UserConfig, UserConfigFnObject } from 'vite'
export { htmlPlaceholders, htmlMinify } from './html'
export { resolveApiTarget, browserRuntime, type ApiTarget, type ApiTargetEnv, type ApiTargetRuntime } from './apiTarget'
export { staticRoots, staticRootFiles, type StaticRoot } from './staticRoots'
import { requirePort } from './env'
import { htmlPlaceholders, htmlMinify } from './html'
import { hmrCircularImportGuard } from './hmr'
import { nestedDependencies, type DeclaredDependencies } from './sourceDeps'
import { staticRoots as staticRootsPlugin, type StaticRoot } from './staticRoots'

export interface VueSpaOptions {
    // The app root (import.meta.dirname of the vite config) — anchors the @ alias.
    dirname: string
    // Compile-time globals; values are serialized automatically (strings become
    // string literals, booleans become true/false, objects become object
    // literals — e.g. the ApiTargetEnv carried by __API_TARGET_ENV__).
    define?: Record<string, string | number | boolean | object | null | undefined> | undefined
    // Extra scss @use resolution roots (e.g. the app's src/scss).
    scssLoadPaths?: string[]
    // 'env' (default): the strict VITE_PORT convention, required when the dev
    // server starts. A number: that port verbatim, for callers that resolved
    // the app's env themselves (see @greendrake/dash/vite). false: no
    // port pinning — for simple apps served on any free port.
    port?: 'env' | number | false
    // Build-time placeholder substitution in index.html (see the
    // htmlPlaceholders plugin) — e.g. injecting an inline bootstrap script.
    htmlPlaceholders?: Record<string, string>
    // Minify index.html (incl. inline JS/CSS) in production builds.
    htmlMinify?: boolean
    // Directories outside the app served at a public URL prefix — product
    // assets both stacks own (locale catalogues, content prose). Served by
    // middleware in dev and copied into dist/ at build, so the two modes
    // publish the same bytes.
    staticRoots?: StaticRoot[]
    // Dependencies that ship TypeScript and SFC source rather than a build, and
    // so must not be pre-bundled (see optimizeDeps below). An entry ending in
    // `/` matches every package under that scope; anything else matches a
    // package by exact name. Defaults to the @greendrake scope, which is
    // source-shipped by construction — an app with source packages of its own
    // names them here, and must name the @greendrake scope again if it uses
    // any.
    sourcePackages?: string[] | undefined
    // Deep-merged last over the preset — the escape hatch for anything
    // app-specific (server.proxy, extra plugins, build tweaks).
    overrides?: UserConfig | undefined
}

// The libraries the app and every component package it renders must share one
// instance of.
const FRAMEWORK = ['vue', 'pinia', 'vue-router']

// Source-shipped by construction, so an app that names none of its own still
// gets the rule that matters for the packages this preset ships beside.
const SOURCE_PACKAGES = ['@greendrake/']

// The app's own package.json dependencies, split the way it splits them.
const declaredDependencies = (dirname: string): DeclaredDependencies => {
    const pkg = JSON.parse(readFileSync(path.join(dirname, 'package.json'), 'utf-8')) as { dependencies?: Record<string, string>; devDependencies?: Record<string, string> }
    return { runtime: Object.keys(pkg.dependencies ?? {}), dev: Object.keys(pkg.devDependencies ?? {}) }
}

// The Vite config a Vue 3 single-page app would otherwise assemble for itself:
// the vue plugin, the strict VITE_PORT pin, workspace fs.allow, the @ alias,
// the dedupe list (each entry must also be a direct dependency of the app, or
// Vite fails to resolve it), the pre-bundling rules a source-shipped package
// needs, and unhashed font asset names so index.html can prefetch them by a
// stable name.
//
// Returns the function form of a Vite config so the VITE_PORT requirement
// applies only to `vite dev`: a production build runs in deploy contexts that
// define no dev port.
export const vueSpa =
    (options: VueSpaOptions): UserConfigFnObject =>
    ({ command }: ConfigEnv): UserConfig => {
        const deps = declaredDependencies(options.dirname)
        const allDeps = [...deps.runtime, ...deps.dev]
        const sourcePackages = options.sourcePackages ?? SOURCE_PACKAGES
        const isSourcePackage = (name: string): boolean => sourcePackages.some(pattern => (pattern.endsWith('/') ? name.startsWith(pattern) : name === pattern))
        // Only the dev server and preview pin a port, so an app that supplies
        // none (and never serves) must not be made to produce one.
        const portPin = command === 'serve' && options.port !== false ? { port: typeof options.port === 'number' ? options.port : requirePort(), strictPort: true } : undefined
        const base: UserConfig = {
            ...(options.define && { define: Object.fromEntries(Object.entries(options.define).map(([key, value]) => [key, JSON.stringify(value)])) }),
            plugins: [vue(), hmrCircularImportGuard(), ...(options.staticRoots?.length ? [staticRootsPlugin(options.staticRoots)] : []), ...(options.htmlPlaceholders ? [htmlPlaceholders(options.htmlPlaceholders)] : []), ...(options.htmlMinify ? [htmlMinify()] : [])],
            ...(command === 'serve'
                ? {
                      server: {
                          ...portPin,
                          // '..' reaches the workspace an app sits directly
                          // inside. Anything the dev server must read from
                          // further afield — a static root beside the backend,
                          // package source a level or two above an app nested
                          // deeper — needs its own entry (server.fs.allow in
                          // the app's overrides), or dev requests for it are
                          // refused.
                          fs: { allow: ['..', ...(options.staticRoots ?? []).map(root => root.dir)] }
                      }
                  }
                : {}),
            // vite preview also loads with command 'serve'; pin it to the same
            // port convention so E2E runs are deterministic.
            ...(portPin && { preview: portPin }),
            resolve: {
                alias: { '@': path.resolve(options.dirname, './src') },
                dedupe: FRAMEWORK
            },
            // A source-shipped package (see sourcePackages) is TypeScript
            // and SFC rather than a build. Pre-bundling one from node_modules
            // splits it in two: esbuild inlines the
            // TypeScript modules — and whatever they import — into the
            // pre-bundle, but externalises the .vue files, which then load the
            // originals and import their own copies of everything (two Pinia
            // instances, one of which createPinia never registered). Served raw
            // instead, a source package resolves to one copy of itself, and the
            // framework libraries it shares with the app are pinned into the
            // pre-bundle so raw and app-side imports meet on the same optimised
            // module. Workspace-linked packages are never pre-bundled, so inside
            // a monorepo the exclusion decides no pre-bundling either way — but
            // it does hide the package from the dependency scanner, which is why
            // the third-party packages it imports are named too (see
            // nestedDependencies).
            optimizeDeps: {
                exclude: allDeps.filter(isSourcePackage),
                include: [...new Set([...FRAMEWORK.filter(name => allDeps.includes(name)), ...nestedDependencies(options.dirname, deps, isSourcePackage)])]
            },
            ...(options.scssLoadPaths && {
                css: {
                    preprocessorOptions: {
                        scss: { loadPaths: options.scssLoadPaths }
                    }
                }
            }),
            build: {
                rolldownOptions: {
                    output: {
                        assetFileNames: assetInfo => {
                            if (assetInfo.names?.[0]?.match(/\.(woff2?|eot|ttf|otf)$/)) {
                                return 'assets/fonts/[name][extname]'
                            }
                            return 'assets/[name]-[hash][extname]'
                        }
                    }
                }
            }
        }
        return options.overrides ? mergeConfig(base, options.overrides) : base
    }
