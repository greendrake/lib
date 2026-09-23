import { reactive } from 'vue'
import type { RouteLocationNormalized, RouteLocationRaw, Router } from 'vue-router'

// Route-level data prefetch. A route declares `meta.fetch` to load its data;
// createSpaApp's beforeEach guard awaits it before the matched component
// mounts and stashes the result in preload[routeName], so components read
// their data synchronously — no async `setup()`, hence no `<Suspense>`
// dependency to get wedged when a fetch fails. `getTitle` derives the document
// title from that data — a live derivation, re-run whenever the slot changes
// (createSpaApp), so a WebSocket push that replaces the slot retitles the tab
// as it re-renders the page. Both live on the vue-router RouteMeta via this
// augmentation so routes and the guard stay typed.
//
// `fetch` is typed against just the route params — all it ever needs to build
// its request — so it can be driven not only from the navigation guard (which
// passes the incoming RouteLocationNormalized) but also ahead of navigation
// from a `router.resolve(...)` location (RouteLocationResolved), as
// prefetch() does to prime a route's data before navigating to it. The two
// shapes differ only in `matched`, which fetch doesn't touch.
//
// The optional `callConfig` is an opaque API call config forwarded to the
// data load (routes hand it to the api client). The navigation guard omits
// it, so a normal navigation shows the default page-centered spinner;
// prefetch() forwards the caller's config — e.g. `{ pending: false }` — to
// suppress that spinner when a button label is already communicating
// progress.
declare module 'vue-router' {
    interface RouteMeta {
        fetch?: (to: Pick<RouteLocationNormalized, 'params'>, callConfig?: object) => Promise<unknown>
        getTitle?: (data: unknown) => string
    }
}

// Route-scoped prefetch results, keyed by route name. Reactive so a param
// change (e.g. /L1 → /L2) re-runs meta.fetch and refreshes the
// already-mounted component. Read sites cast the entry to the concrete type
// the route's fetch produced.
export const preload = reactive<Record<string, unknown>>({})

// One-off prefetch handoff. `prefetch()` runs a route's meta.fetch ahead of
// time and stashes the result here; the navigation guard consumes it (via
// takePrimed) on the matching navigation and skips the otherwise-redundant
// fetch. Keyed by the target's fullPath — not route name — so a primed result
// is only ever delivered to the exact location it was loaded for (priming /L1
// never satisfies a navigation to /L2). Entries are removed on consumption:
// this is a deterministic single-use handoff, not a TTL cache.
const primed = new Map<string, unknown>()

// Run `target`'s meta.fetch now and hand its result to the upcoming navigation.
// The caller navigates afterward with the returned fullPath; the guard then
// uses the primed result instead of fetching again. Use this to fold a route's
// data load into a preceding async operation (e.g. a save) so the wait happens
// under that operation's progress UI rather than during the navigation. A route
// with no meta.fetch is a pass-through — nothing is primed, the fullPath is
// still returned to push.
export const prefetch = async (router: Router, target: RouteLocationRaw, callConfig?: object): Promise<string> => {
    const resolved = router.resolve(target)
    if (resolved.meta.fetch) {
        primed.set(resolved.fullPath, await resolved.meta.fetch(resolved, callConfig))
    }
    return resolved.fullPath
}

// Re-run the current route's meta.fetch and hand the result to the component
// already showing it. For what changes the answer without changing the
// question: the language the server writes a response's labels in. Deliberately
// not a forced re-navigation — the route is the same, so re-running the guards,
// pushing a history entry and returning the reader to the top of a page they
// were reading would all be side effects of a data refresh.
//
// A failure leaves the previous data on screen. The api client's error UX owns
// the failure (retry re-runs this), and the page it would otherwise blank is
// the one the reader is looking at — the same stance the navigation guard takes
// when a route's first fetch fails.
export const refetchRoute = async (router: Router): Promise<void> => {
    const route = router.currentRoute.value
    if (!route.meta.fetch) {
        return
    }
    try {
        preload[route.name as string] = await route.meta.fetch(route)
    } catch {
        return
    }
}

// Consume the primed result for `fullPath`, if any. `hit` distinguishes "nothing
// was primed" (fetch normally) from "primed with a nullish value" (use it).
export const takePrimed = (fullPath: string): { hit: boolean; data: unknown } => {
    if (!primed.has(fullPath)) return { hit: false, data: undefined }
    const data = primed.get(fullPath)
    primed.delete(fullPath)
    return { hit: true, data }
}
