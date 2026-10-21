# @greendrake/dash

Everything an operations dashboard needs before it has any panels of its own: app bootstrap, the splash root layout, a tab-registry dashboard, the theme entry and the Vite/env conventions. What it deliberately does not own is the wire — transport, auth and method map stay with the host, because a dashboard's API surface is the one thing no shell can generalise.

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

`createDash` returns the `SpaApp` (see `@greendrake/vue-app`), so a host can act on it after boot — open a WebSocket eagerly, say. `defaultTitle` is the document title, and what each route's own title — `meta.getTitle`, `@greendrake/vue-app`'s — is joined to as `<route title> | <defaultTitle>`. Pass `suspense: true` when a route component has async setup — the splash then serves as the Suspense boundary's initial content, so there is no blank frame while the route resolves. `ready` takes init work the splash must outlast. `@greendrake/vue-api`'s error UX is wired in (`apiUXHooks`).

`TabDashboard` renders one panel at a time under a `TabBar`. The registry is the single source for the strip and the panels both: adding a tab is one row. The open tab is the first until the user picks another; a host whose panels open one another binds `v-model:active` to the open tab's id. A panel is unmounted when another tab opens, unless its row sets `keepAlive`: then it is kept as it was, state and all, and shown again without mounting anew. Where a dashboard needs its routes code-split, keep the registry inside the lazily-imported route module, so a lightweight route does not pull in the dashboard panels.

Dashboard-wide styling — the denser `--font-md`, headings as section dividers, the shell chrome — lives in `@greendrake/theme/dashboard`, which this package's style entry pulls in together with `@greendrake/font-inter` as the document font. The consuming app adds nothing for styling beyond `index.html`'s `<body class="splash">`.

## The service-state control

`ServiceStateControl` is a panel for one backend service that is `OFF`, `STARTING`, `ON`, `STOPPING` or `ERROR` — the state, why it failed if it did, and the one command that state accepts:

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

`keepServiceStates(transport)` is the keeper: it watches the backend's services over `transport` and keeps the document of each one watched (`@greendrake/vue-api`'s `keepTopicDocuments`).
- `watch(service)` watches a service until the release it returns is called. Given a ref or getter, it follows it.
- `status(service)` is the service's status, undefined while the backend has neither a reading younger than its `maxAgeMs` nor a transition of its own, under way or failed, to send.
- `failure(service)` is why watching it was refused.

A service is read only while something watches it, so a dashboard showing one service of many has that one read, and none while none is shown. The keeper can be made at any time, and needs no connect: the first watch opens the socket, and after a dropped socket returns every watched service is watched again. Watching goes over `transport`, by the keeper's own calls, whatever client the controls are given for commands.

`ServiceStateControl` watches its `service` while it is shown — mounted, and not while a `keepAlive` tab it is on is switched away from — following the prop as it changes. `service` is the name the backend registered the service's machine under; `api` is an `ApiCaller<ServiceStateMethods>` (`@greendrake/vue-api`'s `ApiClient` is one) for the commands, its calls over the socket or HTTP alike; and `services` is the keeper. Any number of controls share one of each. The contracts come from [`@greendrake/service-state`](../service-state), whose `./server` entry is the machine a backend runs behind them. That package is an optional peer dependency: a dashboard importing this entry installs it, and one that does not never needs it. The control shows its own loading indicator until the backend has read the service — or why watching it was refused — then the service's state as its document has it, so a transition another operator started shows here too. A command's answer is nothing: where the service went comes in the document, and the button stays disabled until it has.

The command each state offers is `@greendrake/service-state`'s `CONTROLS`: `OFF` offers *Turn ON*, `ON` *Turn OFF*, `ERROR` *Refresh*; `STARTING` and `STOPPING` offer nothing, there being no interfering with a transition already under way. The labels are `serviceStateMessages` (`start`, `stop`, `refresh`), keyed by the command and overridable with `configureServiceStateMessages`, both from this entry. The current state is on the root element as `data-state`, which is what the border colour is drawn from and what an end-to-end test asserts on. `examples/service-pair` is a complete dashboard and backend built on it.

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

`sourcePackages` passes through to `@greendrake/vite-preset` for a dashboard that pulls in source-shipped packages beyond the `@greendrake` scope. `define`, `htmlData` (runtime settings written into `index.html`, see `@greendrake/vite-preset`) and `overrides` receive the app's resolved env; `requireEnv` turns a missing build input into a failed build rather than a default. `htmlData` and `overrides` also receive the config env, so a dev-only input is demanded only when there is a dev server to demand it for. `VITE_PORT` is required when serving.

**The env file names are load-bearing.** bun injects `.env.development` / `.env.production` into `process.env` at startup, and a `process.env` value outranks the file Vite's loader reads — with those names a production build silently bakes in development credentials. Dashboards therefore use `.env.dev` / `.env.prod`, names bun ignores, and pass the mode explicitly:

```json
"dev": "bun --bun vite --mode dev --host",
"build": "bun --bun vite build --mode prod",
"preview": "bun --bun vite preview --mode dev"
```

Omitting `--mode` fails the run (there is no `.env.development` to find) rather than producing a mis-configured bundle. The short mode name costs nothing: Vite derives `isProduction` from `NODE_ENV`, which it sets per command. Preview serves an already-built `dist/` and only needs a port, hence `--mode dev`.

`process.env` still outranking the files is what lets dev-stack launchers and deploy pipelines inject their own ports, hosts and keys into an otherwise unchanged app — including e2e runs, where the harness supplies the test backend's ports.
