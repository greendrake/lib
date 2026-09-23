# @greendrake/server

What a service binary needs around its request handler: health probes an orchestrator can route on, CORS, and the SIGTERM sequence that gets the process out of the way without dropping work. **Runs on [bun](https://bun.sh)** — it owns the `Bun.serve` call.

It knows nothing about what the service serves; [`@greendrake/rpc-server`](../rpc-server) is one thing that can sit inside it, a plain `fetch` handler is another.

## Install

```sh
bun add @greendrake/server
```

TypeScript source ships as-is (`src/main.ts` is the entry; no build step, no `.d.ts`). Extend `@greendrake/dev-config/tsconfig/node.json` with `"types": ["bun"]`.

## Serving

```ts
import { serve } from '@greendrake/server'

const service = serve({
    name: 'billing-api',
    port: 8080,
    fetch: (request, server) => router(request, server),
    checks: [
        { name: 'db', check: signal => db.ping(signal) },
        { name: 'cache', check: signal => cache.ping(signal) }
    ],
    probeTimeoutMs: 2000,
    shutdown: { deadlineMs: 15_000, lameDuckMs: 3000 },
    drain: () => worker.finish()
})
```

`websocket` takes a `Bun.serve` WebSocket handler where the service has one. `cors` takes `{ origin?, allowHeaders?, exposeHeaders? }`, or `false` for a service no browser reaches.

The returned handle carries the `Bun.Server`, a `draining` flag the application can read to stop taking on work of its own, and `stop()` — the same sequence the signal handlers run, so a test can drive it without the process exiting.

## Probes

`/healthz` — is this process still the process. It deliberately asks nothing of the dependencies: a database outage must not get the service killed and restarted into the same outage. 200, or 503 once draining.

`/readyz` — can this process serve right now. Every check runs at once under `probeTimeoutMs`, and the body names each one's verdict, so a stalled deploy says which dependency and why rather than leaving a bare 503 to be guessed at:

```json
{ "db": "ok", "cache": "fail: connection refused" }
```

The deadline is enforced around each check rather than left to it: a dependency that has stopped answering typically stops answering its cancellation too, and a readiness probe that hangs is worse than one that fails. The check still receives the signal, and should pass it to its I/O.

Both are answered before the application's `fetch` sees the request, and CORS wraps both — a preflight never reaches the app router, or any middleware hung off it that would charge a rate limit for a request the browser made on its own.

## Shutting down

On SIGINT or SIGTERM, in order:

1. `draining` flips, so the probes report 503 and whatever routes traffic stops sending it.
2. `lameDuckMs` passes, still serving — that gap is what gives the unready state time to propagate before the sockets stop accepting. Zero for a service nothing routes to.
3. The listener stops accepting, then `drain()` runs: finish in-flight work, close pools, flush.

The whole sequence races `deadlineMs`. Past it `stop()` answers `'forced'` and the signal handler exits non-zero — task brokers and HTTP clients recover from that (re-delivery, retry), whereas a binary that will not die has to be killed by something else.
