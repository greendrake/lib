# @greendrake/rpc-server

The server side of an application whose clients call the backend by method name, and need to be told when something changes without asking. One table of methods answers over an HTTP endpoint and over a WebSocket, and the same socket carries what the server pushes: events, and state it keeps current on every client. It serves the wire format that [`@greendrake/rpc`](../rpc), its client, defines. **Runs on [bun](https://bun.sh)** — both transports are written against `Bun.serve`.

```sh
bun add @greendrake/rpc-server
```

## Why

A client and its backend that talk in method calls — `user.get`, `order.cancel` — need the same questions settled for every method: who is calling, whether they may, whether the argument is valid, and what the client is told when the call fails. A client that shows live state also has to be told when that state changes.

The obvious way is HTTP routes for the calls and a WebSocket handler for the pushes. The checks are then written twice, and the copies drift apart. The socket needs more code still: a browser cannot put an `Authorization` header on a WebSocket, and the same-origin policy does not apply to one. This package defines each method once and answers it over both transports, with the same checks and error codes.

## Trade-offs

- **Bun only.** Both transports are written against `Bun.serve`, and there is no adapter for another runtime.
- **Its own wire format.** The format is the one `@greendrake/rpc` defines. It is not JSON-RPC, REST or gRPC, and this package generates no schema document and offers no client for another language. Tools that read HTTP methods, paths and status codes learn little: every call is a `POST` to one URL, and a refused or failed call is still answered `200`, with `success: false` in the body. One call gets one response, so there is no batching and no streamed result. For an API that third parties call with tools of their own, ordinary HTTP routes are the better choice.
- **Pushes stay within one process.** `ConnRegistry` holds its sockets in memory. A push reaches the sockets open on the process that sent it, and no others. A service that runs several replicas has to carry each push to every replica itself. `memoryNonceStore` serves one process only, too.
- **Pushes are not stored.** An event goes to the sockets that are open when it is sent, and is kept for nobody else. A client that stops reading is disconnected. So events suit a client that re-reads after a reconnect, as `@greendrake/rpc`'s `liveBinding` does. State that has to be right after a reconnect belongs in a document: state that the server sends whole to a socket, and then keeps current with patches.
- **An argument's shape is written twice.** It is declared in the contract, and again in the schema that validates it. The compiler checks that the schema produces what the contract declares, so a required field cannot be missing from the schema. It does not check the other way. An optional field that the contract declares and the schema leaves out still compiles, and a schema that drops unknown keys, as zod's objects do, drops that field from every call. Writing the shape twice is what lets a client's type-check read the contract alone, rather than the server's code.

## Overview

- **One dispatch table, two transports.** A method is defined once: what it asks of its caller, the schema of its argument, and its handler. HTTP and the WebSocket both dispatch through that definition, so the checks and the error codes are the same on both.
- **An argument's type comes from its validation.** `args` takes any [Standard Schema](https://standardschema.dev) validator, and the handler's parameter has the schema's output type. A method with no schema takes no argument. So a handler never receives a value that nothing has checked.
- **The application defines identity.** It says how a credential becomes a principal, and what each method asks of one. The principal is a type of the application's own.
- **Abandoned work stops.** Each handler gets a signal that aborts when its caller disconnects or cancels the call.
- **Pushes go to whom they are for.** The registry sends an event to one user's sockets, to every socket, or to the sockets that joined a topic. A topic is a named group of sockets: the application picks the name, and a method joins its caller's socket to it.
- **State, not only events.** A document is state that the server keeps current on the sockets. A socket is sent the whole document first, and a patch on each change after that, in order.
- **One contract types both ends.** The method map that a client is typed by is the contract. `MethodDefs<C, P>` holds the dispatch table to it: each schema has to produce the argument that the contract declares, and each handler has to return the result. A call or a handler that disagrees with the contract does not compile.

A whole service:

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

## Setup

The package ships its TypeScript source as it is: `src/main.ts` is the entry, with no build step and no `.d.ts`. Bun runs it directly, and your type-check reads the source rather than declarations. Extend `@greendrake/dev-config/tsconfig/node.json` with `"types": ["bun"]`, and `strict.json` over it if you like. This package checks itself under both, without the DOM lib. It imports the message types of `@greendrake/rpc`, so that check covers them too, and neither package needs anything from your compiler options.

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

**A method with no `args` schema takes no argument.** That is the rule, not an omission. An argument's type comes from its validation, so a handler cannot be handed a value that nothing has checked. A method that takes a free-form payload declares a permissive schema, which makes that explicit.

`method()` returns its argument unchanged, and exists for type inference. A definition written inline in a table typed `Methods<P>` has nowhere to get its argument type from, because that type erases it. Wrap each entry in `method()`, and the schema's output type reaches the handler's parameter. A table typed by a contract needs no `method()` (see [Typing both ends](#typing-both-ends)).

`HandlerContext` carries `principal`, `locale`, `clientIp` and `signal`. The signal aborts when the caller goes away: an HTTP client disconnects, a WebSocket closes, or the client aborts the call. Pass it to whatever I/O the handler does, so that work nobody is waiting for is abandoned rather than run to completion. The context also carries `topics`, the topics that the calling socket is in (see [Pushing](#pushing)). `topics` is `null` for a call that came over HTTP.

### Argument validation

`args` takes any [Standard Schema](https://standardschema.dev) validator. zod, valibot and arktype all implement it, and this package depends on none of them. A failed validation is answered with `INVALID_ARGUMENT`, and the issues are in `error_details`:

```json
{ "success": false, "error": "INVALID_ARGUMENT", "error_details": [{ "field": "filters.0.from", "message": "Expected number" }] }
```

`field` is the issue's path joined with dots. A form can use it as the key for its per-field messages.

### Authentication

The principal type is the application's own. It is the type `P`: whatever the application identifies its callers by, such as a user id, a session or an operator. An `AuthResolver<P>` turns a credential into a principal, or into `null` where the credential names nobody. `null` means an anonymous caller, not a refusal, since what a method requires of its caller is the method's business. `keyAuthResolver(key, principal)` is the static-key pattern: one pre-shared secret stands for one principal. It suits a dashboard for operators, or a deployment tool. It compares in constant time, and an empty key matches nothing.

What a method asks of its caller is its `MethodAccess<P>`, decided in this order:

1. `auth: 'required'` refuses a caller that no credential named, with `AUTH_REQUIRED`. `'none'` lets anyone through.
2. `access`, where the method has one, is the method's own policy over the caller's principal. The principal is `null` for an anonymous caller of an `auth: 'none'` method. `true` admits the caller, and anything else is the code the call is refused with.

```ts
const adminOnly = (user: User | null) => (user?.admin ? true : 'ADMIN_REQUIRED')

const methods: Methods<User> = {
    'user.ban': method({ auth: 'required', access: adminOnly, args: banArgs, handler: (ctx, args) => users.ban(args.id) })
}
```

A library that ships methods takes their `MethodAccess` from the application rather than deciding it, since only the application knows its principals. A dispatcher with no resolver has no principals, so `createDispatcher` throws on a method that requires one.

### Nonces

Where the dispatcher is given a `NonceStore` (its `nonces` option), a request carrying a `nonce` runs once. Without one, nonces are carried and not checked. `NonceStore.checkAndSet` returns false for a repeat, and the call is refused with `DUPLICATE_REQUEST`.

The nonce is checked once the method, the principal and its access are settled, so a call refused before then can be sent again under the same nonce. The nonce is recorded before the argument is validated and the handler runs. A repeat is then refused, whatever became of the first call. So a nonce stops a repeat, and does not replay the answer: a client whose response was lost learns that the call arrived, not what it returned. A call that failed after its nonce was recorded has to be sent again under a new one.

`memoryNonceStore(ttlMs)` serves one process. Behind a load balancer each replica would keep its own set, so use a shared store there: Redis `SET NX PX` implements the interface in a few lines.

### Faults and addresses

`createDispatcher(methods, options)` also takes:

- `onFault(method, error)` — receives each fault. A fault is anything a method fails with other than an `ApiError`, while its caller is still waiting. `console.error` unless given.
- `clientIp(request, peer)` — how both transports derive `ctx.clientIp`. It is the socket peer's address unless given, because a client can write any address into a header. `trustProxy` takes the rightmost `X-Forwarded-For` entry, which is the address the proxy in front saw. It is for a service that every request reaches through a proxy that appends to that header.
- `locale` — a `LocaleResolver`, which maps the language tag a request carries onto one the application supports.

## Transports

`httpHandler(dispatcher, { codec?, credential? })` returns `(request, server) => Promise<Response>`. It takes POST only (405 otherwise), one call per request. The credential is the `Bearer` token of the `Authorization` header, unless `credential(request)` says otherwise.

`websocketHandler(dispatcher, registry, options)` returns `{ upgrade, handlers }`. `upgrade(request, server)` returns `undefined` once Bun has upgraded the request, which is what a `fetch` handler returns for an upgraded request. Otherwise it returns the refusal: 403 for an origin that is not admitted, 400 for a request that is not a WebSocket handshake. Calls run concurrently, and the `id` that the client puts on each frame pairs each response with its request. Options:

- `origins` — the browser origins admitted besides the endpoint's own, which is always admitted. It is `@greendrake/util`'s `OriginPolicy`: `'*'`, a list, or a predicate. A browser page anywhere can open a socket to any host. Without this check, any page a user visits could call the service as that user. A client that sends no `Origin` header is not a browser, and is not checked.
- `tokenParam` — the query parameter that carries the handshake credential, `token` unless given. A query parameter is the only place where a browser's WebSocket constructor lets a page put a credential, and a reconnect sends it again.
- `authFrames` — `{ token, logout }`, the methods that bind the socket to a token's principal and unbind it. They are the protocol's `auth.token` and `auth.logout` unless given. These methods change who every later call on that socket is made by. So on each connection they run one at a time, and a call waits for those sent before it. Otherwise a call sent right after a bind would race it and come back `AUTH_REQUIRED`.
- `onOpen(conn)`, `onClose(conn)`.

A frame carrying `abort: <nonce>` cancels the request that the nonce names, on the same socket. That request's handler has its `signal` aborted, and its response is never sent.

No push is dropped silently. Bun drops a frame once a socket's send buffer passes its `backpressureLimit`. `handlers` sets `closeOnBackpressureLimit`, so such a socket is closed instead. Otherwise a client that has stopped reading would hold wrong state without knowing it. Once it reconnects, its re-read (`liveBinding`) or the documents it is sent as it connects put it right.

A codec applies to every message, in both directions. It is for a service whose clients obscure or encrypt their payloads. Pass it as `new ConnRegistry({ codec })` for the socket, and as `httpHandler(dispatcher, { codec })` for the endpoint.

## Pushing

`ConnRegistry<P>` holds every open socket, and indexes the sockets by principal and by [topic](#topics). A primitive principal is its own key. An object principal is a new object for every socket its credential is resolved on. So the registry has to be given the key that the sockets of one principal share: `new ConnRegistry<User>({ userKey: user => user.id })`. The type requires `userKey` for an object principal.

```ts
registry.emitToUser(userKey, 'thread.updated', { id }) // {type:'event', event, data} to that user's sockets
registry.broadcastEvent('job.progress', progress) // …to every socket
registry.broadcastData('telemetry', reading) // the bare {type, data} form
```

Code that produces pushes takes the `EventSink` interface (`emitToUser` + `broadcastEvent`) rather than the class, so it works with anything that implements the interface. On the client, `@greendrake/rpc`'s `liveBinding` subscribes to these pushes. It applies each push or re-reads, and re-reads again after a dropped socket reconnects.

Access is checked on calls, not on pushes. A socket may open without a credential, as an anonymous caller. A broadcast, and a document that goes to every socket, reach every open socket, anonymous ones included. A topic's pushes go to its members whichever principal they are bound to, and a socket stays in its topics through `auth.logout`. So put nothing that some client may not see in a broadcast, or in a document that goes to every socket. Check access to a topic in the method that joins it.

### Documents

State that the whole service shares, and that is kept current on every socket, is a document rather than a stream of events:

```ts
const cameras = registry.document('cameras', () => fleet.states()) // { gate: {…}, porch: {…} }

fleet.onChange(() => cameras.refresh())
```

- **The whole document.** `document(type, produce)` produces the state at once. Every socket that opens is sent that state whole: `{type, data: {doc}}`.
- **Patches.** Each `refresh()` produces the state again. Every socket is sent the RFC 7386 merge patch from the last state to this one: `{type, data: {patch}}`. Objects merge, `null` removes a member, and anything else replaces, so collections are objects keyed by id. Nothing is sent when nothing changed, so a producer calls `refresh` on any change it knows of.
- **Shape.** The state must be a JSON object, or `document` throws. A member produced as `null` is dropped, the same as a member left out, since a patch could not tell the two apart.
- **Ordering.** Sends are synchronous, so no patch can precede or overtake a socket's document.

State that only some sockets want, the ones that asked for it, is a document sent to the members of a [topic](#topics) alone:

```ts
const unit = registry.document('unit/web', () => ({ status }), { topic: 'unit/web' })
```

- **The whole document.** A socket is sent the whole document as it joins the topic. The send is synchronous, inside `join`, so the document goes out before the joining call's response. A socket already in the topic is not sent it again.
- **Patches** go to the topic's members alone. A socket that leaves or closes is sent nothing further. A reconnected socket is in no topic, so its client joins again.
- **Audience.** The handle is also the topic's `Audience`. `size` is the number of its members. `onPresence` calls its listener as the first member joins, and as the last one leaves or closes. It is for a producer whose state costs something to produce, and is worth producing only while a member is there to receive it.

Code that produces documents takes the `DocumentPublisher` interface (`document`) rather than the class, as code that produces pushes takes `EventSink`.

On the client, `@greendrake/rpc`'s `liveDocument` keeps an object equal to the document. A reconnected socket is sent again every document that goes to every socket, so no re-read is needed. A topic's document is sent again once the reconnected socket joins the topic again. The client has to do that joining.

### Topics

Some state is wanted only by the sockets that asked for it: one record's changes, say, followed by whoever has the record open. For that, a socket joins a topic, and the application publishes to the topic:

```ts
const methods = {
    'thing.watch': method({
        auth: 'required',
        args: z.object({ id: z.string() }),
        handler: (ctx, { id }) => {
            topicsOf(ctx).join(`thing:${id}`)
            return things.get(id)
        }
    }),
    'thing.unwatch': method({
        auth: 'required',
        args: z.object({ id: z.string() }),
        handler: (ctx, { id }) => topicsOf(ctx).leave(`thing:${id}`)
    })
}

registry.publish(`thing:${id}`, 'thing.changed', thing) // {type:'event', event, data} to every socket in the topic
registry.topics() // the topics with at least one socket in them
```

A handler joins and leaves for its own caller, through `ctx.topics`, because only the transport knows which socket a call came in on. `topicsOf(ctx)` returns `ctx.topics`. For a call that came over HTTP, where there is no connection to push to, it throws `WEBSOCKET_REQUIRED` instead. A socket is in a topic until it leaves the topic or closes. A handler that joins after its caller has gone joins nothing. A topic is any string the application chooses. A reconnecting client is a new socket, and has to join again. The natural place to do that is `liveBinding`'s re-read on reconnect.

The registry is also an `Audience`. `size` is how many sockets are open. `onPresence(listener)` calls the listener with `true` as the first socket opens and with `false` as the last one closes, and returns the function that stops those calls. It is for a producer whose pushes cost something to produce, such as a reading taken from somewhere slow, and are worth producing only while somebody is connected to receive them.

## Error codes

Errors are codes, not sentences, so that client code can compare them. A handler reports an outcome of its own with `throw new ApiError(code, details?)`. Anything else it throws is a fault, which is reported to `onFault` and answered `INTERNAL_ERROR`. A refusal by `access` is answered with the code `access` returned.

`@greendrake/rpc`'s README lists the codes that the protocol itself answers with. Here, the details of `INVALID_ARGUMENT` are one `{ field, message }` per schema issue. A call over HTTP to a method that subscribes its caller (`topicsOf`) is refused with `WEBSOCKET_REQUIRED`. This server sets no body limit of its own, so it never answers `REQUEST_TOO_LARGE`: Bun's `maxRequestBodySize` limits a body.

A code ending `_NOT_FOUND` becomes a `NotFoundError` on the client. It is the only kind of code that `@greendrake/rpc` maps to an error class of its own.

## Typing both ends

A client of `@greendrake/rpc` is typed by a method map: an interface from method name to call signature. That interface is the contract between the server and its clients. Declare it in a module of its own, which imports nothing from either side:

```ts
// contract.ts
export interface ApiMethods {
    'user.get': (args: { id: string }) => User
    'user.list': () => User[]
}
```

`MethodDefs<C, P>` is the dispatch table that serves contract `C`, for principal `P`:

- Every method that the contract declares has to be defined.
- A method declared with a parameter has a schema, and the schema has to produce that argument. The handler receives it.
- A method declared with no parameter takes no argument, and has no schema.
- The handler has to return the contract's result, or a promise of it.
- A method takes one argument at most, and the argument is required. A method declared with more than one parameter, or with an optional or rest parameter, cannot be defined. An optional value goes in a field of the argument instead: `(args: { filter?: string }) => User[]`.

```ts
import type { ApiMethods } from './contract'

const methods: MethodDefs<ApiMethods, Operator> = {
    'user.get': { auth: 'none', args: z.object({ id: z.string() }), handler: (ctx, { id }) => users.get(id, ctx.signal) },
    'user.list': { auth: 'required', handler: () => users.all() }
}
```

The client is `new RpcClient<ApiMethods>(transport)`. Each entry of the table takes its argument type from the contract, so it needs no `method()`. A table typed by its contract is still a `Methods<P>`, which `createDispatcher` takes.

The contract describes an argument as the handler receives it, which is what the schema produces. Where a schema fills in a default, the contract still declares the field, and a typed client sends it.

The contract is a module of its own so that a client's type-check reads the contract and nothing else. A client typed from the server's table instead would compile everything that the handlers import: the server's storage, its other services, and their SDKs. `examples/service-pair` is a working server and client built this way.
