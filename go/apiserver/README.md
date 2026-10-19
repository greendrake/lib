# greendrake.nz/go/apiserver

`net/http` scaffolding for a service binary: its listeners and graceful
shutdown, the health probes, CORS, and JSON answers.

```sh
go get greendrake.nz/go/apiserver
```

## Process

`NewProcess(name, deadline, lameDuck, opts...)` is a binary's serving
lifecycle; `WithLogger` is where its lifecycle lines go, `slog.Default()`
unless given. `Listen(label, port, handler)` binds a port at once — a taken
port fails the start, not the serve; `Serve(label, listener, handler)` takes
one the binary has bound itself, on an address of its choosing or to learn
the port it got — and `Go(fn)` runs background work the drain waits for.
`Run(stop, release)` serves every listener until SIGINT or SIGTERM, or until a
listener fails, then drains under `deadline`:

1. `Draining()` flips: `/healthz` and `/readyz` answer 503, and orchestrators
   stop routing new work.
2. A `lameDuck` pause lets that unreadiness propagate.
3. `stop` winds down the non-HTTP work (task brokers, consumers), then every
   listener shuts down gracefully in the order added, then `release` frees
   what their handlers' background work ran on.
4. The `Go` work returns.

`Run` returns the listener's failure, or a drain past `deadline`; the binary
exits on it, and brokers redeliver and clients retry.
`Serve(name, port, deadline, handler)` runs a single-listener process to
completion.

## Probes

`ProbeMux(draining, checks, timeout)` serves `GET /healthz` — 200 unless
draining — and `GET /readyz`, which runs every `DependencyCheck` in parallel
under `timeout` and answers 200 only when all pass and the process is not
draining; its body maps each probe to `ok` or `fail: <reason>`. The host
mounts its own routes on the mux.

## CORS

`CORS(CORSOptions{AllowOrigin, AllowHeaders, ExposeHeaders, Credentials}, next)`
wraps the server's handler. A request from an origin in `AllowOrigin` is
answered with that origin echoed (`Vary: Origin`), a fixed base set of request
headers plus `AllowHeaders`, and `ExposeHeaders`; one from any other origin
gets no CORS headers. `"*"` admits every origin, and cannot go with
`Credentials`. OPTIONS preflights are answered 204 before they reach the
routes.

## JSON

`WriteJSON(w, status, body)` answers a plain-HTTP endpoint with `body` as JSON
(`application/json; charset=utf-8`); a body that does not encode is logged and
answered 500.
