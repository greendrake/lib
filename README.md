# @greendrake packages

TypeScript packages for building web applications — Vue 3 + Vite frontends, bun backends, and the typed wire between them. Each addresses one recurring problem, and an application takes only the ones it has: an RPC client and server over HTTP and WebSocket with live server push, and a Vue binding for it; an SPA bootstrap and a dashboard shell; a component library with data-heavy widgets; design tokens and SCSS primitives; service scaffolding with health probes and a graceful shutdown; and the Vite, TypeScript, ESLint and Playwright configuration that builds and tests the result.

They are developed against real applications rather than in the abstract, and published for anyone whose application has the problems they solve. This repository is a one-way mirror of their source, synchronised from the monorepo they are developed in; releases go to npm from there, all packages at one version. Issues are welcome here; a pull request is applied upstream and arrives back with the next sync.

## Packages

### Frontend

| Package | What it is |
| --- | --- |
| [`@greendrake/dev-config`](dev-config) | TypeScript, ESLint and Prettier configuration for Vue 3 + Vite projects, as config files to extend or re-export rather than rule sets to copy |
| [`@greendrake/css-reset`](css-reset) | Modern CSS reset (adapted from Josh Comeau's and Andy Bell's resets) |
| [`@greendrake/domains`](domains) | Email and domain-name validation backed by the IANA TLD list |
| [`@greendrake/scss-kit`](scss-kit) | Framework-free SCSS primitives: breakpoints (single-sourced with TS via codegen), z-layer map, icon-mask registry, @font-face generator, misc mixins |
| [`@greendrake/util`](util) | Framework-free, Node-safe utilities: typed event emitter, timing, retry, staleness, comparators, fetch helpers, misc |
| [`@greendrake/ui`](ui) | Vue 3 component library: form controls, modals, search, tabs, toasts, layout |
| [`@greendrake/e2e`](e2e) | Playwright kit for single-page apps: config factories, a same-origin crawler, a clipped-content detector, a polling helper and a layout checker |
| [`@greendrake/rpc`](rpc) | RPC client over JSON: typed method-map API, HTTP and reconnecting WebSocket transports, pluggable codec and headers, TTL response cache |
| [`@greendrake/theme`](theme) | Design tokens, document-level element styling, a light/dark theming mixin and an opt-in dashboard palette |
| [`@greendrake/ui-data`](ui-data) | Data-heavy dashboard components: virtualized InfiniteScrollTable, CrudPanel/MasterDetail, record editing and the admin-CRUD kit |
| [`@greendrake/vite-preset`](vite-preset) | Vite config factory and build-env helpers for Vue 3 single-page apps: plugin set, strict dev port, alias, dedupe and source-package pre-bundling rules |
| [`@greendrake/vue-kit`](vue-kit) | Vue/Pinia building blocks: app loading state, media queries, collection-store factory |
| [`@greendrake/vue-api`](vue-api) | Vue binding of @greendrake/rpc: loading-state wiring, response cache, error-UX pipeline, silent calls, live bindings to server push |
| [`@greendrake/vue-app`](vue-app) | SPA bootstrap: app/pinia/router assembly, splash gate, route titles and body classes |
| [`@greendrake/font-inter`](fonts/inter) | Inter webfont (woff2, OFL): @font-face registration via the @greendrake/scss-kit generator; opt-in document default via the ./default entry |
| [`@greendrake/dash`](dash) | Dashboard SPA shell: app bootstrap, splash root layout, tab-registry dashboard, a live service-state control and the Vite/env conventions a dashboard needs |

### Backend

| Package | What it is |
| --- | --- |
| [`@greendrake/rpc-server`](rpc-server) | RPC server over JSON for bun: method dispatch with auth, nonce and Standard Schema argument validation, HTTP and WebSocket transports, connection registry and server push |
| [`@greendrake/service-state`](service-state) | A backend service as a state machine (OFF/STARTING/ON/STOPPING/ERROR): the wire contract both halves share, and the server-side machine behind it |
| [`@greendrake/server`](server) | Service-binary scaffolding for bun: dependency health probes with /healthz + /readyz, CORS, and the SIGTERM-driven graceful-shutdown sequence |

## Consuming them

Every package ships its TypeScript, Vue SFC and SCSS **source** — there is no build step and no `.d.ts` output. A consuming app compiles them with its own Vite and `vue-tsc`, which asks three things of it:

- **Run under bun**: `bun --bun vite …`, `bun --bun playwright test`, `bun src/main.ts`. `vite.config.ts` imports `@greendrake/dash/vite` or `@greendrake/vite-preset`, `playwright.config.js` imports `@greendrake/e2e`; both are TypeScript under `node_modules`, and Node does not type-strip files there. The backend packages go further and are bun-native: `Bun.serve` owns their sockets. The browser bundle itself has no such requirement — Vite compiles the packages' `.ts`, `.vue` and `.scss` like any other source.
- **Extend the shared tsconfig**: `"extends": "@greendrake/dev-config/tsconfig/vue.json"` for an app, `base.json` with `"types": ["bun"]` for a service, so `vue-tsc` and `tsc` check the source under the compiler options it is written for.
- **Provide the peer dependencies**: `vue@^3.5`, `vue-router@^4.6`, `pinia@^3`, `vite@>=8`, `typescript@^6`, `sass-embedded` and `vue-tsc` for the frontend; `@playwright/test` where `@greendrake/e2e` is used. A backend-only consumer needs none of them.

Styles are Sass, imported by bare specifier (`@use '@greendrake/theme'`); `@greendrake/vite-preset`'s README explains why that form and no other. Row types handed to `@greendrake/ui-data` components must be type aliases, not interfaces — the table components take rows as `Record<string, unknown>`, which an interface does not satisfy.

## A pair, end to end

[`examples/service-pair`](examples/service-pair) shows several of the packages working together — a demonstration, not the edge of what they are for. It is a complete dashboard and the backend it talks to, built from nothing but these packages: a dispatch table with per-method auth and schema-validated arguments, an HTTP endpoint and a WebSocket one over it, health probes and a graceful shutdown, and a Vue dashboard whose one control follows the service's state live rather than polling for it. It has its own test at both levels — the API driven by the client the dashboard bundles, and the whole thing through a browser — and CI runs both on every push here.

Each package's README documents the rest: the wire format, transports and live bindings (`@greendrake/rpc`, `@greendrake/rpc-server`, `@greendrake/vue-api`), the dispatch table and push registry (`@greendrake/rpc-server`), the service binary's probes and shutdown (`@greendrake/server`), the components and their styling contract (`@greendrake/ui`, `@greendrake/theme`), the data table and CRUD kit (`@greendrake/ui-data`), the app bootstrap (`@greendrake/vue-app`, `@greendrake/dash`) and the Playwright kit (`@greendrake/e2e`).

## Developing in this repository

```sh
bun install
bun run check   # vue-tsc / tsc in every package
bun run lint
bun run test
bun run e2e     # the example pair, through a browser
```
