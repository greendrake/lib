import { createApp, ref, watch, watchEffect, type App as VueApp, type Component, type Plugin } from 'vue'
import { createPinia, type PiniaPlugin } from 'pinia'
import { createRouter, createWebHistory, type NavigationGuardReturn, type RouteLocationNormalized, type RouteRecordRaw, type Router } from 'vue-router'
import { getReadyStatePromise } from '@greendrake/util/browser'
import { useAppState } from '@greendrake/vue-kit'
import { preload, takePrimed } from './prefetch'

// Not Parameters<VueApp['use']> — that resolves to the two-argument overload
// and would make bare [plugin] entries a type error.
type PluginArgs = [plugin: Plugin, options?: unknown]

export interface SpaAppOptions {
    root: Component
    rootProps?: Record<string, unknown>
    routes: RouteRecordRaw[]
    // Vue plugins installed before mount, each entry the argument tuple for
    // app.use — [plugin] or [plugin, options]. Must install here (not after
    // run()) because components in the first-rendered tree may invoke a
    // plugin's composables during initial setup.
    plugins?: PluginArgs[]
    // Pinia plugins installed on the store instance before any store is used —
    // e.g. per-user cache invalidation, registering every store that defines a
    // reset() action for a session swap. Applied before mount so every store
    // instantiated later is covered.
    piniaPlugins?: PiniaPlugin[]
    // Joined with a route's own title as "<route title> | <default>". A
    // function is resolved inside the title effect, so a default that is a
    // function of reactive state (a translated title following the UI locale)
    // keeps up with it.
    defaultTitle?: string | (() => string)
    // Maintain a `route-<name>` class on <body> for per-route styling.
    routeClasses?: boolean
    // Runs before every navigation, ahead of the route's data fetch. A returned
    // redirect/abort (string | RouteLocation | false) short-circuits
    // navigation; void/undefined/true proceed. Lets a host gate routes (e.g.
    // a login gate) before the matched component resolves.
    beforeRoute?: (to: RouteLocationNormalized, from: RouteLocationNormalized) => Promise<NavigationGuardReturn> | NavigationGuardReturn
    // App init work the boot splash must outlast (config fetch, locale load…).
    ready?: () => Promise<unknown>[]
    // Runs once the router exists, before anything mounts — for a layer that
    // navigates on its own account, such as an error UX's "go home".
    onRouter?: (router: Router) => void
    // Where a failed boot is reported: init work or document readiness
    // rejecting. The splash lifts either way. Absent, the failure is rethrown
    // as an unhandled rejection, which is where an app with no error UX of its
    // own has it logged.
    onBootError?: (error: Error) => void
}

export interface SpaApp {
    vue: VueApp
    router: Router
    run(): void
    // Settles when the app's init work (options.ready) and document readiness
    // are done — deliberately EXCLUDING router readiness, so navigation guards
    // (which run inside the first navigation) can await it without deadlock.
    initDone: Promise<void>
    // Settles when the boot splash stops covering the app: init work, the first
    // paint, and any loading slot still held past it (a lazy route's chunk, its
    // data). Later than initDone, and the moment a native shell hides its own
    // launch splash on — keyed on the same flag so the two never both show.
    splashDone: Promise<void>
    // Overrides the route-derived title for the current page: "<title> |
    // <defaultTitle>", or just the default when title is omitted. Cleared on
    // the next navigation. For a title that IS a function of route data, use
    // the route's meta.getTitle instead — that one keeps up with live updates.
    setPageTitle(title?: string): void
}

const KNOWN_OPTIONS = new Set<keyof SpaAppOptions>(['root', 'rootProps', 'routes', 'plugins', 'piniaPlugins', 'defaultTitle', 'routeClasses', 'beforeRoute', 'ready', 'onRouter', 'onBootError'])

// Route-level code splitting puts a network fetch between a navigation and the
// component that serves it: vue-router awaits the `() => import(…)` loader once
// the guards have passed, with nothing on screen saying why. So the shell holds
// the global loading slot across that import — the same slot the boot sequence,
// a route's meta.fetch and every API call hold, so it drives the same spinner
// and adds no second notion of "busy". A chunk already in memory settles in a
// microtask, which raises and releases the slot without ever painting.
//
// Wrapped here rather than at each `() => import(…)` so a lazy route cannot be
// added without it. The one shape this rules out is a bare functional component
// passed as `component` — vue-router would take the wrapper for a loader — so
// such a component must be wrapped in defineComponent, as every SFC already is.
const withLoadingSlot = (loader: () => Promise<unknown>) => (): Promise<unknown> => {
    const appState = useAppState()
    appState.setLoading(true)
    return loader().finally(() => appState.setLoading(false))
}

const wrapLazyComponents = (routes: RouteRecordRaw[]): RouteRecordRaw[] =>
    routes.map(route => {
        const wrapped = { ...route } as RouteRecordRaw & { component?: unknown; children?: RouteRecordRaw[] }
        if (typeof wrapped.component === 'function') {
            wrapped.component = withLoadingSlot(wrapped.component as () => Promise<unknown>)
        }
        if (wrapped.children) {
            wrapped.children = wrapLazyComponents(wrapped.children)
        }
        return wrapped as RouteRecordRaw
    })

// Renders committed; children have run setup and registered their own loading.
// The second frame guarantees the first paint happened, so releasing the boot
// slot cannot flash the splash off before anything is visible.
const afterFirstPaint = (): Promise<void> =>
    new Promise(resolve =>
        requestAnimationFrame(() => {
            requestAnimationFrame(() => resolve())
        })
    )

export const createSpaApp = (options: SpaAppOptions): SpaApp => {
    for (const key of Object.keys(options)) {
        if (!KNOWN_OPTIONS.has(key as keyof SpaAppOptions)) {
            throw new Error(`createSpaApp: unknown option '${key}'`)
        }
    }

    const vue = createApp(options.root, options.rootProps)
    const pinia = createPinia()
    options.piniaPlugins?.forEach(plugin => pinia.use(plugin))
    vue.use(pinia)

    const router = createRouter({
        history: createWebHistory(),
        // Where every navigation leaves the viewport, in priority order. The
        // router owns this rather than an afterEach hook: it runs after the
        // incoming page has rendered (so a restored offset is not clamped
        // against the outgoing page's height), and its saved positions are
        // held per history ENTRY — a path-keyed map cannot tell /x from
        // /x#section, and one would overwrite the other's position.
        scrollBehavior(to, _from, savedPosition) {
            // Back/forward returns the reader to where they left that entry.
            if (savedPosition) return savedPosition
            // An in-page target is where the URL points.
            if (to.hash) return { el: to.hash }
            // Everything else is a page arrived at afresh — read from the top,
            // never mid-way down at the offset the previous one was left at.
            return { top: 0 }
        },
        routes: wrapLazyComponents(options.routes)
    })

    options.onRouter?.(router)

    const appState = useAppState()
    // The boot slot: held from construction, released after init work and the
    // first paint. Keeps isFirstLoading semantics (splash vs spinner) exact.
    appState.setLoading(true)

    // The flag itself, not the release below: another slot may still be held
    // when the boot one goes (a lazy route's chunk, its data), and the splash
    // stays up for those too.
    // once: the flag's sole transition is the true→false flip watched for —
    // setLoading only ever clears it.
    const splashDone = new Promise<void>(resolve => {
        watch(
            () => appState.isFirstLoading,
            () => resolve(),
            { once: true }
        )
    })

    // Splash/spinner body classes. index.html ships <body class="splash"> so
    // the pre-JS splash is visible before this watcher takes over.
    watch(
        () => appState.isLoading,
        loading => {
            const classes = loading ? [appState.isFirstLoading ? 'splash' : 'spinner'] : ['splash', 'spinner']
            document.body.classList[loading ? 'add' : 'remove'](...classes)
        }
    )

    router.beforeEach(async (to, from) => {
        if (options.beforeRoute) {
            const result = await options.beforeRoute(to, from)
            if (result !== undefined && result !== true) {
                return result
            }
        }
        // Route-data prefetch (see prefetch.ts): for a route carrying
        // meta.fetch, await the data and stash it in preload[routeName] for
        // the component to read synchronously; getTitle sets meta.title ahead
        // of the title join below. If the upcoming navigation was set up via
        // prefetch(), consume that one-off primed result instead of fetching
        // again. A failed fetch aborts the navigation silently: the api
        // client's error toast already owns the failure's UX (retry re-runs
        // the fetch; acknowledge navigates home), so the router must treat it
        // as a settled decision, not an error to re-raise.
        const { fetch } = to.meta
        if (fetch) {
            try {
                const primed = takePrimed(to.fullPath)
                preload[to.name as string] = primed.hit ? primed.data : await fetch(to)
            } catch {
                return false
            }
        }
    })

    router.afterEach((to, from, failure) => {
        if (failure) {
            return
        }
        // A committed navigation is the app's "something usable is on screen"
        // signal — what the offline surfaces choose between the splash and a
        // banner on. See appState for why isFirstLoading cannot serve.
        appState.hasNavigated = true
        if (!options.routeClasses) {
            return
        }
        if (from.name) {
            document.body.classList.remove(`route-${String(from.name)}`)
        }
        if (to.name) {
            document.body.classList.add(`route-${String(to.name)}`)
        }
    })

    options.plugins?.forEach(([plugin, pluginOptions]) => (pluginOptions === undefined ? vue.use(plugin) : vue.use(plugin, pluginOptions)))

    let settleInitDone!: () => void
    const initDone = new Promise<void>(resolve => {
        settleInitDone = resolve
    })

    // The document title is DERIVED, not stamped once per navigation. A route's
    // meta.getTitle runs inside the effect below, over the route's preload slot
    // — the same reactive slot the page renders from and a WS push replaces — so
    // an update that re-renders a heading retitles the tab with it, with no
    // per-page wiring and nothing to forget at a new push site. Any other
    // reactive state getTitle reads is tracked the same way.
    //
    // A title that is not a function of route data (one read off the rendered
    // <h1>, say) comes in through setPageTitle instead; it overrides the
    // derivation until the next navigation clears it.
    const titleOverride = ref<string>()
    const applyTitle = (title?: string): void => {
        const fallback = typeof options.defaultTitle === 'function' ? options.defaultTitle() : options.defaultTitle
        if (title && title !== fallback) {
            document.title = fallback ? `${title} | ${fallback}` : title
        } else if (fallback) {
            document.title = fallback
        }
    }
    watch(
        () => router.currentRoute.value.fullPath,
        () => (titleOverride.value = undefined)
    )
    watchEffect(() => {
        if (titleOverride.value !== undefined) {
            applyTitle(titleOverride.value)
            return
        }
        const { name, meta } = router.currentRoute.value
        const derived = meta.getTitle ? meta.getTitle(typeof name === 'string' ? preload[name] : undefined) : meta.title
        applyTitle(derived === undefined || derived === null ? undefined : String(derived))
    })

    const setPageTitle = (title?: string): void => {
        titleOverride.value = title ?? ''
    }

    const run = (): void => {
        vue.use(router)
        vue.mount(document.body)
        Promise.all([...(options.ready?.() ?? []), getReadyStatePromise()]).then(settleInitDone, (e: unknown) => {
            // A failed boot must still settle: everything downstream of
            // initDone (the splash release below, content guards awaiting it)
            // would otherwise wait forever, leaving the failure invisible
            // behind a splash that never lifts. Settle first, then hand the
            // error to whatever shows it.
            settleInitDone()
            if (!options.onBootError) {
                throw e
            }
            options.onBootError(e as Error)
        })
        // The boot slot deliberately does NOT await router.isReady(): the first
        // navigation may never commit (a login gate parking it behind the auth
        // modal, an external-redirect route aborting it) and the splash must
        // not outlive interactive UI. Route-data fetches hold the loading
        // counter themselves (an api call without a pending ref drives
        // appState), so the splash still covers a slow first route's data.
        initDone.then(afterFirstPaint).then(() => {
            appState.setLoading(false)
        })
    }

    return {
        vue,
        router,
        run,
        initDone,
        splashDone,
        setPageTitle
    }
}
