# @greendrake/rpc-server

The server half of the [`@greendrake/rpc`](../rpc) wire format: one dispatch table, an HTTP endpoint and a WebSocket endpoint over it, per-method auth and argument validation, and a connection registry that pushes to clients unasked. **Runs on [bun](https://bun.sh)** — `Bun.serve` owns the sockets. The protocol core (dispatch, auth, nonce, validation) is pure functions over the wire types and touches no runtime API.

## Install

```sh
bun add @greendrake/rpc-server
```

TypeScript source ships as-is (`src/main.ts` is the entry; no build step, no `.d.ts`). Extend `@greendrake/dev-config/tsconfig/node.json` with `"types": ["bun"]`, and `strict.json` over it if you like: this package checks itself under both, without the DOM lib, so neither it nor `@greendrake/rpc` — the client this serves, whose wire types it imports — asks anything of your options.

## A service, whole

```ts
import { ConnRegistry, clientIp, createDispatcher, httpHandler, keyAuthResolver, memoryNonceStore, method, websocketHandler } from '@greendrake/rpc-server'
import { z } from 'zod'

const registry = new ConnRegistry()

const methods = {
    'user.get': method({
        auth: 'user',
        args: z.object({ id: z.string() }),
        handler: (ctx, args) => users.get(args.id, ctx.signal)
    }),
    'user.list': method({ auth: 'admin', handler: () => users.all() })
}

const dispatcher = createDispatcher(methods, {
    auth: keyAuthResolver(process.env.ADMIN_API_KEY!, { userId: 'operator', isAdmin: true, phantom: false }),
    nonces: memoryNonceStore(5 * 60 * 1000)
})
const api = httpHandler(dispatcher)
const ws = websocketHandler(dispatcher, registry)

Bun.serve({
    port: 8080,
    websocket: ws.handlers,
    async fetch(request, server) {
        const { pathname } = new URL(request.url)
        if (pathname === '/v1/ws') return (await ws.upgrade(request, server)) ? undefined : new Response(null, { status: 400 })
        if (pathname === '/v1/api') return api(request, clientIp(request, server))
        return new Response(null, { status: 404 })
    }
})
```

## Methods

A `MethodDef` is what a method requires of its caller, what its argument looks like, and what runs:

```ts
interface MethodDef<A = void, R = unknown> {
    auth: 'none' | 'user' | 'admin'
    allowPhantom?: boolean
    args?: StandardSchemaV1<unknown, A>
    handler(ctx: HandlerContext, args: A): R | Promise<R>
}
```

**A method with no `args` schema takes no argument.** That is the rule, not an omission: validation is where an argument's type comes from, so a handler cannot be handed a value nothing has checked. A method taking a free-form payload declares a permissive schema and says so.

`method()` is identity at runtime and exists for inference — written inline in a table, a definition has nowhere to get its argument type from, because the table's own type erases it. Wrap each entry in it and the schema's output type reaches the handler's parameter.

`HandlerContext` carries `auth`, `locale`, `clientIp` and a `signal` that aborts when the caller goes away — an HTTP client disconnecting, a WebSocket closing. Pass it to whatever I/O the handler does, so work nobody is waiting for is abandoned rather than run to completion.

### Argument validation

`args` takes any [Standard Schema](https://standardschema.dev): zod, valibot and arktype all implement it, and this package depends on none of them. A failure answers `INVALID_ARGUMENT` with the issues in `error_details`:

```json
{ "success": false, "error": "INVALID_ARGUMENT", "error_details": [{ "field": "filters.0.from", "message": "Expected number" }] }
```

`field` is the issue's path joined with dots, which is what a form keys its per-field messages by.

### Authentication

`AuthResolver` turns a credential into an `AuthInfo { userId, isAdmin, phantom }`, or `null` where it names nobody — a stranger, not a refusal, since what a method requires of a principal is the dispatcher's business. `userId` is `unknown`: it is the application's own id type, and nothing here reads it.

`keyAuthResolver(key, info)` is the static-key pattern — one pre-shared secret standing for one identity, for a dashboard behind an operator gate or a deployment tool. An empty key matches nothing.

The auth rules, in the order a refusal is decided:

| `auth` | Refuses with |
| --- | --- |
| `'none'` | nothing |
| `'user'` | `AUTH_REQUIRED` for an anonymous caller |
| `'admin'` | `AUTH_REQUIRED`, then `ADMIN_REQUIRED` |

A phantom — a provisional principal the application has not fully registered — is additionally refused with `PHANTOM_NOT_ALLOWED` unless the method sets `allowPhantom`.

### Nonces

A request carrying a `nonce` runs once: `NonceStore.checkAndSet` answers false for a repeat, and the call is refused with `DUPLICATE_REQUEST`. `memoryNonceStore(ttlMs)` is one process's worth — behind a load balancer the replicas would each keep their own set, so use a shared store (Redis `SET NX PX` implements the interface in a few lines).

## Transports

`httpHandler(dispatcher, { codec? })` returns `(request, clientIp) => Promise<Response>`: POST only (405 otherwise), the credential from the `Authorization` header (`Bearer <token>` or a bare pre-shared key), and one call per request. `clientIp(request, server)` reads `X-Forwarded-For`, then `X-Real-Ip`, then the socket.

`websocketHandler(dispatcher, registry, { onOpen?, onClose? })` returns `{ upgrade, handlers }`. The handshake credential is the `token` query parameter — the only channel a browser's WebSocket constructor leaves open, having no headers, and one a reconnect carries again for free. Calls dispatch concurrently, correlated by the `id` the client puts on each frame.

The exception is `auth.token` and `auth.logout`, the in-band frames `@greendrake/rpc`'s `WsTransport` sends: they change who every later call on that socket is made by, so each goes on a per-connection gate the calls wait behind. Without it a call sent right after a bind would race it and come back `UNAUTHENTICATED`.

A codec applies to everything on the wire, in both directions, for a service whose clients obscure or encrypt their payloads: `new ConnRegistry(codec)` for the socket, `httpHandler(dispatcher, { codec })` for the endpoint.

## Pushing

`ConnRegistry` holds every open socket and indexes them by principal:

```ts
registry.emitToUser(userId, 'thread.updated', { id })  // {type:'event', event, data} to that user's sockets
registry.broadcastEvent('job.progress', progress) // …to every socket
registry.broadcastData('telemetry', reading) // the bare {type, data} form
```

Push-producing code takes the `EventSink` interface (`emitToUser` + `broadcastEvent`) rather than the class, so it can be driven by anything. On the client, `@greendrake/rpc`'s `liveBinding` subscribes to these, folds each push in or re-reads, and re-reads again after a dropped socket returns.

## Error codes

Codes, not sentences: the client keys on them. A handler's own outcomes are `throw new ApiError(code, details?)`; anything else it throws is a fault, logged and answered `INTERNAL_ERROR`.

| Code | Meaning |
| --- | --- |
| `UNKNOWN_METHOD` | no such method in the table |
| `INVALID_JSON` | the body or frame did not parse |
| `INVALID_ARGUMENT` | the argument failed the method's schema |
| `AUTH_REQUIRED` | the method needs a caller and there is none |
| `ADMIN_REQUIRED` | the caller is not an admin |
| `PHANTOM_NOT_ALLOWED` | the caller is provisional and this method is not open to one |
| `UNAUTHENTICATED` | an `auth.token` frame named nobody |
| `AUTH_NOT_CONFIGURED` | an auth frame reached a service with no resolver |
| `DUPLICATE_REQUEST` | this nonce has already been used |
| `INTERNAL_ERROR` | a handler faulted |

By convention a code ending `_NOT_FOUND` becomes a `NotFoundError` client-side, which is the one class `@greendrake/rpc` maps specially. Nothing here enforces it.

## The pair's typing

`ClientMethods<M>` turns a dispatch table into the method map a client hands `RpcClient`, or any typed client built on it:

```ts
export const methods = { /* … */ }
export type ApiMethods = ClientMethods<typeof methods>
```

Declared once, on the side that defines the methods: a method whose argument shape or result changes stops compiling in whatever calls it. `examples/service-pair` is a working pair built this way.
