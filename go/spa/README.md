# greendrake.nz/go/spa

Serves a single-page app from a Go binary, the app and the API it talks to on
one origin: baked into the binary, one file to deploy, or from a directory
beside it, which a deploy replaces without restarting the binary.

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

## A build on disk

`Handler` reads everything through the file system it is given as it is asked
for, so over a directory it serves whatever build is there at the time:

```go
app, err := spa.Handler(os.DirFS(dir))
```

serves `dir/dist`. A deploy that uploads the new build beside the old one and
points a `dist` symlink at it atomically (`ln -sfn` to a temporary name, then
`mv -T` over `dist`) has it served from the next request on, with no restart.
Until a build is there, the pages 404.

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

`Handler` writes `settings`' JSON into the element of every index it serves.
An `index.html` without the element is an error when `Handler` is called, not
a broken page later; a build put under it since, whose index has lost the
element, is answered 500 and logged rather than served without the data. A
value that does not marshal panics in `Data`.

## The build

`cmd/bake` is the whole build, run from the binary's module:

```sh
go run greendrake.nz/go/spa/cmd/bake -app ../web -o myapp ./cmd/myapp
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
