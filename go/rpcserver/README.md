# greendrake.nz/go/rpcserver

The server side of an application's API, as named methods. One table of
methods, with authentication declared per method, is served over an HTTP
POST endpoint and over a WebSocket, which also carries what the server
pushes. The wire format is the package's own, described below. It is not
JSON-RPC.

```sh
go get greendrake.nz/go/rpcserver
```

## Why

An application whose client is its own — a single-page app, a mobile app —
calls its server by name: `orders.list`, `profile.update`. It also wants to
be told things without asking: a message has arrived, a job has finished.

The obvious build is two APIs: HTTP routes for the calls, and a WebSocket
with a protocol made up for the pushes. The two then solve the same problems
twice. A browser cannot set headers on a WebSocket, so the socket needs a
second way to authenticate. Errors are status codes on one side and invented
frames on the other. And a handler written for a route does not serve a call
that arrives on the socket. rpcserver serves one table of methods over both
transports, with one message format, one kind of credential and one set of
error codes.

## Trade-offs

- **The wire format is its own.** It is not JSON-RPC: errors are string
  codes, a request carries a nonce, a locale and an abort, and there are no
  batches and no notifications. There is no schema either. The table is a
  Go map, and nothing generates a client or an API description from it. For
  an API that others consume from a published schema or generated clients,
  use OpenAPI or gRPC.
- **HTTP says nothing about the outcome.** Every call is a POST to one URL,
  answered 200 whether it succeeded or failed. The method and the outcome
  are in the bodies. A proxy, an access log or a monitor that watches status
  codes sees neither.
- **Push stays in one process, and is not stored.** A `ConnRegistry` and a
  `Broadcast` hold the sockets open on their own process. An event reaches
  the sockets its user has open there at that moment. There is no queue and
  no replay, and a server that runs as several processes has to carry events
  between them itself.
- **Bearer tokens only.** The package reads no cookie. Over WebSocket a
  token is resolved once, at the handshake or in an `auth.token` frame. The
  socket then stays authenticated as the token's owner until `auth.logout`
  or until it closes, whatever becomes of the token.

## Overview

rpcserver serves one table of methods over HTTP and over WebSocket:

- **One message format, two transports.** A handler is written once, as
  `func(hc, args) (result, error)`. The same request and response are sent
  as an HTTP POST or as WebSocket frames, and the handler is not told which.
- **Authentication is declared per method.** A method says whether it needs
  a principal, which is an authenticated caller, and what it asks of one. A
  bearer token names the principal on either transport. One hook, the
  `AuthGate`, decides who may call what. `NewDispatcher` panics on a method
  that asks something nothing would enforce.
- **A failure is a code.** A response either succeeded or carries a code the
  client branches on. Arguments that fail validation are answered
  `INVALID_ARGUMENT`, with an entry for each field rule that failed.
- **Retries and cancellation are in the protocol.** A request may carry a
  nonce: a string unique to it. With a `NonceStore`, a nonce seen before is
  refused, so a retried call does not run twice. Over WebSocket a later
  frame aborts a request in flight by its nonce, and the context of its
  handler is cancelled.
- **Push shares the socket.** An event goes to every connection of one
  user. For an app without authentication, a `Broadcast` has every socket
  keep a copy of some state: sent whole once, and as patches after that.

The package holds no policy. What a token is, who may call what, which
locales exist and which validation rules apply all belong to the host: the
server binary that builds the table and serves it.

## Wire format

A request is one JSON object:

| Field | |
|---|---|
| `method` | the method's name |
| `arguments` | what the method takes: an object of named arguments, an array of positional ones, or absent |
| `nonce` | optional, a string unique to the request: what deduplication refuses a second time (`DUPLICATE_REQUEST`), and what `abort` names |
| `locale` | optional, the caller's language tag, resolved against the languages the server supports |
| `id` | WebSocket only: the correlation id its response carries back |
| `abort` | WebSocket only: the nonce of an earlier request to cancel, alone or beside a new request |

A response is `{success: true, data}` or `{success: false, error,
error_details}`. `error` is one of the `Code*` constants, or a code of the
method's own. `error_details` is what the method attached: for
`INVALID_ARGUMENT`, a `[]rpcargs.FieldError`.

Over HTTP the request is the POST body, and the response is the body
answered. The status is 200 whatever the outcome.

Over WebSocket every frame the server sends carries a `type`:

- `response`, with the request's `id`;
- `event`, a push addressed to a user: `{type: "event", event, data}`;
- one of a Broadcast's own types (below): `{type, data}` for a signal, and
  `{type, data: {doc}}` then `{type, data: {patch}}` for a document.

Two methods are the socket's own:

- `auth.token` takes one positional argument, a bearer token, and binds the
  socket to the token's principal. It is answered `UNAUTHENTICATED` when the
  token names no principal, and `AUTH_NOT_CONFIGURED` on a dispatcher with
  no `AuthResolver`.
- `auth.logout` ends that binding.

An `abort` gets no response, and the response of the request it cancelled
is not sent.

## Dispatch

`NewDispatcher(methods, opts...)` takes the table of methods, a
`map[string]MethodDef`. The host merges the method maps of its libraries
into this one table. Each `MethodDef` has:

- `Handler`: a `HandlerFunc(hc, args) (result, error)`.
- `Auth`: `AuthNone` or `AuthRequired`.
- `Access`: what the method asks of its principal. The host's `AuthGate`
  reads it, and its meaning is the host's to define.
- `AllowPlain`: over HTTP, the method is accepted as plain JSON where
  requests must be encoded. A WebSocket has no such exemption.

A request runs through these steps, in order:

1. The locale and the validator are put on the context. The locale is the
   one the request asks for, resolved by the host's `LocaleResolver`
   (`WithLocaleResolver`). Handlers read it with `LocaleFrom(ctx)`. The
   validator is the dispatcher's `Validator` (`WithValidator`), read with
   `ValidatorFrom(ctx)`.
2. The method is looked up. An unknown one is `UNKNOWN_METHOD`.
3. A method requiring a principal is refused without one: `AUTH_REQUIRED`.
   The transport resolves the principal from the request's bearer token,
   through the `AuthResolver` that `WithAuthResolver` gives. An
   `AuthResolver` turns a token into `AuthInfo`. Its `Claims` field is for
   the host, and the package does not read it. `KeyAuthResolver` is the
   resolver for a pre-shared key, which it compares in constant time.
4. The host's `AuthGate` (`WithAuthGate`) runs, on every call of a method
   requiring a principal. It is the one authorisation hook: it reads the
   method's `Access`, and may put what it loaded on the context. A WebSocket
   resolves its token only once, so the gate is also where a host checks a
   principal again. The package holds no policy of its own. `NewDispatcher`
   panics on a method that has an `Access` and no principal to ask it of, or
   no gate to enforce it.
5. The nonce is looked up in the `NonceStore` that `WithNonceStore` gives. A
   nonce seen before is answered `DUPLICATE_REQUEST`, not the first
   request's response. A new nonce is recorded before the handler runs, and
   stays recorded if the handler fails. A request refused before this step
   does not record its nonce, so it can be sent again with the same one.
   Nonces are one namespace for all callers. If the store itself fails, the
   failure is logged and the request runs. `NewMemoryNonceStore` keeps
   nonces in the process, each for five minutes (`NonceTTL`). It is for a
   server that runs as one process. A server that runs as several supplies a
   shared `NonceStore`.
6. The handler runs. An `*APIError` (`NewAPIError`,
   `NewAPIErrorWithDetails`) is answered by its code. Any other error is
   logged, and answered `INTERNAL_ERROR`.
   `Answer(method, err, []ErrorCode)` maps a handler's sentinel errors to
   codes.

`GetAuth(ctx)` is the principal, and `UserID[T](hc)` is its id as the host's
type.

`hc.ClientIP` is the caller's address. It is the address the connection
came from (`PeerIP`), unless the host derives it from a proxy it trusts
(`WithClientIP`). `ForwardedIP` derives it behind exactly one proxy: it is
the last `X-Forwarded-For` entry, which that proxy appended. For a request
without the header, it is the address the connection came from.

`hc.UserAgent` is the caller's `User-Agent` header. Over WebSocket the
address and the user agent are both read from the handshake, so they are the
same for every call on the socket.

`WithLogger` sets where the dispatcher and its transports log failures,
`slog.Default()` unless given.

The `Code*` constants are the codes the protocol answers with itself.
`MethodAuthToken`, `MethodAuthLogout` and `TokenParam` are the WebSocket's
reserved methods and its token parameter.

## Arguments: `rpcserver/rpcargs`

Three functions parse a method's arguments:

- `ParseAndValidate[T]` parses a struct, validated by its
  `go-playground/validator` tags.
- `ParseAndValidateVar[T]` parses one primitive.
- `ParsePositional` parses positional arguments.

Arguments that do not parse or validate are `INVALID_ARGUMENT`, with a
`FieldError` for each rule that failed. A client that gives a call no
arguments sends a request without any. The targets then keep their zero
values, and `ParseAndValidate` validates those like any other value.

The validator is the one the request's dispatcher was given.
`rpcargs.NewValidator(rules...)` makes one: it is go-playground's, with the
built-in tags and the host's `Rule`s. Each `Rule` is declared beside the
code whose arguments it validates. The built-in tags:

- `trimmed_nonempty`: a string that is not empty once trimmed.
- `email_safe`: an email address of a shape that is safe in a mail header.
  It is more permissive than go-playground's `email`, which refuses legal
  addresses such as `+suffix@example.io`. It admits no line break.

Code that parses arguments outside a dispatcher, such as a test, puts a
validator on its context with `ContextWithValidator`.

`OneOf` refuses a value outside a set of allowed ones: a set known only at
run time, or the values of a string type. `Maybe[T]` tells a field that is
absent from one that was sent as null. Tag such a field `omitzero`. Then
an absent field is left out when the struct is encoded, instead of being
sent as null.

## Transports

`NewHTTPHandler(d, WithHTTPEncoding(e), WithHTTPBodyLimit(n))` and
`NewWSHandler(d, registry, WithWSEncoding(e), WithWSOriginPatterns(patterns...),
WithWSOnOpen(fn), WithWSOnClose(fn), WithWSReadLimit(n))`.

- A credential is a `Bearer` token. Over HTTP it is in the `Authorization`
  header. Over WebSocket it is in the `token` query parameter, or in an
  `auth.token` frame. A token in the query parameter is part of the URL, and
  URLs are commonly written to access logs. The `auth.token` frame avoids
  that.
- An HTTP body over the limit (`DefaultHTTPBodyLimit`, 1 MiB, unless given) is
  `REQUEST_TOO_LARGE`. A WebSocket message over the read limit (the WebSocket
  library's 32 KiB unless given) closes the socket.
- A browser's socket is admitted from the endpoint's own origin, and from
  the origins `WithWSOriginPatterns` lists (`https://app.example.com`,
  `app://localhost`). A pattern without a scheme matches the host alone. A
  socket from any other origin is refused at the handshake. HTTP's origin
  policy is CORS, which is the host's to apply.
- Without an `Encoding` the messages are plain JSON. An `Encoding` passes
  them through a `Codec`, for a server that obscures or encrypts its
  messages. A request that opens and closes as JSON does, with braces or
  brackets, is taken as it is. Any other is decoded. `EncodeResponse`
  encodes every response. `RequireEncodedRequest` refuses a plain request,
  as a malformed one. A principal marked `PlainTransport` is exempt both
  ways.

A `ConnRegistry` holds each user's open connections. It is an `Emitter`:
`Emit(ctx, userID, event, data)` pushes `{type: "event", event, data}` to
them.

Nothing pushed to an open socket is ever dropped. A socket's send buffer holds 64
frames. When it fills, the client has stopped keeping up, and a lost frame would leave
that client wrong without its knowing. So the socket is closed instead, at once and
without a close handshake. The client reconnects. Its state is put right by what it
reads again on reconnecting, or by the documents it is sent when the socket opens. A
push that cannot be encoded as JSON panics: it is a bug in the code that pushed it.

## Broadcast

`Broadcast` is the endpoint of an app without authentication. It is one WebSocket
endpoint. It carries the dispatcher's methods and everything the app pushes, and it
pushes the same to every open socket. `NewBroadcast().Handler(dispatcher, opts...)`
serves it. `opts` are the transport's, except the open and close hooks, which the
Broadcast sets itself.

- `Document(type, produce)` shares some state with every socket. The state is one JSON
  object, and every socket keeps a copy of the whole of it.
  - An opening socket is sent the document as last published, `{type, data: {doc}}`.
  - Each change after that goes as an RFC 7386 merge patch over it,
    `{type, data: {patch}}`. Objects merge, `null` removes a member, and anything else
    replaces, so collections are objects keyed by id.
  - A member produced as `null` is dropped, the same as one left out, since a patch
    cannot tell the two apart.
  - `produce` runs once when the document is made, so there is a document for every
    socket that opens. It runs again on each `Refresh`. A `Refresh` pushes what changed
    since the document was last published, and nothing when nothing did. The code that
    owns the state calls `Refresh` on any change it knows of.
  - Every `Refresh` produces the entire state, encodes it and compares it with the last
    one. A changed array goes out whole, and every socket gets every document. That
    suits a modest state that everyone shares, such as a dashboard's. It does not suit a
    large state, or one that differs by viewer.
  - Sending a socket its document and publishing a patch never overlap. So no patch
    reaches a socket before its document, and no patch computed before the document
    reaches it after.
  - `produce` runs under the document's lock. So code must not call `Refresh` while it
    holds a lock that `produce` takes.
  - A state that is not a JSON object panics: it is a bug in `produce`.
- `Push(type, data)` sends a signal to every open socket, as a `{type, data}` frame. A
  signal says that something happened, and the client answers it by reading again.

## `rpcservertest`

A client of a WebSocket endpoint, for tests. It reads the endpoint as a browser does.

- `Dial(t, url)` opens a socket on an httptest server's `http://` URL, query and all, and closes it when the test ends.
- `Read(t, conn, &v)` decodes the next message into `v`.
- `Call(t, conn, method, args)` is for a socket that keeps no document. It calls `method` with `args` as its named arguments, and returns the response's `(data, code)`. The response must be the next frame.
- `Data(t, conn, type)` is the next frame's `data`. The frame must be a `{type, data}` frame of `type`.
- `State(t, conn, type)` is the next frame of the document `type`, returned as `(doc, patch)`. Exactly one of the two is non-nil.
- `Keep[D](t, conn, type)` keeps the document `type` as a page does. The next frame must be the whole document. The document, decoded as `D`, is in its `Doc`.
  - `Next(t)` merges in the next frame, which must be a patch.
  - `Until(t, done)` merges in patches until `done(Doc)` holds.
  - `Call(t, method, args)` calls `method` with `args` as its named arguments, and returns the response's `(data, code)`. It merges in the patches that come ahead of the response.

Each fails the test where the frame is not what it should be, or does not come within five seconds.
