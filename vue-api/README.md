# @greendrake/vue-api

What a Vue app needs around every call to its server, decided once: a loading indicator, one prompt when a call fails, waiting for an outage to end, and live updates from what the server pushes. It is the Vue and Pinia binding of `@greendrake/rpc`.

```sh
bun add @greendrake/vue-api vue pinia
```

`vue` (^3.5.34) and `pinia` (^3.0.4) are peers. `@greendrake/rpc`, `@greendrake/ui` (the toast, and its `ok` and `cancel` strings), `@greendrake/util` and `@greendrake/vue-kit` (the global loading state) install as dependencies.

The package ships TypeScript source and one Vue SFC with an SCSS style block as they are, with no build step and no `.d.ts`. The consuming Vite + Vue 3 app compiles them, so it needs `@vitejs/plugin-vue` and a Sass implementation in its toolchain. Pinia has to be installed before the first call that is not `silent`.

## Why

`@greendrake/rpc` gives an app a typed call that resolves or rejects. An app needs more than that around each call. Something should show that the call is pending. When it fails, the user should be told, and offered another try. When the network is down, the call should wait rather than fail.

Written at each call site, that is a `try`/`catch`, a busy flag and an error message, repeated everywhere and a little different each time. A global error hook removes the repetition, but it can only report. It cannot run the failed call again and hand the result to the code that was awaiting it. `ApiClient` puts all of it behind the call itself, so the calling code sees success.

## Trade-offs

- **It decides the UX.** A failure is one blocking toast, drawn by `@greendrake/ui`, that covers the app until it is answered. The wording can be changed, and the behaviour cannot. An app that shows its errors inline, by the form or the call, has to opt each call out with `silent`, or pass codes back to the caller with `bubbled`.
- **One wire format.** It works only with `@greendrake/rpc`'s wire format, and with Vue 3, Pinia and a browser.
- **Waiting has no deadline.** A call that waits for the connection stays pending for as long as the outage lasts. A call whose error handler returned without throwing never settles at all, unless the call was made `unattended`. The code after its `await` does not run, and neither does a `finally` around it.
- **Try again sends a new request.** Each attempt carries a fresh nonce, so the server cannot recognise it as a repeat. Where a call reached the server and only its response was lost, Try again applies it a second time. That is why such a call is put to the user, and not re-sent automatically.

## Overview

`ApiClient` does this around every call:

- **The calling code sees success.** A failed call raises a prompt with Try again and Cancel. Try again re-runs the call, and the code awaiting it carries on with the result. By default a call rejects only when the user answers the prompt with anything but Try again.
- **One prompt, whatever fails.** One blocking toast stands for every failure raised while it shows, and Try again retries them all.
- **Calls wait for the connection.** A call that fails while the connection is down is kept pending, and sent again when the connection returns. That is done only where sending it again cannot apply it twice.
- **Loading state without wiring.** Each call adds one to the app's loading counter while it is pending, or sets a `Ref<boolean>` that the caller passes.
- **Errors by code.** A handler per error code runs before the prompt. It can end the session, redirect, or pass the error back to the caller.
- **A response cache.** The methods an app lists are answered from memory for a set time.

Beside the client:

- **Connectivity from the connection.** Given the socket that carries the calls, `useConnectivity` reports whether calls can succeed. `navigator.onLine` cannot say: it stays true behind a captive portal, and when the server cannot be reached.
- **Live data.** `useLive` binds a component to the server's pushes, and keeps a failed re-read from interrupting the user. `keepDocument` and `keepTopicDocuments` keep reactive objects in step with the server's state streams. A state stream, in `@greendrake/rpc`'s wire format, is a document that the server sends whole and then patches.
- **A retry for plain fetches.** `retryWhenOnline` makes a `fetch` outside the client wait for the connection, as the client's own calls do.

## The app's `api.ts`

One module declares the typed method map, builds the transport and exports the client. Every store and component imports the client from it.

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
    'vlan.list': () => Vlan[]
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
import { WebSocketPipe, WsTransport, tokenAuth } from '@greendrake/rpc'
import { ApiClient, bubbled, configureApiUX } from '@greendrake/vue-api'

export interface Methods {
    'taxonomy.tree': (args: { taxonomy: string }) => TaxonomyNode[]
    'thread.message.create': (args: { thread_id: string; body: string }) => { message: Message }
}

export interface Pushes {
    'thread.message': ThreadMessageEvent
}

export const transport = new WsTransport<Pushes>({ pipe: new WebSocketPipe({ url: __WS_URL__ }), auth: tokenAuth })

// This socket carries every call, so its state says whether the app is connected.
configureApiUX({ connectionSource: transport })

export const api = new ApiClient<Methods>({
    transport,
    locale: () => i18n.global.locale.value,
    cache: { ttl: 300, endpoints: { 'taxonomy.tree': true } },
    errorHandlers: {
        SESSION_EXPIRED: () => session.end(),
        ...bubbled(['INVALID_ARGUMENT'])
    }
})
```

Calls from components and stores:

```ts
import { ref } from 'vue'
import { api } from '@/api'

const host = await api.call('host.get', 42) // Host; the global spinner shows while pending

const saving = ref(false)
await api.with({ pending: saving }).call('host.update', { id: 42, name: 'nas' }) // this ref instead

const vlans = await api.with({ silent: true }).call('vlan.list') // no spinner, no toast; errors reject
```

`call(method, ...args)` takes a method's own arguments, as `RpcClient.call` does (`@greendrake/rpc`'s `Caller`). A method that takes none is called with none. `with(options)` returns a caller whose calls are made under `CallOptions`. It is a separate entry point, because nothing at runtime tells trailing options from a last argument. `ApiCaller<M>` is the two together, for a library to type the client it is given.

## ApiClient

`ApiClientOptions`:

- `transport` — any `@greendrake/rpc` `Transport`.
- `cache?: CacheConfig` — the `@greendrake/rpc` `ResponseCache` config. The endpoint list decides two things at once. A listed method is served from the cache and stored in it, and it is sent without a nonce. List only reads of static data that is the same for everyone.
- `idempotent?: (keyof M & string)[]` — the methods that are safe to send twice, but whose answers are not to be reused. These are per-user reads, and writes that are idempotent by the identity they carry (a record created under an id that the client generated, an upload presigned for a named object). A listed method is sent without a nonce and is never cached. A method on neither list carries a nonce and is never cached. A nonce gives a request two things on the server: deduplication, and abort by nonce. A method that is safe to send twice needs neither. Without a nonce, a call that an outage cuts off waits for the connection instead of asking the user (see Waiting for the connection). So a method that a caller would abort by its nonce belongs on neither list.
- `errorHandlers?: Record<string, (e: ApiError) => void>` — keyed by the `code` of the `ApiError` a call fails with (a `NotFoundError` included), and consulted before the toast. A handler that throws passes the error to the caller with its `details` intact, and no toast is shown. A handler that returns has replaced the call's flow with its side effect (logout, redirect). The call then never settles, unless it was made `unattended`: an `unattended` call rejects with the error. `bubbled(codes, as?)` builds the handlers that pass each of `codes` to the caller. Each throws what `as` makes of the error, which by default is the error itself.
- `locale?: () => string | undefined` — the UI language tag put in every request envelope, evaluated per call.

`CallOptions`:

- `pending` — `undefined`: the call adds one to `@greendrake/vue-kit`'s `useAppState` loading counter while it is pending (the global spinner). A `Ref<boolean>`: that scoped busy flag is set instead. `false`: no loading indication.
- `validate: (result) => boolean | Error` — rejects a result that the server calls success. A returned `Error` is thrown, and `false` throws `UnacceptableResultError`. Either is then handled as a failed call is.
- `silent: true` — no loading indication, no error handlers and no toast: errors reject straight to the caller. It is for background calls, and for callers that present their own errors.
- `unattended: true` — a call that nobody is watching, such as one made by a background task. It waits for the connection during an outage as other calls do (see Waiting for the connection). The error handler of its code runs too: an answer that ends the session ends it whichever call brings it. But it shows no loading indication (`pending` is not consulted) and no toast, and every failure rejects to the caller. Where a handler ran and returned, the call rejects after the handler. An `unattended` call always settles, since the code awaiting it has to learn how it ended. `silent` skips both the waiting and the handlers, which is why it does not serve here.
- `cache: false` — reads past the cache and does not store the answer. It cannot opt an unlisted method in, and it leaves the nonce rule alone.

Cache access on the client: `invalidateCache(method, ...args)` (with args, that entry; without, the whole method), `clearCache()`.

`setDebugDelay(method, ms)` is for dev builds only. It holds a call pending for `ms`, so that its loading state can be inspected. `0` clears the delay. It is inert in production.

## Failed calls

A call may fail with no error handler taking the failure. Unless it is `silent` or `unattended`, the call then raises the failure on `useExceptionState()`. That store shows one blocking toast at a time (`@greendrake/ui`'s `showToast`, `variant: 'warning'`, `blocking`). The toast's kind is `classifyException(e)`:

| kind       | error                                         | toast                                                                                                                                                              |
| ---------- | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `notfound` | `NotFoundError`                               | acknowledge-only — asking again would ask for what is not there: "To the home page" when nothing usable is on screen yet, otherwise "OK" and the visitor stays put |
| `network`  | `NetworkError` (fetch failure, socket closed) | Try again / Cancel                                                                                                                                                 |
| `other`    | everything else                               | Try again / Cancel; also reported to the `errorSink` and logged with `console.warn`                                                                                |

The toast stands for every failure raised while it shows, and it shows the kind of the latest one. Try again re-runs every call the toast stands for. The button reads "Trying..." until every retried attempt settles, and a repeat failure counts up in the message. Cancel rejects each call's promise with its original error. A call's success withdraws that call's own failure, never one raised by a concurrent call. The toast closes when the last failure it stands for is withdrawn. Each caller of `fail` registers its own failure, even when another caller has registered the same error object. The app's error handler (`apiUXHooks.onError`) skips an error that has already been raised. So a cancelled call's error adds nothing when its caller lets it propagate.

`useExceptionState()` has the state `exception: ExceptionKind` (`'network' | 'notfound' | 'other' | false`), and three actions:

- `fail(e: Error, options?: FailOptions)` registers a failure, and returns the function that withdraws it.
- `raised(e)` says whether the error has been registered.
- `cancelAll()` cancels every failure the toast stands for, as the toast's Cancel does.

`FailOptions`:

- `retry?: () => Promise<void> | void` — a promise keeps the toast on "Trying..." until it settles.
- `cancel?: () => void`
- `onDismiss?: () => void` — runs when the toast is cancelled, or when a not-found's toast is acknowledged.
- `reportContext?: Record<string, unknown>` — attached to the error report.

An app raises its own errors through the same store, e.g. `fail(new NotFoundError())` from a route that resolved to nothing.

Start-up configuration, `configureApiUX({ errorSink?, goHome?, connectionSource? })`:

- `errorSink: ErrorSink` — `(error, context?) => void`. It receives `other`-kind failures and abnormal WebSocket closes (`WebSocketClosedError.isAbnormal`), for reporting. Without one, nothing is reported.
- `goHome` — the home action of the not-found toast. The default is `location.assign('/')`. `getGoHome()` returns the configured function, for modules that navigate home outside a component.
- `connectionSource: ConnectionSource` — `{ readonly connected: boolean; onConnectionChange(listener) }`. It is read once, when `useConnectivity` is first used, so configure it before then. `@greendrake/rpc`'s `WsTransport` satisfies it as it is.

The configuration is page-wide: there is one `errorSink`, one `goHome` and one `connectionSource`. An app with two sockets has to choose which of them reports connectivity.

`apiUXHooks` is `{ onRouter(router), onError(error), onRouteFetchError() }`, for an app's start-up code that offers those hooks:

- `onRouter` sets `goHome` to `router.push('/')` the moment the router exists.
- `onError` shows a failed start-up, or any error Vue catches, through `useExceptionState().fail`. Its Try again reloads the page.
- `onRouteFetchError` quietly abandons a navigation whose data fetch failed. The fetch went through the client, whose toast has already shown the failure.

The router is typed structurally (`{ push(to: string): unknown }`), so this package takes no router dependency.

### Strings

Every string this package shows is in one reactive `apiMessages` object (`ApiMessages`), built with `@greendrake/ui`'s `configurableMessages`. `configureApiMessages(overrides)` merges overrides in. Call it at start-up, and again on a locale change. Keys and defaults:

- the toast's: `errorOther` "Oops.. Sorry, something went wrong", `errorNetwork` "Looks like a network connectivity issue", `errorNotFound` "The requested page not found", `tryAgain`, `trying` "Trying...", `toHomePage` "To the home page". Its OK and Cancel are `@greendrake/ui`'s `ok` and `cancel`.
- the connection's (see `useConnectionMessage`): `deviceOffline` "You appear to be offline — waiting for your connection…", `unreachable` "Cannot reach the site — waiting for the connection…".

`apiStrings` is these strings in the form that a host's i18n setup takes them. The host is the app that uses this package. The strings are read from the `api` namespace of the host's message catalogue, under these keys.

### Waiting for the connection

A call that is not `silent` may get a `NetworkError` while `useConnectivity().offline` is true. The call is then kept pending and not failed, provided that sending it again cannot apply it twice. That holds in two cases: the call carries no nonce (a method on the `cache` or `idempotent` list), or the transport rejected it before writing it (`WebSocketClosedError` with `inFlight === false`). A waiting call does not count in the global loading counter. It waits on `whenOnline()`, and re-runs once per reconnection. A scoped `pending` ref stays on meanwhile, so the submitting control stays disabled.

A request that carries a nonce and had been sent when the socket closed is another matter: it may have executed on the server, with only its response lost. An `unattended` call therefore rejects with the error, for its caller to decide. Any other call gets the toast, and the decision stays with the user.

## Connectivity

`useConnectivity()` is a Pinia setup store:

- `offline` — calls cannot currently succeed. It is driven by the configured `ConnectionSource`. With none, it is driven by `navigator.onLine` and the window's `online`/`offline` events. With a source it starts `false` (not yet known), so that no offline notice flashes while the first connection is being made.
- `confirmedOffline` — offline, and a reconnect attempt has failed too (the second consecutive `false` report). An offline notice that would interrupt a working screen waits for this, so a socket that reconnects on its first try goes unnoticed. Without a source, the device's own report counts as confirmed at once.
- `deviceOffline` — what the device itself reports. With a source it decides nothing but the wording.
- `whenOnline(): Promise<void>` — resolves the moment calls can succeed, or immediately when they already can.

`useConnectionMessage()` is the message to show while offline, as a computed string. It is `apiMessages.deviceOffline` while the device says it is offline, and `apiMessages.unreachable` otherwise. "Your connection is gone" and "the site cannot be reached" are different things to tell someone, and the device's report is the one thing that tells them apart.

`retryWhenOnline(attempt)` re-runs `attempt` each time connectivity returns. It is for fetches made outside `ApiClient` (static content, locale files), which would otherwise stay failed after failing offline. Only a `NetworkError` while `offline` is retried. Anything else is thrown on the first attempt.

```ts
import { fetchJson } from '@greendrake/util'
import { retryWhenOnline } from '@greendrake/vue-api'

const strings = await retryWhenOnline(() => fetchJson<Strings>(`/locales/${locale}.json`))
```

`keepConnected<Methods>(transport, method)` keeps a `WsTransport` connected for the page's life, and returns the function that stops it. It takes any `Revivable`: something with `probe` and `pipe.reconnectNow`. The window's `online` event cuts short the delay of a waiting reconnect. When the page becomes visible, `keepConnected` does the same for a socket that is waiting to reconnect, and probes with `method`. It probes again every 60 s for as long as the page stays visible. A socket that died without a close event (a backgrounded app, an expired NAT mapping) is thereby abandoned and reconnected. Nothing runs while the page is hidden. `method` is a key of the app's method map. A probe takes any answer as proof that the socket is alive, a refusal included, so a method that the server lacks would report every socket alive.

```ts
keepConnected<Methods>(transport, 'app.ping')
```

## Live updates

`bindLive(transport, options)` is `@greendrake/rpc`'s `liveBinding`, with this package's rules for what a `resync` shows and what becomes of its failure. `useLive` is the component-scoped form that releases on unmount.

```ts
import { backgroundCall, useLive } from '@greendrake/vue-api'
import { api, transport } from '@/api'

useLive(transport, {
    events: {
        'thread.message': { when: pushed => pushed.thread_id === props.threadId }
    },
    resync: async () => {
        thread.value = await api.with(backgroundCall(true)).call('thread.get', props.threadId)
    }
})
```

The mechanism — relevance, `apply` versus `resync`, the reconnect re-read, coalescing, release — is documented in `@greendrake/rpc`. What this package adds is what happens around a `resync`:

- It shows nothing while it runs. `backgroundCall(background)` is the one place that decides what that means. For a background load it is `{ silent: true }`, which skips both the loading counter and the handling of a failed call. For a load that somebody asked for it is `{}`. `pending: false` is not enough: it suppresses the spinner, and still raises the blocking retry toast over a screen the user is reading.
- Its failure does not interrupt the user, and an unexpected one is still reported. A `NotFoundError` is dropped: the entity went away while the socket was down, and the next navigation says so. A `NetworkError` is dropped: the socket went again mid-read, and the binding's own reconnect rule re-reads when it returns. Anything else goes to the `errorSink` configured through `configureApiUX`. It is reported, never shown in a toast.
- It must read past a response cache that the missed pushes would have invalidated. Invalidate the entries the read would hit, then read. The fresh answer then replaces the stale entry for the next reader too.

A state stream needs none of this: the server sends the socket the whole document, so there is no re-read to run and no failure of one to classify.

**A document every socket keeps.** `keepDocument(transport, type, initial)` keeps one for the page's life, and returns it. It makes `initial` reactive, and `@greendrake/rpc`'s `liveDocument` applies every frame of `type` to it. A socket is sent such a document whole only as it opens. So call `keepDocument` where the transport is made, before it connects, rather than in a component that may mount afterwards:

```ts
export const transport = new WsTransport<Record<never, never>, Pushes>({ pipe })
export const cameras: Cameras = keepDocument(transport, 'cameras', {})
void transport.connect()
```

**Topic memberships.** `holdTopics(transport, { join, leave })` holds a socket's topic memberships by key, for any topic subscription. `hold(key)` returns the release.

- **Counting.** The first hold of a key joins and the last release of it leaves.
- **Swaps send nothing.** A release takes effect only after Vue has finished its current update (`nextTick`), so a component swapped for another holding the same key sends nothing.
- **Order.** A key's calls run one at a time, in the order asked, so the server never runs a join and a leave out of order.
- **Reconnects.** A dropped socket takes its memberships with it. So after each return from a drop every held key joins again, unless a join of it is still pending. A join that fails with a `NetworkError` is retried at the next such return. Any other failure is the key's `failure(key)`, until a join of the key succeeds.
- **Leaving.** A leave is sent only where there is a membership to leave. It is not sent where the key's last join was refused or lost. It is not sent while the socket is down either: that would only open a new socket to leave a topic it was never in. A leave that fails for any reason but the network goes to the `errorSink`.
- **Plain calls.** `join` and `leave` are plain calls over the transport: an `RpcClient`'s, never an `ApiClient`'s. An `ApiClient` call waits for the connection, or for the user's Try again. Either would re-join a key that nobody holds any more, or keep a join pending indefinitely.

**A document a topic keeps.** `keepTopicDocuments(transport, { typeOf, empty, join, leave })` is `holdTopics` with the document of `typeOf(key)` kept for each membership. The socket is sent the whole document as it joins. So such a document is held where it is shown, at any time, and the first hold opens the socket.

- `doc(key)` is the held key's document. It is `empty()` until the document arrives. It is undefined while the key is not held. It is also undefined while the key's last join has been refused, since nothing more is coming to keep it current.
- A membership's document is fresh. It is started after any earlier leave of the key has settled, so nothing sent before that leave reaches it.

```ts
const units = keepTopicDocuments(transport, {
    typeOf: (name: string) => `unit/${name}` as const,
    empty: () => ({}),
    join: name => client.call('unit.watch', { name }),
    leave: name => client.call('unit.unwatch', { name })
})
const release = units.hold('web') // units.doc('web'), units.failure('web')
```

`backgroundCall` also serves any loader that is told whether anybody asked for its load. A table that re-reads after a push has such a loader, and it is where a panel's re-read actually calls the API:

```ts
const load = (args: ListArgs, background: boolean) => api.with(backgroundCall(background)).call('admin.thread.list', args)
```
