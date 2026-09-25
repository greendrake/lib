# @greendrake/vue-api

The Vue/Pinia binding of `@greendrake/rpc`. `ApiClient` adds loading-state wiring, a response cache, an error-UX pipeline (one blocking retry/acknowledge toast), offline parking and per-code error handlers to a typed method map; alongside it a connectivity store, a retry helper for plain fetches, and a component-scoped WebSocket push subscription.

## Install

```sh
bun add @greendrake/vue-api vue pinia
```

`vue` (^3.5.34) and `pinia` (^3.0.4) are peers. The package ships TypeScript source and one Vue SFC with an SCSS style block as-is (no build step, no `.d.ts`): the consuming Vite + Vue 3 app compiles them, so it needs `@vitejs/plugin-vue` and a Sass implementation in its toolchain. `@greendrake/rpc`, `@greendrake/ui` (the toast and its message strings), `@greendrake/util` and `@greendrake/vue-kit` (the global loading state) install as dependencies.

## The app's `api.ts`

One module declares the typed method map, builds the transport and exports the client; every store and component imports from it.

```ts
// src/api.ts
import { HttpTransport } from '@greendrake/rpc'
import { ApiClient } from '@greendrake/vue-api'
import type { Host, HostQuery, ListResult, Vlan } from './types'

export interface Methods {
    'host.list': (query: HostQuery) => ListResult<Host>
    'host.get': (id: number) => Host
    'host.update': (record: { id: number } & Partial<Host>) => Host
    'host.delete': (args: { id: number }) => void
    'vlan.list': (query: Record<string, never>) => Vlan[]
}

export const api = new ApiClient<Methods>({
    transport: new HttpTransport({
        url: __API_URL__,
        headers: () => ({ 'x-api-key': __API_KEY__ })
    })
})
```

The same with a WebSocket transport, in-band session auth and server push:

```ts
// src/api.ts
import { WebSocketPipe, WsTransport, type WsAuthStrategy } from '@greendrake/rpc'
import { ApiClient, configureApiUX } from '@greendrake/vue-api'

export interface Methods {
    'auth.token': (token: string) => void
    'auth.logout': () => void
    'taxonomy.tree': (args: { taxonomy: string }) => TaxonomyNode[]
    'thread.message.create': (args: { thread_id: string; body: string }) => { message: Message }
}

export interface Pushes {
    'thread.message': ThreadMessageEvent
}

const auth: WsAuthStrategy = {
    authenticate: (send, token) => send({ method: 'auth.token', arguments: [token] }),
    deauthenticate: send => send({ method: 'auth.logout' })
}

export const transport = new WsTransport<Pushes>({ pipe: new WebSocketPipe({ url: __WS_URL__ }), auth })

// This socket carries every call, so it is the app's connectivity oracle.
configureApiUX({ connectionSource: transport })

export const api = new ApiClient<Methods>({
    transport,
    locale: () => i18n.global.locale.value,
    cache: { ttl: 300, endpoints: { 'taxonomy.tree': true } },
    errorHandlers: {
        UNAUTHENTICATED: () => session.end(),
        INVALID_INPUT: e => {
            throw e
        }
    }
})
```

Calls from components and stores:

```ts
import { ref } from 'vue'
import { api } from '@/api'

const host = await api.call('host.get', [42]) // Host; the global spinner shows while pending

const saving = ref(false)
await api.call('host.update', [{ id: 42, name: 'nas' }], { pending: saving }) // this ref instead

const vlans = await api.call('vlan.list', [{}], { silent: true }) // no spinner, no toast; errors reject
```

`call(method, args, options?)` takes the arguments as a tuple (`Parameters<M[K]>`), unlike `RpcClient.call`'s variadic form.

## ApiClient

`ApiClientOptions`:

- `transport` — any `@greendrake/rpc` `Transport`.
- `cache?: CacheConfig` — the `@greendrake/rpc` `ResponseCache` config. The endpoint list decides two things at once: a listed method is served from and stored in the cache and goes out without a nonce; every other method carries a nonce and is never cached. List only stateless, platform-static reads.
- `errorHandlers?: Record<string, (e: Error) => void>` — keyed by server error code (`codeOf`), consulted before the toast. A handler that throws bubbles the error to the caller with its `ApiError.details` intact, bypassing the toast; a handler that returns parks the call forever — its side effect (logout, redirect) supersedes the call's flow.
- `locale?: () => string | undefined` — the UI language tag put in every request envelope, evaluated per call.

`CallOptions`:

- `pending` — `undefined`: the call holds a slot in `@greendrake/vue-kit`'s `useAppState` loading counter (the global spinner). A `Ref<boolean>`: that scoped busy flag is set instead. `false`: no loading indication.
- `validate: (result) => boolean | Error` — rejects a result the server calls success: a returned `Error` is thrown, `false` throws `UnacceptableResultError`. Feeds the same retry pipeline as a failure.
- `silent: true` — no loading indication and no error UX; errors reject straight to the caller. For background calls and callers owning their own error presentation.
- `cache: false` — reads past the cache and does not store the answer. Cannot opt an unlisted method in and leaves the nonce rule alone.

Cache access on the client: `invalidateCache(method, ...args)` (with args, that entry; without, the whole method), `clearCache()`, `primeCache(method, args, data)`.

`setDebugDelay(method, ms)` — dev builds only: holds a call pending for `ms` so its loading state can be inspected; `0` clears. Inert in production.

## Error-UX pipeline

A non-silent call that fails, and that no error handler claimed, raises the failure on `useExceptionState()`, which shows one blocking toast at a time (`@greendrake/ui`'s `showToast`, `variant: 'warning'`, behind a full-screen overlay). The toast's kind is `classifyException(e)`:

| kind | error | toast |
| --- | --- | --- |
| `notfound` | `NotFoundError` | acknowledge-only: "To the home page" when nothing usable is on screen yet, otherwise "OK" and the visitor stays put |
| `oneoutcome` | any other `OneOutcomeError` | acknowledge-only, shows the error's own message |
| `network` | `NetworkError` (fetch failure, socket closed) | Try again / Cancel |
| `other` | everything else | Try again / Cancel; also reported to the error sink and logged with `console.warn` |

Retry re-runs the same call (the button reads "Trying..." until the retried attempt settles, and a repeat failure counts up in the message); Cancel rejects the call's promise with the original error. A call's success clears its own toast, never one raised by a concurrent call. A same-kind failure while a toast is showing re-drives the existing toast rather than stacking another. The copy is `@greendrake/ui`'s `messages` (`errorNetwork`, `errorNotFound`, `errorOther`, `tryAgain`, `trying`, `cancel`, `ok`, `toHomePage`), overridable with `configureUiMessages`.

`useExceptionState()`: state `exception: ExceptionKind` (`'network' | 'notfound' | 'oneoutcome' | 'other' | false`); action `fail(e: Error | false, options?: FailOptions)`, where `false` clears the toast. `FailOptions`: `retry?: () => Promise<void>`, `cancel?: () => void`, `onDismiss?: () => void` (runs when an acknowledge-only toast is acknowledged), `reportContext?: Record<string, unknown>` (attached to the error report). An app raises its own errors through the same store, e.g. `fail(new NotFoundError())` from a route that resolved to nothing.

Boot wiring, `configureApiUX({ errorSink?, goHome?, connectionSource? })`:

- `errorSink: ErrorSink` — `(error, context?) => void`; receives `other`-kind failures and abnormal WebSocket closes (`WebSocketClosedError.isAbnormal`) for reporting. Nothing is reported without one.
- `goHome` — the not-found toast's home action; default `location.assign('/')`. `getGoHome()` returns the configured function for modules that navigate home outside a component.
- `connectionSource: ConnectionSource` — `{ readonly connected: boolean; onConnectionChange(listener) }`, read once when `useConnectivity` first materialises, so configure it before the store is first used. `@greendrake/rpc`'s `WsTransport` satisfies it as-is.

`apiUXHooks` is `{ onRouter(router), onBootError(error) }`, for an app bootstrap that offers those two hooks: `onRouter` sets `goHome` to `router.push('/')` the moment the router exists, and `onBootError` shows a failed boot through `useExceptionState().fail`. The router is typed structurally (`{ push(to: string): unknown }`), so this package takes no router dependency.

### Offline parking

A non-silent call that fails with a `NetworkError` while `useConnectivity().offline` is true is parked instead of toasted, provided re-issuing it cannot double-apply: it carries no nonce (a cacheable read), or the transport rejected it before writing it (`WebSocketClosedError` with `inFlight === false`). A parked call releases its global loading slot — a scoped `pending` ref stays on, so the submitting control stays disabled — waits on `whenOnline()`, and re-runs once per reconnection. A nonce-carrying request that was on the wire when the socket went may have executed server-side with only its response lost, so it keeps the toast and the decision stays with the user.

## Connectivity

`useConnectivity()` is a Pinia setup store:

- `offline` — calls cannot currently succeed. Driven by the configured `ConnectionSource`; with none, by `navigator.onLine` and the window's `online`/`offline` events. With a source it starts `false` (not yet known), so the boot handshake does not flash offline surfaces.
- `confirmedOffline` — offline and a reconnect attempt has failed too (the second consecutive `false` report). Surfaces that interrupt a working screen wait for this, so a socket that reconnects on its first try passes unremarked. Without a source the device signal confirms itself.
- `deviceOffline` — the device's own claim. With a source it is never authoritative; it only picks wording.
- `whenOnline(): Promise<void>` — resolves the moment calls can succeed; immediately when they already can.

`retryWhenOnline(attempt)` re-runs `attempt` each time connectivity returns, for fetch sites that bypass `ApiClient` (static content, locale files) and would otherwise be a dead end after an offline miss. Only a `NetworkError` while `offline` is retried; anything else propagates on the first attempt.

```ts
import { fetchJson } from '@greendrake/util'
import { retryWhenOnline } from '@greendrake/vue-api'

const strings = await retryWhenOnline(() => fetchJson<Strings>(`/locales/${locale}.json`))
```

## Live updates

`bindLive(transport, options)` is `@greendrake/rpc`'s `liveBinding` under this package's UX policy, and `useLive` is the component-scoped form that releases on unmount.

```ts
import { backgroundCall, useLive } from '@greendrake/vue-api'
import { api, transport } from '@/api'

useLive(transport, {
    events: {
        'thread.message': { when: pushed => pushed.thread_id === props.threadId }
    },
    resync: async () => {
        thread.value = await api.call('thread.get', [props.threadId], backgroundCall(true))
    }
})
```

The mechanism — relevance, `apply` versus `resync`, the reconnect re-read, coalescing, release — is documented in `@greendrake/rpc`. What this package adds is what happens around a `resync`:

- It runs unseen. `backgroundCall(background)` is the one place that decides what that means: `{ silent: true }`, which bypasses both the loading counter and the error-UX pipeline. `pending: false` is not enough — it suppresses the spinner and still raises the blocking retry toast over a screen the user is reading.
- Its failure neither interrupts nor vanishes. A `NotFoundError` is dropped (the entity went away while the socket was down; the next navigation says so), a `NetworkError` is dropped (the socket went again mid-read, and the binding's own reconnect rule re-reads when it returns), and anything else goes to the `errorSink` configured through `configureApiUX` — reported, never toasted.
- It must read past a response cache the missed pushes would have invalidated: invalidate the entries the read would hit, then read, so the stale entry is repaired rather than stepped around.

`backgroundCall` also serves any loader told whether anybody asked for its load — a table re-reading after a push, say, which is where a panel's re-read actually calls the API:

```ts
const load = (args: ListArgs, background: boolean) => api.call('admin.thread.list', [args], backgroundCall(background))
```
