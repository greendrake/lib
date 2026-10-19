# greendrake.nz/go/rpcserver

The server end of the `@greendrake/rpc` wire format (its README defines the
envelopes, error codes and WebSocket framing; it is not JSON-RPC): a method
table with per-method authentication, served over one HTTP POST endpoint and
over WebSocket with server push.

```sh
go get greendrake.nz/go/rpcserver
```

## Dispatch

`NewDispatcher(methods, opts...)` over a `map[string]MethodDef` — each a
`HandlerFunc(hc, args) (result, error)` with its `Auth` (`AuthNone`,
`AuthRequired`), its `Access` (what it asks of its principal, in the terms of
the host's gate) and `AllowPlain` (accepted as plain JSON where requests must
be encoded). Libraries ship their methods as such maps; the host merges them.
A request runs:

1. the locale its envelope asks for, through the host's `LocaleResolver`
   (`WithLocaleResolver`; handlers read `LocaleFrom(ctx)`), and the
   dispatcher's `Validator` (`WithValidator`; `ValidatorFrom(ctx)`);
2. the method lookup — `UNKNOWN_METHOD`;
3. a principal, for a method requiring one — `AUTH_REQUIRED`. The transport
   resolves it from the request's bearer token (`WithAuthResolver`: an
   `AuthResolver` turns a token into `AuthInfo`, whose `Claims` are the host's;
   `KeyAuthResolver` is the pre-shared-key one, compared in constant time);
4. the host's `AuthGate` (`WithAuthGate`), for every principal of a method
   requiring one: the one authorisation hook, which reads the method's
   `Access` and may put what it loaded on the context. The package holds no
   policy of its own. A method with an `Access` and no principal to ask it of,
   or no gate to enforce it, is refused by `NewDispatcher`;
5. nonce dedup — a nonce seen before is `DUPLICATE_REQUEST`
   (`WithNonceStore`; `NewMemoryNonceStore` keeps nonces in process, for a
   server that runs as one; one that runs as several supplies a shared
   `NonceStore`). A request refused before this leaves its nonce unspent;
6. the handler. An `*APIError` (`NewAPIError`, `NewAPIErrorWithDetails`) is
   answered by its code; anything else is a fault, logged and
   `INTERNAL_ERROR`. `Answer(method, err, []ErrorCode)` maps a handler's
   sentinels to codes.

`GetAuth(ctx)` is the principal; `UserID[T](hc)` its id as the host's type.
`hc.ClientIP` is the caller's address: the socket peer's, unless the host
derives it from a proxy it trusts (`WithClientIP`). `WithLogger` is where
faults go, `slog.Default()` unless given.

The codes the protocol answers with itself are the `Code*` constants; the
WebSocket's reserved methods and token parameter are `MethodAuthToken`,
`MethodAuthLogout` and `TokenParam`.

## Arguments: `rpcserver/rpcargs`

`ParseAndValidate[T]` (a struct, `go-playground/validator` tags),
`ParseAndValidateVar[T]` (one primitive) and `ParsePositional` parse a method's
arguments; arguments that do not parse or validate are `INVALID_ARGUMENT`,
with a `FieldError` per failed rule. A request without arguments — what a
client sends for a call it gives none — has none: the targets keep their zero
values, which `ParseAndValidate` validates like any other.

The validator is the request's dispatcher's: `rpcargs.NewValidator(rules...)` is
go-playground's, with the built-in `trimmed_nonempty` tag and the host's
`Rule`s, each living with the code it validates. Code parsing arguments
outside a dispatcher (a test) puts one on its context with
`ContextWithValidator`. `OneOf` refuses a value outside a set known only at run
time; `Maybe[T]` tells a field absent from one sent as null.

## Transports

`NewHTTPHandler(d, WithHTTPEncoding(e), WithHTTPBodyLimit(n))` and
`NewWSHandler(d, registry, WithWSEncoding(e), WithWSOriginPatterns(patterns...),
WithWSOnOpen(fn), WithWSOnClose(fn), WithWSReadLimit(n))`.

- A credential is a `Bearer` token: the `Authorization` header over HTTP; over
  WebSocket the `token` query parameter or an `auth.token` frame.
- An HTTP body over the limit (`DefaultHTTPBodyLimit`, 1 MiB, unless given) is
  `REQUEST_TOO_LARGE`. A WebSocket message over the read limit (the WebSocket
  library's 32 KiB unless given) closes the socket.
- A browser's socket is admitted from the endpoint's own origin and from the
  origins `WithWSOriginPatterns` lists (`https://app.example.com`,
  `app://localhost`; a pattern without a scheme matches the host alone).
  Any other is refused at the handshake. HTTP's origin policy is CORS, the
  host's.
- Without an `Encoding` the wire carries plain JSON. An `Encoding` puts a
  `Codec` on it — the server end of `@greendrake/rpc`'s, for a wire that is
  obscured or encrypted: a request that is structurally JSON is taken as it is
  and any other is decoded; `EncodeResponse` encodes every response, and
  `RequireEncodedRequest` refuses a plain request as a malformed one. A
  principal marked `PlainTransport` is exempt both ways.

A `ConnRegistry` holds each user's live connections and is an `Emitter`:
`Emit(ctx, userID, event, data)` pushes `{type: "event", event, data}` to them.

Nothing pushed is ever dropped. A socket whose 64-frame send buffer fills has a client
that has stopped keeping up, and a frame lost would leave that client silently wrong.
So the socket is closed instead, at once and without a close handshake. The client
reconnects, and its re-read on reconnect (`@greendrake/rpc`'s `liveBinding`) or the
documents it is greeted with put it right. A push that does not marshal is its
producer's bug, and panics.

## Broadcast

`Broadcast` is the endpoint of an app without auth: one WebSocket, for every live
socket alike, carrying the dispatcher's methods and everything the app pushes.
`NewBroadcast().Handler(dispatcher, opts...)` serves it. `opts` are the transport's;
the open and close hooks are the Broadcast's own.

- `Document(type, produce)` is a state stream: the whole of some state as one JSON
  object, kept by every socket.
  - An opening socket is sent the document as last published, `{type, data: {doc}}`.
  - Each change after that goes as an RFC 7386 merge patch over it,
    `{type, data: {patch}}`. Objects merge, `null` removes a member, and anything else
    replaces, so collections are objects keyed by id.
  - A member produced as `null` is dropped, the same as one left out, since a patch
    cannot tell the two apart.
  - `produce` runs once when the document is made, so every greeting has a document.
    It runs again on each `Refresh`, which pushes what changed since the document was
    last published, and nothing when nothing did. A producer calls `Refresh` on any
    change it knows of.
  - Greeting and publishing are serialised, so no patch precedes a socket's document
    or overtakes it.
  - `produce` runs under the document's lock. A producer must therefore not hold a
    lock `produce` takes while calling `Refresh`.
  - A state that is not a JSON object panics, as the producer's bug.
- `Push(type, data)` sends a signal to every live socket as a bare `{type, data}`
  frame: word that something happened, which the client answers by re-reading.

## `rpcservertest`

A client of a WebSocket endpoint, for tests, which reads it as a browser does.

- `Dial(t, url)` opens a socket on an httptest server's `http://` URL, query and all, and closes it when the test ends.
- `Read(t, conn, &v)` decodes the next message into `v`.
- `Data(t, conn, type)` is the next frame's `data`; the frame must be a bare data frame of `type`.
- `State(t, conn, type)` is the next frame of the document `type`, returned as `(doc, patch)`, exactly one of them non-nil.
- `Keep[D](t, conn, type)` keeps the document `type` from its greeting on, as a page does, decoded as `D` in its `Doc`:
  - `Next(t)` merges in the next frame, which must be a patch;
  - `Until(t, done)` merges in patches until `done(Doc)` holds;
  - `Call(t, method, args)` calls `method` with `args` as its named arguments and returns the response's `(data, code)`, merging in the patches that come ahead of it.

Each fails the test where the frame is not what it should be, or does not come within five seconds.
