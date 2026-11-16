# greendrake.nz/go/apiserver

What a Go service binary needs around its `net/http` handlers: listeners that
stop without cutting work off, health probes, CORS, JSON answers and a
handler for webhooks. No dependencies.

```sh
go get greendrake.nz/go/apiserver
```

## Why

A service behind a load balancer, or under an orchestrator, is stopped on
every deploy. The obvious `main` ends in `http.ListenAndServe`, and it stops
badly. A Go program that does not catch SIGTERM exits at once, and the
requests in flight get no answer. Closing the listener on the signal is not
enough. A load balancer that polls a probe keeps sending requests until its
next poll, and those meet a closed port. And `http.Server.Shutdown` does not
wait for queue consumers, or for the goroutines a handler started.

Stopping well is a fixed sequence of steps, run against a deadline.
`Process` is that sequence, written once. The package adds the other code
such a binary repeats: health probes, a CORS policy, a JSON answer and a
webhook handler.

## Trade-offs

- **`Run` stops on a signal, and on nothing else.** It serves until SIGINT
  or SIGTERM, or until a listener fails. There is no context to cancel and
  no method that stops it. A binary that has to stop its servers on a
  condition of its own cannot do that through `Run`.
- **Listeners have no TLS and no timeouts.** `Process` sets each
  `http.Server`'s handler and nothing else: no TLS, and no read, write or
  idle timeouts. That fits a service behind a proxy that terminates TLS and
  bounds slow clients. A TLS listener is bound by the binary and passed to
  `Serve`. The timeouts cannot be set.

## Overview

- **Ports are bound at the start.** `ListenAndServe` binds when serving
  starts. In a binary with several listeners, each served from its own
  goroutine, a port already in use fails inside a goroutine, after the rest
  of the program is running. `Listen` binds its port at once, so a taken
  port fails the start.
- **Stopping is one sequence.** Answer the probes "not ready". Wait for
  that to be seen. Stop the background work, shut the listeners down, and
  release what their handlers ran on. Wait for what is left, and give up at
  a deadline. `ProbeMux` serves the probes that the sequence turns to 503.
- **The other code such a binary repeats.** A CORS policy, a JSON answer,
  and a webhook handler, whose status is what the provider decides
  redelivery on.
- **Plain `net/http`.** Handlers are `http.Handler`s, the probes come as an
  `*http.ServeMux` to add routes to, and the router stays the binary's
  choice.

## Process

`NewProcess(name, deadline, lameDuck, opts...)` holds a binary's listeners
and background work, from its start to its stop. `WithLogger` sets where it
logs the start and the stop, `slog.Default()` unless given.

- `Listen(label, port, handler)` binds a port at once, on every interface.
  A taken port therefore fails the start, not the serve.
- `Serve(label, listener, handler)` takes a listener the binary has bound
  itself: on one address, with TLS, or to learn the port it got.
- `Go(fn)` runs background work. The drain, described next, waits for it.

`Run(stop, release)` serves every listener until SIGINT or SIGTERM, or until
a listener fails. Then it drains: it stops taking new work and lets the work
in flight finish. The drain has four steps:

1. The flag that `Draining()` returns is set. `/healthz` and `/readyz` then
   answer 503, and orchestrators stop routing new work.
2. A pause of `lameDuck` gives that answer time to be seen.
3. `stop` stops the non-HTTP work (task brokers, consumers). Then every
   listener shuts down gracefully, in the order added. Then `release` frees
   what their handlers' background work ran on.
4. `Run` waits for the `Go` work to return.

One `deadline` covers all four steps. When it passes, `Run` returns an
error with the drain still running, and what was in flight is lost when the
binary exits.

`Run` returns the listener's failure, or a drain past `deadline`. The binary
exits on it. What the exit cuts off recovers elsewhere: brokers redeliver,
and clients retry.

The listeners shut down with `http.Server.Shutdown`. It waits for requests,
but not for hijacked connections such as WebSockets. Ending those is left to
`stop` or `release`.

The package-level `Serve(name, port, deadline, handler)` runs a
single-listener process to completion.

## Probes

`ProbeMux(draining, checks, timeout)` returns a mux that serves two probes:

- `GET /healthz` answers 200 unless the process is draining.
- `GET /readyz` runs every `DependencyCheck` in parallel under `timeout`. It
  answers 200 only when all pass and the process is not draining. Its body
  maps each check to `ok` or `fail: <reason>`, where the reason is the
  check's error text.

`/readyz` runs every check on every request. Nothing is cached, so a check
has to be cheap enough for the rate the probe is polled at. Neither probe
asks who is calling. So keep secrets out of check errors, or keep the probes
off a public listener.

The binary adds its own routes to the mux.

## CORS

`CORS(CORSOptions{AllowOrigin, AllowHeaders, ExposeHeaders, Credentials}, next)`
wraps the server's handler. The policy is fixed apart from those options:

- A request from an origin in `AllowOrigin` is answered with that origin
  echoed (`Vary: Origin`). The answer allows a fixed base set of request
  headers plus `AllowHeaders`, and exposes `ExposeHeaders`.
- Origins are compared as whole strings, so there is no pattern for "every
  subdomain".
- A request from any other origin gets no CORS headers.
- `"*"` admits every origin, and cannot go with `Credentials`.
- The methods allowed are always `POST, OPTIONS, GET, PUT, DELETE`. `PATCH`
  is not among them, so a browser will not send one across origins.
- No `Access-Control-Max-Age` is set.
- Every `OPTIONS` request is answered 204 before it reaches the wrapped
  handler, so that handler never sees the method.

## JSON

`WriteJSON(w, status, body)` answers a plain-HTTP endpoint with `body` as JSON
(`application/json; charset=utf-8`). A body that does not encode is logged
and answered 500.

## Webhooks

`Webhook(name, rejected, handle)` is the handler of a provider's callback
that reports an event and expects only a status back. `handle(r, body)`
authenticates the callback and acts on it. Its error decides the status, and
the provider decides by the status whether to send the callback again:

- an error matching `rejected` (`errors.Is`) is a 401, which says nothing of
  why;
- any other error is a 500;
- no error is a 200.

A body past `MaxWebhookBody` (1 MiB) is a 413, and one that cannot be read
is a 400. Every answer except the 200 is logged under `name`.

The answer is a status and nothing else: `handle` is given the request and
its body, not the response. A provider that expects something in the
answer's body — a verification challenge echoed back, say — needs a handler
of its own.
