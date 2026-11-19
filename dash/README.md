# @greendrake/dash

The shell of an operations dashboard, the internal page a team runs a backend from. One call boots the app behind a splash screen, a list of tabs lays out the panels, and a helper writes the Vite config. The panels and the API are the dashboard's own.

```sh
bun add @greendrake/dash vue vue-router pinia
```

## Why

A dashboard of this kind is a few panels: tables, forms, switches. Before its first panel, every such dashboard needs the same things: a Vue app with a router and a store, a splash screen until the app is ready, a layout that holds the panels, a look, a Vite config, and a rule for where build settings come from.

A team with several backends has several dashboards, and so has that work several times. The obvious way to get it is to copy the last dashboard. The copies then diverge, and a fix made in one does not reach the others. This package holds that part once, and leaves the panels and the API to each dashboard.

## Trade-offs

- **It decides the stack.** A dashboard built on it is a Vue 3 app with `vue-router` and Pinia, built by Vite with a Sass compiler. A dashboard on another framework or bundler cannot use it.
- **It brings its look.** `createDash` imports the style entry: `@greendrake/theme` with its dashboard preset, and Inter as the document font. There is no booting without them. A dashboard with a design of its own is better assembled from `@greendrake/vue-app` directly.
- **No API and no sign-in.** Apart from the service-state control, the package makes no API call. It has no authentication and no login screen. Transport, authentication and the list of methods are different for every backend, so they stay with the host: the dashboard app built on this package. The host also decides who may open the dashboard.

## Overview

- **Boot.** `createDash` assembles the app, the router and the store, and shows a splash screen until init work and the first paint are done. It sets the document title and sets up the error handling of API calls.
- **Layout.** `TabDashboard` renders the panels from a list of tabs: one entry per tab.
- **Look.** The style entry brings a dashboard theme and its font.
- **Build.** `dashApp` is the Vite config, and settles where build settings come from.
- **A first panel.** `ServiceStateControl` shows one backend service live, with the command that starts or stops it.

## Setup

`vue`, `vue-router` and `pinia` are peer dependencies. The app also needs `vite`, which is a peer of `@greendrake/vite-preset`, and a Sass compiler, as dev dependencies. The package ships TypeScript, SFC and SCSS source with no build, and `dashApp` is the Vite config that compiles it.

## Booting

```ts
import { createDash, TabDashboard, type DashTab } from '@greendrake/dash'

const tabs: DashTab[] = [
    { id: 'users', label: 'Users', component: UsersPanel },
    { id: 'logs', label: 'Logs', component: LogsPanel, props: { api } }
]

createDash({
    splash: ['ACME', 'OPS'],
    defaultTitle: 'Acme Ops',
    routes: [{ path: '/', name: 'Dashboard', component: TabDashboard, props: { tabs } }]
})
```

`createDash` returns the `SpaApp` (see `@greendrake/vue-app`), so the host can act on it after boot: open a WebSocket early, say. `defaultTitle` is the document title. A route's own title, which `@greendrake/vue-app` takes from `meta.getTitle`, is joined to it as `<route title> | <defaultTitle>`. Pass `suspense: true` when a route component has async setup. The splash then serves as the Suspense boundary's initial content, so there is no blank frame while the route resolves. `ready` gives the init work that the splash has to wait for. `@greendrake/vue-api`'s error handling is set up (`apiUXHooks`).

`TabDashboard` renders one panel at a time under a `TabBar`. The list of tabs is the single source for both the tab strip and the panels: adding a tab is one entry. The open tab is the first one until the user picks another. It is kept in component state: a tab has no URL of its own, and a reload opens the first tab again. A host binds `v-model:active` to the open tab's id when its panels open one another, or when it keeps the open tab in the route itself. A panel is unmounted when another tab opens, unless its entry sets `keepAlive`. Then the panel is kept as it was, with its state, and shown again without being mounted anew. Where a dashboard needs its routes code-split, keep the list of tabs inside the lazily-imported route module, so that a lightweight route does not pull in the dashboard's panels.

Dashboard-wide styling is in `@greendrake/theme/dashboard`: the denser `--font-md`, headings as section dividers, the styling of the app shell. This package's style entry imports it, together with `@greendrake/font-inter` as the document font. For styling, the host adds nothing beyond `<body class="splash">` in its `index.html`.

## The service-state control

`ServiceStateControl` is a panel for one backend service, which is `OFF`, `STARTING`, `ON`, `STOPPING` or `ERROR`. It works only against a backend that serves the methods and pushes of `@greendrake/service-state`. It watches over a WebSocket, a `WsTransport` from `@greendrake/rpc`: there is no polling. It shows the state, why the service failed if it did, and the one command that state accepts:

```ts
// api.ts
import { keepServiceStates } from '@greendrake/dash/service-state'

export const transport = new WsTransport<Record<never, never>, ServiceStatePushes>({ pipe })
export const api = new ApiClient<ServiceStateMethods>({ transport })
export const services = keepServiceStates(transport)

// main.ts
import { ServiceStateControl } from '@greendrake/dash/service-state'

const tabs: DashTab[] = [
    { id: 'web', label: 'Web', component: ServiceStateControl, props: { service: 'web', api, services } },
    { id: 'worker', label: 'Worker', component: ServiceStateControl, props: { service: 'worker', api, services } }
]
```

`keepServiceStates(transport)` makes the keeper. The keeper watches the backend's services over `transport`. For each service it watches, it keeps the service's document: the status of the service as the backend pushes it (`@greendrake/vue-api`'s `keepTopicDocuments`).

- `watch(service)` watches a service until the function it returns is called. Given a ref or getter, it watches whichever service that names at the time.
- `status(service)` is the service's status. It is undefined while the backend has nothing to send. The backend has something to send when its reading of the service is younger than its `maxAgeMs`, or when a transition it started is under way or has failed.
- `failure(service)` is the error with which watching the service was refused.

A service is read only while something watches it. A dashboard that shows one service of many has that one read, and none while none is shown. The keeper can be made at any time, and needs no connect call: the first watch opens the socket. When a dropped socket reconnects, every watched service is watched again. The keeper watches by its own calls over `transport`, whichever client the controls are given for commands.

`ServiceStateControl` watches its `service` while it is shown. That is while it is mounted, except while it is on a `keepAlive` tab that is not the open one. When the prop changes, it watches the new service. Its props:

- `service` is the name under which the backend registered the service's state machine.
- `api` is an `ApiCaller<ServiceStateMethods>` for the commands (`@greendrake/vue-api`'s `ApiClient` is one). Its calls may go over the socket or over HTTP.
- `services` is the keeper.

Any number of controls share one `api` and one keeper. The contracts come from [`@greendrake/service-state`](../service-state), whose `./server` entry is the state machine that a backend runs to serve them. That package is an optional peer dependency: a dashboard that imports this entry installs it, and one that does not never needs it.

The control shows its own loading indicator until the backend has read the service. If watching the service was refused, it shows why instead. Once the service is read, the control shows the state from the service's document, so a transition that another operator started shows here too. A command's response carries no data. The state that the service moved to arrives in the document, and the button stays disabled until it does.

The command each state offers is `@greendrake/service-state`'s `CONTROLS`. `OFF` offers _Turn ON_, `ON` offers _Turn OFF_ and `ERROR` offers _Refresh_. `STARTING` and `STOPPING` offer nothing: a transition that is under way cannot be interrupted. The labels are `serviceStateMessages` (`start`, `stop`, `refresh`), keyed by the command. `configureServiceStateMessages` overrides them. Both come from this entry. The current state is on the root element as `data-state`. The border colour follows it, and an end-to-end test can assert on it. `examples/service-pair` is a complete dashboard and backend built on it.

## Vite and env

```ts
import { dashApp } from '@greendrake/dash/vite'

export default defineConfig(
    dashApp({
        dirname: import.meta.dirname,
        define: env => ({ __API_URL__: requireEnv(env, 'API_URL') }),
        overrides: (env, { command }) => (command === 'serve' ? { server: { proxy: /* … */ } } : {})
    })
)
```

`sourcePackages` is passed on to `@greendrake/vite-preset`. It is for a dashboard that uses packages shipped as source beyond the `@greendrake` scope. `define`, `htmlData` (runtime settings written into `index.html`, see `@greendrake/vite-preset`) and `overrides` receive the app's resolved env. `requireEnv` fails the build when a build input is missing, rather than falling back to a default. The same three also receive Vite's config env, which has the command and the mode. A `define` value can therefore depend on them, and an input that only the dev server needs is required only when serving. `VITE_PORT` is required when serving.

**The env file names matter.** bun loads `.env.development` / `.env.production` into `process.env` at startup, and a `process.env` value takes precedence over the file that Vite's loader reads. With those names, a production build silently gets development credentials. Dashboards therefore use `.env.dev` / `.env.prod`, names that bun ignores, and pass the mode explicitly:

```json
"dev": "bun --bun vite --mode dev --host",
"build": "bun --bun vite build --mode prod",
"preview": "bun --bun vite preview --mode dev"
```

Omitting `--mode` fails the run (there is no `.env.development` to find) rather than producing a mis-configured bundle. The short mode name costs nothing: Vite derives `isProduction` from `NODE_ENV`, which it sets per command. Preview serves an already-built `dist/` and only needs a port, hence `--mode dev`.

`process.env` still takes precedence over the files. That is what lets a script that starts the dev stack, or a deploy pipeline, pass its own ports, hosts and keys to an otherwise unchanged app. E2E runs are one case: the test harness supplies the test backend's ports.
