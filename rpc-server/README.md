# @greendrake/rpc-server

The server half of the [`@greendrake/rpc`](../rpc) wire format: one dispatch table, an HTTP endpoint and a WebSocket endpoint over it, per-method auth and argument validation, and a connection registry that pushes to clients unasked — events, and the state documents every socket keeps. **Runs on [bun](https://bun.sh)** — `Bun.serve` owns the sockets. The protocol core (dispatch, auth, nonce, validation) is pure functions over the wire types and touches no runtime API.

## Install

```sh
bun add @greendrake/rpc-server
```

TypeScript source ships as-is (`src/main.ts` is the entry; no build step, no `.d.ts`). Extend `@greendrake/dev-config/tsconfig/node.json` with `"types": ["bun"]`, and `strict.json` over it if you like: this package checks itself under both, without the DOM lib, so neither it nor `@greendrake/rpc` — the client this serves, whose wire types it imports — asks anything of your options.

## A service, whole

```ts
import { ConnRegistry, createDispatcher, httpHandler, keyAuthResolver, memoryNonceStore, method, websocketHandler, type Methods } from '@greendrake/rpc-server'
import { z } from 'zod'

// The application's principal: here the one operator a static key names.
type Operator = 'operator'

const registry = new ConnRegistry<Operator>()

const methods: Methods<Operator> = {
    'user.get': method({
        auth: 'none',
        args: z.object({ id: z.string() }),
        handler: (ctx, args) => users.get(args.id, ctx.signal)
    }),
    'user.list': method({ auth: 'required', handler: () => users.all() })
}

const dispatcher = createDispatcher(methods, {
    auth: keyAuthResolver<Operator>(process.env.ADMIN_API_KEY!, 'operator'),
    nonces: memoryNonceStore(5 * 60 * 1000)
})
const api = httpHandler(dispatcher)
const ws = websocketHandler(dispatcher, registry, { origins: ['https://dash.example.com'] })

Bun.serve({
    port: 8080,
    websocket: ws.handlers,
    fetch(request, server) {
        const { pathname } = new URL(request.url)
        if (pathname === '/v1/ws') return ws.upgrade(request, server)
        if (pathname === '/v1/api') return api(request, server)
        return new Response(null, { status: 404 })
    }
})
```

## Methods

A `MethodDef` is what a method requires of its caller, what its argument looks like, and what runs:

```ts
interface MethodDef<A = void, R = unknown, P = never> {
    auth: 'none' | 'required'
    access?(principal: P | null): true | string
    args?: StandardSchemaV1<unknown, A>
    handler(ctx: HandlerContext<P>, args: A): R | Promise<R>
}
```

**A method with no `args` schema takes no argument.** That is the rule, not an omission: validation is where an argument's type comes from, so a handler cannot be handed a value nothing has checked. A method taking a free-form payload declares a permissive schema and says so.

`method()` is identity at runtime and exists for inference — written inline in a table, a definition has nowhere to get its argument type from, because the table's own type erases it. Wrap each entry in it and the schema's output type reaches the handler's parameter.

`HandlerContext` carries `principal`, `locale`, `clientIp` and a `signal` that aborts when the caller goes away — an HTTP client disconnecting, a WebSocket closing, an abort naming the call. Pass it to whatever I/O the handler does, so work nobody is waiting for is abandoned rather than run to completion. It also carries `topics`, the calling socket's subscriptions (see [Pushing](#pushing)), which is `null` for a call that came over HTTP.

### Argument validation

`args` takes any [Standard Schema](https://standardschema.dev): zod, valibot and arktype all implement it, and this package depends on none of them. A failure answers `INVALID_ARGUMENT` with the issues in `error_details`:

```json
{ "success": false, "error": "INVALID_ARGUMENT", "error_details": [{ "field": "filters.0.from", "message": "Expected number" }] }
```

`field` is the issue's path joined with dots, which is what a form keys its per-field messages by.

### Authentication

The principal is the application's: `P`, whatever it names its callers by — a user id, a session, an operator. An `AuthResolver<P>` turns a credential into one, or `null` where it names nobody: a stranger, not a refusal, since what a method requires of its caller is the method's business. `keyAuthResolver(key, principal)` is the static-key pattern — one pre-shared secret standing for one principal, for a dashboard behind an operator gate or a deployment tool. It compares in constant time, and an empty key matches nothing.

What a method asks of its caller is its `MethodAccess<P>`, decided in this order:

1. `auth: 'required'` refuses a caller no credential named with `AUTH_REQUIRED`. `'none'` lets anyone through.
2. `access`, where the method has one, is its own policy over the caller's principal (`null` for an anonymous caller of an `auth: 'none'` method): `true` admits, and anything else is the code the call is refused with.

```ts
const adminOnly = (user: User | null) => (user?.admin ? true : 'ADMIN_REQUIRED')

const methods: Methods<User> = {
    'user.ban': method({ auth: 'required', access: adminOnly, args: banArgs, handler: (ctx, args) => users.ban(args.id) })
}
```

A library shipping methods takes their `MethodAccess` from the application rather than deciding it, since only the application knows its principals. A dispatcher with no resolver has no principals, and `createDispatcher` throws on a method that requires one.

### Nonces

A request carrying a `nonce` runs once: `NonceStore.checkAndSet` answers false for a repeat, and the call is refused with `DUPLICATE_REQUEST`. The nonce is checked once the method, the principal and its access are settled, so a call refused before then leaves it unspent. `memoryNonceStore(ttlMs)` is one process's worth — behind a load balancer the replicas would each keep their own set, so use a shared store (Redis `SET NX PX` implements the interface in a few lines).

### Faults and addresses

`createDispatcher(methods, options)` also takes:

- `onFault(method, error)` — where a fault goes: anything a method fails with but an `ApiError`, while its caller is still there. `console.error` unless given.
- `clientIp(request, peer)` — how both transports derive `ctx.clientIp`. The socket peer's address unless given: a header naming another is the client's to write. `trustProxy` takes the rightmost `X-Forwarded-For` entry, the address the proxy in front saw, for a service every request reaches through one that appends to it.
- `locale` — a `LocaleResolver` mapping the envelope's tag onto one the application ships.

## Transports

`httpHandler(dispatcher, { codec?, credential? })` returns `(request, server) => Promise<Response>`: POST only (405 otherwise), one call per request. The credential is the `Bearer` token of the `Authorization` header unless `credential(request)` says otherwise.

`websocketHandler(dispatcher, registry, options)` returns `{ upgrade, handlers }`. `upgrade(request, server)` answers `undefined` once Bun has the socket, which is what a `fetch` returns for an upgraded request, and otherwise the refusal: 403 for an origin not admitted, 400 for a request that is no handshake. Calls dispatch concurrently, correlated by the `id` the client puts on each frame. Options:

- `origins` — the browser origins admitted beside the endpoint's own, which always is (`@greendrake/util`'s `OriginPolicy`: `'*'`, a list, or a predicate). A browser page anywhere can open a socket to any host, so without this check any page its user visits could drive the service as them. A client sending no `Origin` is no browser, and is not asked.
- `tokenParam` — the handshake credential's query parameter, `token` unless given: the only channel a browser's WebSocket constructor leaves open, and one a reconnect carries again for free.
- `authFrames` — `{ token, logout }`, the methods binding the socket to a token's principal and unbinding it; the wire's `auth.token` and `auth.logout` unless given. They change who every later call on that socket is made by, so each goes on a per-connection gate the calls wait behind. Without it a call sent right after a bind would race it and come back `AUTH_REQUIRED`.
- `onOpen(conn)`, `onClose(conn)`.

A frame carrying `abort: <nonce>` cancels the request on the same socket that nonce names: its handler's `signal` aborts and its response is never sent.

Nothing pushed is ever dropped. Bun drops a frame once a socket's send buffer passes its `backpressureLimit`; `handlers` sets `closeOnBackpressureLimit`, so such a socket is closed instead. A client that has stopped reading would otherwise be left silently wrong. Once it reconnects, its re-read (`liveBinding`) or the documents it is greeted with put it right.

A codec applies to everything on the wire, in both directions, for a service whose clients obscure or encrypt their payloads: `new ConnRegistry({ codec })` for the socket, `httpHandler(dispatcher, { codec })` for the endpoint.

## Pushing

`ConnRegistry<P>` holds every open socket and indexes them by principal and by [topic](#topics). A primitive principal is its own key; an object one, a fresh object for every socket its credential is resolved on, names the key its sockets share — `new ConnRegistry<User>({ userKey: user => user.id })` — and the type insists.

```ts
registry.emitToUser(userKey, 'thread.updated', { id })  // {type:'event', event, data} to that user's sockets
registry.broadcastEvent('job.progress', progress) // …to every socket
registry.broadcastData('telemetry', reading) // the bare {type, data} form
```

Push-producing code takes the `EventSink` interface (`emitToUser` + `broadcastEvent`) rather than the class, so it can be driven by anything. On the client, `@greendrake/rpc`'s `liveBinding` subscribes to these, folds each push in or re-reads, and re-reads again after a dropped socket returns.

### Documents

State the whole service shares, kept current on every socket, is a document rather than a stream of events:

```ts
const cameras = registry.document('cameras', () => fleet.states()) // { gate: {…}, porch: {…} }

fleet.onChange(() => cameras.refresh())
```

- **Greeting.** `document(type, produce)` produces the state now, and every socket that opens is sent it whole: `{type, data: {doc}}`.
- **Patches.** Each `refresh()` produces the state again and sends every socket the RFC 7386 merge patch from the last one: `{type, data: {patch}}`. Objects merge, `null` removes a member, and anything else replaces, so collections are objects keyed by id. Nothing is sent when nothing changed, so a producer calls `refresh` on any change it knows of.
- **Shape.** The state must be a JSON object, or `document` throws. A member produced as `null` is dropped, the same as one left out, since a patch could not tell the two apart.
- **Ordering.** Sends are synchronous, so no patch can precede or overtake a socket's document.

Document-producing code takes the `DocumentPublisher` interface (`document`) rather than the class, as push-producing code takes `EventSink`.

On the client, `@greendrake/rpc`'s `liveDocument` keeps the document. A reconnect is greeted with it afresh, so no re-read is needed.

### Topics

For state only the sockets that asked for it want — one record's changes, followed by whoever has it open — a socket joins a topic, and the application publishes to the topic:

```ts
const methods = {
    'thing.watch': method({
        auth: 'user',
        args: z.object({ id: z.string() }),
        handler: (ctx, { id }) => {
            topicsOf(ctx).join(`thing:${id}`)
            return things.get(id)
        }
    }),
    'thing.unwatch': method({
        auth: 'user',
        args: z.object({ id: z.string() }),
        handler: (ctx, { id }) => topicsOf(ctx).leave(`thing:${id}`)
    })
}

registry.publish(`thing:${id}`, 'thing.changed', thing) // {type:'event', event, data} to every socket in the topic
registry.topics() // the topics with at least one socket in them
```

A handler joins and leaves for its own caller, through `ctx.topics`; only the transport knows which socket a call came in on. `topicsOf(ctx)` is that, or `WEBSOCKET_REQUIRED` for a call that came over HTTP, where there is no connection to push to. A socket is in a topic until it leaves it or closes, and a handler that joins after its caller has gone joins nothing. A topic is whatever string the application makes it; a reconnecting client is a new socket and joins again, which `liveBinding`'s re-read on reconnect is the natural place to do.

The registry is also an `Audience`: `size` is how many sockets are open, and `onPresence(listener)` calls the listener with `true` as the first one opens and `false` as the last one closes, returning the function that stops it. It is for a producer whose pushes cost something to produce — a reading taken from somewhere slow — and are worth producing only while somebody is connected to be told.

## Error codes

Codes, not sentences: the client keys on them. A handler's own outcomes are `throw new ApiError(code, details?)`; anything else it throws is a fault, reported to `onFault` and answered `INTERNAL_ERROR`. An `access` refusal answers with the code it returned.

The codes the protocol answers with itself are specified by `@greendrake/rpc`'s wire section. `INVALID_ARGUMENT`'s details here are `{ field, message }` per schema issue. `WEBSOCKET_REQUIRED` answers a method that subscribes its caller (`topicsOf`) called over HTTP. This server sets no body limit of its own, so `REQUEST_TOO_LARGE` is not among its answers: Bun's `maxRequestBodySize` bounds a body.

A code ending `_NOT_FOUND` becomes a `NotFoundError` client-side, the one class `@greendrake/rpc` maps specially.

## The pair's typing

`ClientMethods<M>` turns a dispatch table into the method map a client hands `RpcClient`, or any typed client built on it:

```ts
export const methods = { /* … */ }
export type ApiMethods = ClientMethods<typeof methods>
```

Declared once, on the side that defines the methods: a method whose argument shape or result changes stops compiling in whatever calls it. `examples/service-pair` is a working pair built this way.
