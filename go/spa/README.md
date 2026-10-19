# greendrake.nz/go/spa

Ships a single-page app inside a Go binary: one file to deploy, the app and
the API it talks to on one origin.

```sh
go get greendrake.nz/go/spa
```

## The binary's side

The main package embeds the app's build from its own `dist/` and serves it:

```go
//go:embed all:dist
var dist embed.FS

app, err := spa.Handler(dist)
…
mux.Handle("/api", api)
mux.Handle("/", app)
```

`Handler` serves a regular file that exists as itself, and any other path — a
directory's too, which is never listed — the app's `index.html`, so the app's own
routes work on a full page load. `dist/.gitkeep`
is tracked and the rest of `dist/` ignored, so the package compiles before the
app has been built (its pages then 404).

## Runtime data

What the app can only learn once the binary runs — the binary's own
configuration — travels in its index. The app's build leaves an empty data
element in `index.html`:

```html
<script type="application/json" id="env"></script>
```

and the binary fills it:

```go
app, err := spa.Handler(dist, spa.Data("env", settings))
```

`Handler` writes `settings`' JSON into the element once, when it starts. An
`index.html` without the element is an error then, not a broken page later.

## The build

`cmd/bake` is the whole build, run from the binary's module:

```sh
go run greendrake.nz/go/spa/cmd/bake -app ../web -o ipcam-app ./cmd/ipcam-app
```

1. The app's build in its directory — `bun run build`, or the command
   `-build` gives — which leaves the build in its `dist/`. The environment
   passes through: an app's build settings (an API base such as
   `API_URL=/api`) ride it.
2. The build replaces the main package's `dist/`, all but `.gitkeep`. A
   directory without `.gitkeep` is refused, never emptied.
3. `go build -trimpath -ldflags='-s -w'` with `CGO_ENABLED=0`: a static binary
   for this machine, or for `-target <os>/<arch>`. The target is a flag rather
   than `GOOS`/`GOARCH` in the environment, which would have `go run` build
   bake itself for the target and fail to run it. `-version <version>` is
   stamped into the string variable `-version-var` names, `main.version`
   unless given.
