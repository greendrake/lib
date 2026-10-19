# service-pair

A dashboard and the backend it talks to, both TypeScript, both built entirely from the published packages. It shows one thing: a control for a service that is `OFF`, `STARTING`, `ON`, `STOPPING` or `ERROR`. Pressing *Turn ON* starts it; three seconds later it is on — and so it says on every dashboard open at the time, because the state arrives as a server push rather than an answer to whoever asked.

The service itself is mocked: a flag, a delay, and a switch that makes the next transition fail so `ERROR` is reachable. A real one would wrap a systemd unit, a container or a device, and the rest of this would be unchanged.

```
api/   @greendrake/{rpc-server,server,service-state} — dispatch table, WebSocket, the machine
dash/  @greendrake/{dash,rpc,vue-api,service-state}  — one tab, one ServiceStateControl
```

## Running it

```sh
bun install
cd api && API_PORT=8100 ADMIN_API_KEY=example-admin-key DASH_ORIGIN=http://localhost:5180 bun run start
cd dash && bun run dev          # reads .env.dev
```

The dashboard is on the port in `dash/.env.dev`; its `WS_URL` and `ADMIN_API_KEY` must agree with what the API was started with, and the API's `DASH_ORIGIN` with where the dashboard is served: it is the one page the API lets open its socket.

## Testing it

```sh
cd api  && bun test             # the pair over the wire, no browser
cd dash && bun run e2e          # the pair through a browser; starts both halves itself
```

The API's own test drives the real server with `@greendrake/rpc`'s client — the same client the dashboard bundles — so it is an interop test and not a mock on either side. The Playwright run starts both halves and opens two pages, which is how the "everyone sees the same state" claim gets checked.

## What to copy from it

- `api/src/methods.ts` — the dispatch table, and `ClientMethods<…>` deriving the client's method map from it so the two sides cannot drift.
- `api/src/service.ts` — `serve()` with probes and a shutdown deadline around an HTTP endpoint and a WebSocket one.
- `dash/src/api.ts` — one `WsTransport` carrying both the calls and the pushes, authenticated at the handshake.
- `dash/vite.config.ts` — `dashApp` and the `.env.dev` / `.env.prod` convention.
