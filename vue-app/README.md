# @greendrake/vue-app

SPA bootstrap for Vue 3: one `createSpaApp()` call assembles the Vue app, Pinia and the router, holds the boot splash until the app's init work and first paint are done, derives the document title from the route, and fetches a route's data before its component mounts.

## Install

```sh
bun add @greendrake/vue-app vue vue-router pinia
```

Builds on `@greendrake/vue-kit` (the app loading state) and `@greendrake/util`.

## `createSpaApp(options)`

```ts
import { createSpaApp } from '@greendrake/vue-app'
import Root from './Root.vue'

const app = createSpaApp({
    root: Root,
    routes: [
        { path: '/', name: 'home', component: Home },
        { path: '/items/:id', name: 'item', component: () => import('./Item.vue'), meta: { fetch: to => api.call('item.get', to.params.id), getTitle: data => (data as Item).name } }
    ],
    defaultTitle: 'My App',
    routeClasses: true,
    ready: () => [config.load()]
})
app.run()
```

| Option | Effect |
| --- | --- |
| `root`, `rootProps` | The root component and its props. Mounted on `document.body`. |
| `routes` | `RouteRecordRaw[]`. A lazily-imported component (`() => import(…)`) holds the global loading slot while its chunk loads, so a slow chunk shows the same spinner as a data fetch. |
| `plugins` | Vue plugins installed before mount, each entry the argument tuple for `app.use`: `[plugin]` or `[plugin, options]`. |
| `piniaPlugins` | Pinia plugins installed on the store before any store is used. |
| `defaultTitle` | A string, or a function of reactive state (a translated title follows the locale). |
| `routeClasses` | Maintain a `route-<name>` class on `<body>` for per-route styling. |
| `beforeRoute(to, from)` | Runs before every navigation, ahead of the route's data fetch. A returned redirect, `RouteLocation` or `false` short-circuits; `undefined`/`true` proceed. |
| `ready` | `() => Promise[]` — init work the boot splash must outlast (config fetch, locale load). |
| `onRouter(router)` | Runs once the router exists, before mount — for a layer that navigates on its own account, such as an error UX's "go home". |
| `onBootError(error)` | Where a failed boot is reported (see Boot sequence). Without it the failure is rethrown as an unhandled rejection. |

An unknown option is an error.

With `@greendrake/vue-api`, spread its `apiUXHooks` into the options: the not-found toast's "To the home page" then navigates through this router — which also lets stores and other non-component modules navigate home without importing the app module — and a failed boot shows as that package's error toast.

```ts
import { apiUXHooks } from '@greendrake/vue-api'

createSpaApp({ root: Root, routes, ...apiUXHooks })
```

The returned `SpaApp`:

- `vue`, `router` — the instances;
- `run()` — installs the router, mounts, and starts the boot sequence;
- `initDone` — settles when the `ready` promises and document readiness are done. Router readiness is deliberately excluded, so navigation guards can await it without deadlock;
- `splashDone` — settles when the boot splash stops covering the app: init work, the first paint, and any loading slot still held past it (a lazy route's chunk, its data). The moment a native shell hides its own launch screen on;
- `setPageTitle(title?)` — overrides the route-derived title until the next navigation; with no argument, just the default title.

### Boot sequence

The global loading slot (`useAppState` from `@greendrake/vue-kit`) is held from construction. `index.html` ships `<body class="splash">`, so the pre-JS splash is visible before the watcher takes over; while the slot is held the body carries `splash` (first load) or `spinner` (later), and both are removed when it clears. `run()` mounts, awaits `ready()` plus document readiness, waits for the first paint to commit, and releases the slot. A failed `ready` still settles `initDone` — nothing downstream may wait forever behind a splash that never lifts — and hands the error to `onBootError`.

Scroll behaviour: back/forward restores the saved position; a hash scrolls to its element; any other navigation starts at the top.

## Route data

Routes declare their data load on `meta`, typed through a `vue-router` module augmentation:

```ts
declare module 'vue-router' {
    interface RouteMeta {
        fetch?: (to: Pick<RouteLocationNormalized, 'params'>, callConfig?: object) => Promise<unknown>
        getTitle?: (data: unknown) => string
    }
}
```

The navigation guard awaits `meta.fetch(to)` before the matched component mounts and stores the result in `preload[routeName]`, so a component reads its data synchronously — no async `setup()`, no `<Suspense>`:

```ts
import { preload } from '@greendrake/vue-app'

const item = computed(() => preload.item as Item)
```

`preload` is reactive: a param change (`/items/1` → `/items/2`) re-runs the fetch and refreshes the mounted component. A failed fetch aborts the navigation silently — the API client's error toast owns the failure's UX.

`meta.getTitle(data)` derives the document title from the preloaded data, live: it re-runs whenever the slot changes, so a push that replaces the data retitles the tab as it re-renders the page. Titles render as `<route title> | <defaultTitle>`; a route without a title shows the default.

### Prefetching ahead of navigation

```ts
import { prefetch } from '@greendrake/vue-app'

const path = await prefetch(router, { name: 'item', params: { id } }, { pending: false })
await router.push(path)
```

`prefetch(router, target, callConfig?)` runs the target route's `meta.fetch` now and hands the result to the upcoming navigation; the guard consumes it and skips the second fetch. Use it to fold a route's data load into a preceding operation (a save), so the wait happens under that operation's progress UI. The result is keyed by the resolved `fullPath` and consumed exactly once, by that location. `callConfig` is forwarded to the fetch — e.g. `{ pending: false }` to suppress the page spinner when a button already shows progress. A route with no `meta.fetch` primes nothing; the `fullPath` is still returned to push. `takePrimed(fullPath)` is the consuming half, exported for a custom guard.
