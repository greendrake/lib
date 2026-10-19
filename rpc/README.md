# @greendrake/rpc

Client for the `@greendrake/rpc` wire format, defined below: a typed method-map facade (`RpcClient`), an HTTP transport and a reconnecting WebSocket transport with server push, the error classes both transports map response codes to, a TTL response cache, and a pluggable payload codec. Framework-free: loading state, error UX and connectivity belong to whatever UI binding is built on it.

## Install

```sh
bun add @greendrake/rpc
```

TypeScript source ships as-is (`src/main.ts` is the entry; no build step, no `.d.ts`), for Vite + Vue 3 apps written in TypeScript. The one runtime dependency, `@greendrake/util` (`NetworkError`, `Emitter`, `safeFetch`, `randomString`), installs with it.

## The wire

This package is the client end of one format, and this section defines it for both ends: every server of the format implements it. It is deliberately not JSON-RPC. JSON on both transports, with one request envelope and one response envelope.

### Envelopes

```ts
interface RpcRequest {
    method: string
    nonce?: string // dedup key; omitted on nonce-less calls
    locale?: string // the caller's UI language tag, when a provider is configured
    arguments?: unknown[] // omitted when empty
}

interface RpcResponse {
    success: boolean
    data?: unknown // on success
    error?: string // the error code, on failure
    error_details?: unknown // structured payload attached alongside the code
}
```

Over HTTP one `POST` carries one request, and its body is the response. Over WebSocket many calls share one socket:

- A request carries an `id`, and its response comes back as `{ type: 'response', id, ...RpcResponse }`. A frame that does not parse is answered with no `id`.
- Calls run concurrently, so responses come back in whatever order they finish.
- The server pushes frames nobody asked for: `{ type: 'event', event, data }`, and bare `{ type, data }` for any other `type` but `response`.

### State streams

A state stream is a bare frame type whose `data` is one of two shapes:

- `{ doc }`, the whole document, a JSON object. A server sends it first on each socket that is to keep the document.
- `{ patch }`, an RFC 7386 merge patch over it. Objects merge, `null` removes a member, and anything else replaces, so collections are objects keyed by id.

A document has no `null` members, since a patch could not tell one from a removal. A patch is only ever over the document its own socket was sent.

### Credentials

A credential is a token, and a token that names nobody leaves its caller anonymous rather than refused: what that costs is decided by each method's own rule.

- **HTTP:** `Authorization: Bearer <token>`.
- **WebSocket, at the handshake:** the `token` query parameter (`TOKEN_PARAM`). A browser cannot put a header on a WebSocket, and a reconnect carries the URL again, so the socket re-authenticates by itself.
- **WebSocket, in band:** the reserved methods, which the server answers itself.
  - `auth.token` (`AUTH_TOKEN_METHOD`), with `arguments: [token]`, binds the socket to the principal the token names. It answers success, `UNAUTHENTICATED` for a token naming nobody, `INVALID_ARGUMENT` for anything but one token, or `AUTH_NOT_CONFIGURED` from a server with no notion of identity.
  - `auth.logout` (`AUTH_LOGOUT_METHOD`) unbinds it, and succeeds.

  Calls sent after one of these are made by whoever it leaves the socket bound as.

A server admits a browser's socket from its own origin and from the origins it is configured with, and refuses any other at the handshake. HTTP's origin policy is CORS.

### Nonces and aborts

A request carrying a `nonce` runs once: a repeat is refused with `DUPLICATE_REQUEST`. The nonce is checked once the method, the principal and its access are settled, so a request refused before then leaves its nonce unspent.

Over WebSocket a frame may carry `abort: <nonce>`, the nonce of an earlier request on the same socket. That request is cancelled, and its response is never sent. The abort itself is answered with nothing. It may come alone, `{ abort }`, or on a new request: a search typed on a debounce cancels the previous query and starts the next in one frame.

### Error codes

The codes the protocol answers with itself are `PROTOCOL_CODES` (`ProtocolCode`, the type). A method's own codes are its contract's.

| Code | Answers |
| --- | --- |
| `INTERNAL_ERROR` | a fault in the method: anything it failed with but a code of its own |
| `UNKNOWN_METHOD` | a method the server does not serve |
| `INVALID_JSON` | a body or frame that is no request envelope |
| `REQUEST_TOO_LARGE` | an HTTP body over the server's limit |
| `INVALID_ARGUMENT` | arguments that do not parse or validate; `error_details`, where present, has an entry per failed field, each naming its `field` (the dotted path as sent, empty for a positional argument) and saying the rest in its validator's terms |
| `AUTH_REQUIRED` | a method that needs a principal, called without one |
| `UNAUTHENTICATED` | an `auth.token` whose token names nobody |
| `AUTH_NOT_CONFIGURED` | an `auth.token` to a server with no notion of identity |
| `DUPLICATE_REQUEST` | a nonce already used |
| `WEBSOCKET_REQUIRED` | a method that needs a socket to push to, called over HTTP |

A code ending in `_NOT_FOUND` says the thing a call names does not exist, and the client maps every one to a `NotFoundError`.

### Building and settling

`packRequest(method, args, nonce, locale?)` builds the request: a fresh random `nonce` when `nonce` is true, `arguments` only when non-empty, `locale` only when supplied. `settleResponse(response, method)` returns `data` on success or throws the mapped error (see Errors); in a Vite dev build a non-success response is also logged with `console.warn`.

## Typed method map

Declare the API surface once as method name → call signature; every call site infers argument and result types from it.

```ts
import { HttpTransport, RpcClient } from '@greendrake/rpc'

interface Methods {
    'user.get': (id: string) => User
    'user.update': (args: { id: string; name: string }) => void
    'search.things': (query: { q: string; limit?: number }) => { data: Thing[]; total: number }
}

const client = new RpcClient<Methods>(new HttpTransport({ url: 'https://api.example.com/v1/api' }))

const user = await client.call('user.get', 'U1') // User
await client.call('user.update', { id: 'U1', name: 'Ann' })
```

`new RpcClient<M>(transport, locale?)` — `locale` is `() => string | undefined`, evaluated per call, so a language switch applies from the next call. `call(method, ...args)` is the one way to invoke a method, and every request it sends carries a nonce. A method that takes no arguments is called with none, and its request carries no `arguments` field. `Caller<M>` is that `call`, which `RpcClient` and `@greendrake/vue-api`'s `ApiClient` both implement: the type for code handed either. `MethodMap` and `MethodMapConstraint<M>` are the constraint types; a plain interface qualifies.

`Transport` is one method, `send(request: RpcRequest): Promise<unknown>`, resolving to the response `data` or rejecting with a mapped error. Anything implementing it plugs into the client.

## HTTP transport

```ts
import { HttpTransport } from '@greendrake/rpc'

const transport = new HttpTransport({
    url: 'https://api.example.com/v1/api',
    headers: () => ({ authorization: `Bearer ${session.token}` }),
    requestInit: { keepalive: true }
})
```

`HttpTransportOptions`:

- `url` — POSTed to for every call.
- `headers?: HeadersProvider` — `() => Record<string, string>`, evaluated per request so time-variant values (tokens) are always fresh.
- `codec?: Codec` — applied to the outbound body and the inbound response text (see Codec).
- `requestInit?: Partial<RequestInit>` — merged into every `fetch` call.

A `fetch` rejection (DNS, refused connection, offline, CORS) surfaces as `@greendrake/util`'s `NetworkError`.

## WebSocket transport

Two layers. `WebSocketPipe` owns the connection lifecycle and raw string frames; `WsTransport` speaks the wire format over it: id-correlated request/response, typed push subscription, and an injected auth handshake re-applied on every reconnect.

```ts
import { WebSocketPipe, WsTransport, tokenAuth } from '@greendrake/rpc'

interface Pushes {
    'thread.message': { thread_id: string; body: string }
}

const transport = new WsTransport<Pushes>({
    pipe: new WebSocketPipe({ url: 'wss://api.example.com/v1/ws' }),
    auth: tokenAuth
})

const off = transport.onEvent('thread.message', data => {
    // data: Pushes['thread.message']
})
await transport.authenticate(session.token)
```

### WebSocketPipe

`WebSocketPipeOptions`:

- `url`
- `queryAuth?: { param: string; token: string }` — a connection-level credential appended to the URL as a query parameter and resolved by the server at the handshake. Because it rides the URL, reconnects re-authenticate by themselves. For per-session, in-band auth use `WsTransport`'s `auth` instead.
- `reconnectDelayMs?: (attempt: number) => number` — delay before reconnect attempt N (0-based, reset on a successful open). Default: a constant 3000 ms.

The first attempt starts on `ensureConnected()`, which is also the eager-connect entry point. Every close — including an attempt that never opened — arms the next attempt behind its delay, until `dispose()`. `ensureConnected()` resolves when the current-or-next attempt opens and rejects with that attempt's `WebSocketClosedError` if it fails; a caller arriving after a failure awaits the upcoming attempt.

Members:

- `connected` — the socket is `OPEN`.
- `send(raw)` — throws unless open.
- `reconnectNow()` — collapses a waiting reconnect's remaining delay and attempts immediately, for a signal that connectivity is likely back (the `online` event, an app resuming from background). A no-op unless a reconnect is actually waiting.
- `drop()` — abandons a socket whose peer is gone without a close frame: fails its in-flight calls and arms the reconnect immediately, instead of waiting out the browser's own CLOSING timeout. Not terminal.
- `dispose()` — closes with code 1000 and stops reconnecting for good.

The pipe is an `Emitter<WebSocketPipeEvents>` from `@greendrake/util`: `on('open')`, `on('close', (error: WebSocketClosedError) => …)`, `on('message', (raw: string) => …)`; `on` returns the unsubscribe function.

### WsTransport

`WsTransportOptions`: `pipe`, `codec?` (see Codec), `auth?: WsAuthStrategy`.

`WsTransport<PEvents, PData>`: `PEvents` types the `{ type: 'event', event, data }` frames (`onEvent`), `PData` the bare `{ type, data }` frames (`onData`). Both default to an empty map, so a push surface has to be declared to subscribe. `PushMap` and `PushMapConstraint<P>` are the constraint types.

- `send(request)` — awaits the connection, writes the frame with a generated `id`, resolves with the correlated response's `data`. Calls in flight when the socket closes reject with a `WebSocketClosedError` flagged `inFlight`.
- `onEvent(event, handler)` / `onData(type, handler)` — return the unsubscribe function.
- `authenticate(token)` — records the credential as this transport's identity and runs `auth.authenticate(send, token)` on the live socket; resolves once the server has accepted it, rejects on a refusal. The identity is re-applied on every reconnect, whether or not an earlier bind succeeded. Repeating it with the credential the live socket is already bound with joins that bind rather than sending a second frame.
- `deauthenticate()` — forgets the identity and runs `auth.deauthenticate(send)`.
- `connect()` — `pipe.ensureConnected()`.
- `connected` — whether calls can currently succeed: an open socket whose identity bind has settled, not merely an open one.
- `onConnectionChange(listener)` — one call per underlying transition, deliberately not deduplicated: consecutive `false` reports are consecutive failed attempts, which is how a consumer tells a blip from an outage. Returns the unsubscribe function.
- `probe(method, deadlineMs = 5000)` — liveness check for a half-open socket (a backgrounded mobile app, an expired NAT mapping): sends `method` and, if nothing at all arrives on the socket before the deadline, drops it and reconnects at once. Resolves `true` when the socket is alive (any answer counts, a refusal included), `false` when it was dropped or was already down.

`WsAuthStrategy` is `{ authenticate(send, token), deauthenticate(send) }`, with `send: WsSend = (payload: RpcRequest) => Promise<unknown>`. The transport owns only the lifecycle; the handshake is the strategy's. `tokenAuth` is the wire's own, over `auth.token` and `auth.logout`; a server answering other methods is given a strategy sending those.

## Live bindings

`liveBinding(transport, options)` binds a view, a store or a cache to the pushes that say it changed, and returns the release function.

```ts
import { liveBinding } from '@greendrake/rpc'

const release = liveBinding(transport, {
    events: {
        // Folded in locally: the push carries what changed.
        'thing.changed': { when: data => data.id === shownId, apply: data => Object.assign(shown, data.thing) },
        // No apply: the push is a signal, so it is answered by re-reading.
        'thing.moderated': { when: data => data.id === shownId }
    },
    resync: async () => {
        shown = await readThing(shownId)
    }
})
```

A list panel binds the same way, with an unfiltered handler and its own re-read:

```ts
const release = liveBinding(transport, {
    events: { 'row.changed': {} },
    resync: () => table.refresh()
})
```

The contract:

- **Relevance.** `when` decides whether a push concerns what the binding shows; omitted, every push does.
- **Apply or resync.** `apply` folds the push in and nothing else happens. Without it the push runs `resync` — so `{}` means "every such push re-reads". A handler with neither `apply` nor a `resync` to fall back on throws at binding time.
- **Reconnect.** `resync` also runs when the socket returns from a drop the binding witnessed: a connection has no memory, so what it missed is only recoverable by asking. The first connect after the binding is created is not a return from a drop. A binding without `resync` subscribes to nothing connection-related.
- **Coalescing.** One `resync` at a time; pushes arriving during a run collapse into a single follow-up after it, however many there are — they ask the same question. This is `@greendrake/util`'s `coalesced`, which a server building what it pushes can use the same way.
- **No error policy.** The binding awaits `resync` only to know when the slot is free, and interprets neither outcome: `resync` owns its errors. It must also not be served by a response cache the missed pushes would have invalidated — invalidate the entries its read would hit, then read.
- **Release.** The returned function detaches every subscription and drops any pending follow-up; calling it twice is harmless.

## State streams

`liveDocument(transport, type, target)` keeps `target` the document the state stream `type` carries, and returns the release function.

```ts
import { liveDocument, type StateFrame } from '@greendrake/rpc'

interface Pushes {
    cameras: StateFrame<Record<string, CameraState>>
}

const cameras = reactive<Record<string, CameraState>>({})
liveDocument(transport, 'cameras', cameras)
```

- **Document and patch.** Each `doc` frame replaces `target` whole: a reconnect's greeting carries the state afresh, removals made while the socket was away included. Each `patch` frame is merged in place, so a reactive target re-renders only what the patch touches.
- **No resync.** The server re-sends the document on every reconnect, which is the re-read a `liveBinding` would otherwise make.
- **Protocol violations throw.** A patch with no document before it on the same socket is one.
- **Typing.** `type` must be a key of the transport's data pushes typed `StateFrame<D>`; `target` is a `D`. `MergePatch<T>` is what a patch over `T` can carry.
- **Any id.** A member named for something every object inherits (`__proto__`, `toString`) lands as the target's own member and reaches no prototype. One named `__proto__` is defined rather than assigned, and Vue does not track a key of that name, so on a reactive target it is held but does not re-render.

A server's half is here too, so that both ends agree on what a document is: `documentOf(type, value)` is a state's wire form (JSON, null members dropped, a JSON object or it throws), and `mergePatchBetween(prev, next)` is the patch from one document to the next, `null` when they are equal. `@greendrake/rpc-server`'s documents are built on them.

## Errors

- `ApiError` — a failure the server answered with: `code` is the response's `error` (and the `message`), `details` its `error_details`.
- `NotFoundError` — extends `ApiError`, for every `*_NOT_FOUND` code. `new NotFoundError()`, an app's own not-found (a route that resolved to nothing), is `NOT_FOUND`.
- `WebSocketClosedError` — extends `@greendrake/util`'s `NetworkError`. Carries `code`, `reason` and `wasClean` from the `CloseEvent`; `inFlight` (whether the failed request was already on the wire — one that never left is safe to re-issue, one in flight may have executed server-side with only its response lost); `isAbnormal` (anything but a clean 1000/1001 close); `asInFlight()`.

Mapping rule, `makeError(code, details?)`, shared by both transports: any code ending in `_NOT_FOUND` becomes a `NotFoundError`, every other code an `ApiError`, each carrying `details`. Transport failures — a `fetch` rejection, a socket closing under a call — are `NetworkError`s, so one `instanceof NetworkError` check classifies all of them.

## Response cache

`ResponseCache` is an in-memory TTL cache keyed by method + args. Only for stateless, platform-static reads — never per-user data that changes within a session.

```ts
import { ResponseCache } from '@greendrake/rpc'

const cache = new ResponseCache({
    ttl: 300, // seconds, for endpoints listed as `true`
    endpoints: {
        'taxonomy.tree': true,
        'search.things': 60 // per-endpoint TTL in seconds
    }
})

cache.get('taxonomy.tree', [{ taxonomy: 'category' }]) // { hit: boolean; data?: unknown }
cache.set('taxonomy.tree', [{ taxonomy: 'category' }], tree) // no-op for an unlisted method
cache.invalidate('search.things') // every entry for the method
cache.invalidate('search.things', { q: 'x' }) // that args-key only
cache.clear() // everything, e.g. when the session identity changes
```

`ttlFor(method)` returns the seconds for a listed method, `false` otherwise. `CacheConfig` and `CacheLookup` are the config and `get` result types. `RpcClient` holds no cache; a client that composes one derives its nonce rule from the same endpoint list.

## Codec

```ts
interface Codec {
    encode(s: string): string
    decode(s: string): string
}
```

Both transports run `encode` over the serialised request and `decode` over the raw response or frame text before parsing. `identityCodec`, the default, passes text through unchanged. Supply a custom codec through the `codec` option of either transport where the wire is obscured or encrypted.
