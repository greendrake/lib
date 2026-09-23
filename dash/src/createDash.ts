// The declaration has to reach every program that compiles this file — this
// package's own check, a sibling package, an app, an outside consumer — and a
// path reference is what travels with the source. The rule's advice does not
// apply: an `import` cannot carry an ambient wildcard module declaration.
// eslint-disable-next-line @typescript-eslint/triple-slash-reference
/// <reference path="./styles.d.ts" />
import './style.scss'
import type { RouteRecordRaw } from 'vue-router'
import { createSpaApp, type SpaApp } from '@greendrake/vue-app'
import DashRoot from './DashRoot.vue'

export interface DashConfig {
    /** Splash lines, shown until the app's init work and first paint are done. */
    splash: string[]
    routes: RouteRecordRaw[]
    /** Set when a route component has async setup — see DashRoot's prop. */
    suspense?: boolean
    /** Init work the boot splash must outlast. */
    ready?: () => Promise<unknown>[]
}

// Boots a dashboard SPA: the shared splash root over the host's routes. The
// wire (transport, auth, method map) stays with the host — a dashboard's API
// surface is the one thing no shell can own.
export const createDash = (config: DashConfig): SpaApp => {
    const app = createSpaApp({
        root: DashRoot,
        rootProps: { splash: config.splash, suspense: config.suspense },
        routes: config.routes,
        ready: config.ready
    })
    app.run()
    return app
}
