# greendrake/lib

Packages for building web applications, and an app built from them. TypeScript packages for Vue 3 + Vite frontends, bun backends and the typed wire between them: an RPC client and server over HTTP and WebSocket with live server push, and a Vue binding for it; an SPA bootstrap and a dashboard shell; a component library with data-heavy widgets; design tokens and SCSS primitives; service scaffolding with health probes and a graceful shutdown; the Vite, TypeScript, ESLint and Playwright configuration that builds and tests the result; and live camera video in the browser. Go modules for the server side: the same RPC wire format, service scaffolding, a single-page app inside a Go binary, and IP cameras — a DVRIP and an RTSP client, a camera engine, and their streams cast to browsers. And `ipcam-app`, one camera in the browser, served by a single static binary.

They are developed against real applications rather than in the abstract, and published for anyone whose application has the problems they solve. This repository is a one-way mirror of their source, synchronised from the monorepo they are developed in; releases go out from there — npm packages to npm, Go modules as tags here, apps as GitHub Releases with their binaries — each package on its own version, released when it changes. Issues are welcome here; a pull request is applied upstream and arrives back with the next sync.

## Packages

### Frontend

| Package | What it is |
| --- | --- |
| [`@greendrake/dev-config`](dev-config) | TypeScript and Prettier configuration for Vue 3 + Vite projects, as config files to extend or re-export rather than option sets to copy |
| [`@greendrake/css-reset`](css-reset) | Modern CSS reset (adapted from Josh Comeau's and Andy Bell's resets) |
| [`@greendrake/scss-kit`](scss-kit) | Framework-free SCSS primitives: breakpoints (single-sourced with TS via codegen), z-layer map, icon-mask registry, @font-face generator, misc mixins |
| [`@greendrake/font-inter`](fonts/inter) | Inter webfont (woff2, OFL): @font-face registration via the @greendrake/scss-kit generator; opt-in document default via the ./default entry |
| [`@greendrake/util`](util) | Framework-free, Node-safe utilities: typed event emitter, timing, retry, staleness, comparators, fetch helpers, misc |
| [`@greendrake/rpc`](rpc) | RPC client over JSON: typed method-map API, HTTP and reconnecting WebSocket transports, pluggable codec and headers, TTL response cache |
| [`@greendrake/theme`](theme) | Design tokens, document-level element styling, a light/dark theming mixin and an opt-in dashboard palette |
| [`@greendrake/domains`](domains) | Email and domain-name validation backed by the IANA TLD list |
| [`@greendrake/ui`](ui) | Vue 3 component library: form controls, modals, search, tabs, toasts, layout |
| [`@greendrake/vite-preset`](vite-preset) | Vite config factory and build-env helpers for Vue 3 single-page apps: plugin set, strict dev port, alias, dedupe and source-package pre-bundling rules |
| [`@greendrake/vue-kit`](vue-kit) | Vue/Pinia building blocks: app loading state, media queries, collection-store factory |
| [`@greendrake/vue-api`](vue-api) | Vue binding of @greendrake/rpc: loading-state wiring, response cache, error-UX pipeline, silent calls, live bindings to server push |
| [`@greendrake/vue-app`](vue-app) | SPA bootstrap: app/pinia/router assembly, splash gate, route titles and body classes |
| [`@greendrake/dash`](dash) | Dashboard SPA shell: app bootstrap, splash root layout, tab-registry dashboard, a live service-state control and the Vite/env conventions a dashboard needs |
| [`@greendrake/e2e`](e2e) | Playwright kit for single-page apps: config factories, a same-origin crawler, a clipped-content detector, a polling helper and a layout checker |
| [`@greendrake/eslint-config`](eslint-config) | ESLint flat config for TypeScript + Vue 3 projects, to re-export or spread rather than a rule set to copy |
| [`@greendrake/mse-video`](mse-video) | MSE live video over WebSocket: framework-free playback core with stall watchdogs and a server reset protocol, plus a thin Vue wrapper |
| [`@greendrake/ui-data`](ui-data) | Data-heavy dashboard components: virtualized InfiniteScrollTable, CrudPanel/MasterDetail, record editing and the admin-CRUD kit |
| [`@greendrake/ipcam`](ipcam) | The browser end of the camera engine (greendrake.nz/go/ipcam): its API bound to a camera, the cameras' states, and the camera's tile and settings components |

### Backend

| Package | What it is |
| --- | --- |
| [`@greendrake/rpc-server`](rpc-server) | RPC server over JSON for bun: method dispatch with auth, nonce and Standard Schema argument validation, HTTP and WebSocket transports, connection registry and server push |
| [`@greendrake/service-state`](service-state) | A backend service as a state machine (OFF/STARTING/ON/STOPPING/ERROR): the wire contract both halves share, and the server-side machine behind it |
| [`@greendrake/server`](server) | Service-binary scaffolding for bun: dependency health probes with /healthz + /readyz, CORS, and the SIGTERM-driven graceful-shutdown sequence |

### Go

| Package | What it is |
| --- | --- |
| [`greendrake.nz/go/apiserver`](go/apiserver) | Package apiserver is generic net/http scaffolding for a service binary: dependency health probes with /healthz + /readyz handlers, the process lifecycle (listeners, background work, the SIGTERM-driven graceful shutdown), CORS middleware, and JSON answers for plain-HTTP endpoints. |
| [`greendrake.nz/go/camstream`](go/camstream) | Package camstream is a camera's stream, whatever brings it and wherever it goes: its frames, video or audio, with their codecs and timing; a connected stream (Monitor); and a stream fanned out to its subscribers while it has any (Source). |
| [`greendrake.nz/go/dvrip`](go/dvrip) | Package dvrip is a client for DVRIP, the protocol of Xiongmai-based cameras and recorders (also known as Sofia, NetSurveillance or XMEye): logging in, reading and writing configuration, and pulling live streams with their audio. |
| [`greendrake.nz/go/ipcam`](go/ipcam) | Package ipcam is the camera engine's core: what it asks of a camera, through the ports Driver, Camera and Settings that each protocol's adapter implements, and the settings it edits, in terms of its own rather than any protocol's. |
| [`greendrake.nz/go/ipcamrpc`](go/ipcamrpc) | Package ipcamrpc is a greendrake.nz/go/ipcam fleet over the wire, to a browser app: the methods it calls on the cameras (greendrake.nz/go/rpcserver), each on the camera it names; every camera's state, which it keeps (a Broadcast document); and the streams it watches (greendrake.nz/go/webcast), keyed <camera>/<stream>. |
| [`greendrake.nz/go/rpcserver`](go/rpcserver) | Package rpcserver is the server end of the `@greendrake/rpc` wire format: a method dispatch table with per-method authentication, served over HTTP (one POST endpoint) and over WebSocket (the same envelope plus a correlation id and server push). |
| [`greendrake.nz/go/rtsp`](go/rtsp) | Package rtsp is an RTSP 1.0 client (RFC 2326), as thin as playing an IP camera's stream takes: requests on one TCP connection, authenticated as the camera asks (Basic, or Digest), the stream's RTP packets sent to a pair of the client's UDP ports or interleaved on the connection, and H.265 put back together from them. |
| [`greendrake.nz/go/spa`](go/spa) | Package spa ships a single-page app inside a Go binary: the binary's main package embeds the app's build from its dist/ directory, which the bake command fills before compiling the binary, and Handler serves it. |
| [`greendrake.nz/go/webcast`](go/webcast) | Package webcast casts live camera streams (greendrake.nz/go/camstream) to browsers: H.265 in fragmented MP4 with the camera's G.711 audio beside it, any number of streams over one WebSocket, each pulled off its camera only while someone watches. |

### Apps

| Package | What it is |
| --- | --- |
| [`ipcam-app`](apps/ipcam-app) | One DVRIP or RTSP camera in the browser, its streams, and over DVRIP their sound and the camera's settings, served by a single static binary |

## Consuming the TypeScript packages

Every package ships its TypeScript, Vue SFC and SCSS **source** — there is no build step and no `.d.ts` output. A consuming app compiles them with its own Vite and `vue-tsc`, which asks three things of it:

- **Run under bun**: `bun --bun vite …`, `bun --bun playwright test`, `bun src/main.ts`. `vite.config.ts` imports `@greendrake/dash/vite` or `@greendrake/vite-preset`, `playwright.config.js` imports `@greendrake/e2e`; both are TypeScript under `node_modules`, and Node does not type-strip files there. The backend packages go further and are bun-native: `Bun.serve` owns their sockets. The browser bundle itself has no such requirement — Vite compiles the packages' `.ts`, `.vue` and `.scss` like any other source.
- **Extend the shared tsconfig**: `"extends": "@greendrake/dev-config/tsconfig/vue.json"` for an app, `node.json` with `"types": ["bun"]` for a service, so `vue-tsc` and `tsc` check the source under the compiler options it is written for. The packages a service imports also hold under `strict.json` over that, for a service stricter than the default.
- **Provide the peer dependencies**: `vue@^3.5`, `vue-router@^4.6`, `pinia@^3`, `vite@>=8`, `typescript@^6`, `sass-embedded` and `vue-tsc` for the frontend; `@playwright/test` where `@greendrake/e2e` is used. A backend-only consumer needs none of them.

Styles are Sass, imported by bare specifier (`@use '@greendrake/theme'`); `@greendrake/vite-preset`'s README explains why that form and no other. Row types handed to `@greendrake/ui-data` components must be type aliases, not interfaces — the table components take rows as `Record<string, unknown>`, which an interface does not satisfy.

## Consuming the Go modules

`go get greendrake.nz/go/<name>`. The modules live in [`go/`](go), one directory each, and a version of one is the tag `go/<name>/v<version>`; the module proxy and pkg.go.dev serve them like any other. Inside this repository, [`go.work`](go.work) puts them beside each other and the apps' servers.

## A pair, end to end

[`examples/service-pair`](examples/service-pair) shows several of the packages working together — a demonstration, not the edge of what they are for. It is a complete dashboard and the backend it talks to, built from nothing but these packages: a dispatch table with per-method auth and schema-validated arguments, an HTTP endpoint and a WebSocket one over it, health probes and a graceful shutdown, and a Vue dashboard whose one control follows the service's state live rather than polling for it. It has its own test at both levels — the API driven by the client the dashboard bundles, and the whole thing through a browser — and CI runs both on every push here.

[`apps/ipcam-app`](apps/ipcam-app) is an app built from both kinds: a Go server baking in its Vue SPA. Each release of it carries its binaries.

Each package's README documents the rest: the wire format, transports, live bindings and state documents (`@greendrake/rpc`, `@greendrake/rpc-server`, `@greendrake/vue-api`, `greendrake.nz/go/rpcserver`), the dispatch table and push registry (`@greendrake/rpc-server`), the service binary's probes and shutdown (`@greendrake/server`, `greendrake.nz/go/apiserver`), the components and their styling contract (`@greendrake/ui`, `@greendrake/theme`), the data table and CRUD kit (`@greendrake/ui-data`), the app bootstrap (`@greendrake/vue-app`, `@greendrake/dash`), the Playwright kit (`@greendrake/e2e`), and the cameras (`greendrake.nz/go/ipcam`, `@greendrake/ipcam`).

## Developing in this repository

```sh
bun install
bun run check   # vue-tsc / tsc in every package
bun run lint
bun run test
./check-go      # gofmt, vet and test of every Go module
bun run e2e     # the example pair and the apps, through a browser
```
