# greendrake.nz/go/spa

Serves a single-page app from a Go binary, so that the app and the API it
talks to share one origin. The app's build is either embedded in the binary,
which leaves one file to deploy, or read from a directory beside it, which a
deploy replaces without restarting the binary. No dependencies.

```sh
go get greendrake.nz/go/spa
```

## Why

A single-page app is a folder of static files and an API. The usual setup
hosts each separately: the files on a static host or behind a web server,
the API in a binary. That is two things to deploy and keep in sync. It also
means two origins, and so CORS, or a proxy configured to join them into one.

Serving the files from the API's binary removes all of that. Go's `embed`
and `http.FileServer` look like the whole job, and they are not. A full page
load of one of the app's own routes is answered 404. The app's settings are
fixed when it is built. And the app's build has to be where the Go compiler
embeds it from, before every compile. This package covers the three.

## Trade-offs

- **Every path that is not a file is answered with the app.** Once a build
  is in place, the server answers no page with 404: showing "not found" is
  the app's router's job. The same goes for a file that is gone. A page
  loaded before a deploy may ask for a script the new build does not have,
  and it is answered the index, with status 200.
- **No caching headers and no compression.** The handler sets no
  `Cache-Control` and no `ETag`, and compresses nothing. The index also goes
  out without a `Last-Modified`. Wrap the handler, or put a proxy in front,
  to add what the app needs.

## Overview

- **The app's routes are served.** The app routes in the browser, so a path
  such as `/orders/42` exists only there. A full page load of it asks the
  server for a file it does not have, and `http.FileServer` answers 404. It
  also lists directories. `Handler` serves a file that exists as itself,
  and the app's `index.html` for every other path.
- **Settings come from the running binary.** What is compiled into the
  app's JavaScript cannot change after the build. A binary that runs in
  several environments then needs a build of the app for each, or the app
  has to request its settings before it can start. `Data` writes a value of
  the binary's own, such as its configuration, into the index as it is
  served. The app reads it from the page, with no request.
- **One command builds both.** The app's build has to be where the Go
  compiler embeds it from, before the binary is compiled, every time.
  `cmd/bake` builds the app, places the build for the embed and compiles
  the binary, as one command.
- **Embedded or on disk.** `Handler` reads from any `fs.FS`. Over an
  embedded one, the app is inside the binary. Over a directory, a deploy
  replaces the build while the binary runs, and the next request gets the
  new one.

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

`Handler` serves a regular file that exists as itself. Any other path gets
the app's `index.html`, so the app's own routes work on a full page load.
That includes the path of a directory, which is never listed. Only GET and
HEAD are served: any other method is answered 405.

`dist/.gitkeep` is tracked by git and the rest of `dist/` is ignored, so the
package compiles before the app has been built. Every page is then answered
404.

## A build on disk

`Handler` reads everything through the file system it is given, as it is
asked for. Over a directory it therefore serves whatever build is there at
the time:

```go
app, err := spa.Handler(os.DirFS(dir))
```

serves `dir/dist`. A deploy uploads the new build beside the old one, and
points a `dist` symlink at it atomically: `ln -sfn` to a temporary name,
then `mv -T` over `dist`. The new build is served from the next request on,
with no restart. Until a build is there, every page is answered 404.

An embedded app changes only with the binary: every change to the app is a
new build and a new deploy of the binary. Where the app is released more
often than the server, serve it from disk.

## Runtime data

Some things the app can learn only once the binary runs: the binary's own
configuration. They are sent in the app's index. The app's build leaves an
empty data element in `index.html`:

```html
<script type="application/json" id="env"></script>
```

and the binary fills it:

```go
app, err := spa.Handler(dist, spa.Data("env", settings))
```

`Handler` writes `settings`' JSON into the element of every index it serves.

- `Data` encodes its value once, when it is called. Every visitor is served
  the same JSON and can read it. So it is for configuration: not for
  secrets, and not for anything that differs by user or by request.
- `Handler` looks for the element's opening tag byte for byte:
  `<script type="application/json" id="env">` for the id `env`. If the app's
  build rewrites the tag — reorders its attributes, drops the quotes —
  `Handler` does not find the element.
- An `index.html` without the element is an error when `Handler` is called,
  not a broken page later.
- A build put under the handler after that may have an index without the
  element. That index is answered 500 and logged. It is not served without
  the data.
- A value that cannot be encoded as JSON panics in `Data`.

## The build

`cmd/bake` is the whole build, run from the binary's module:

```sh
go run greendrake.nz/go/spa/cmd/bake -app ../web -o myapp ./cmd/myapp
```

1. The app is built in its directory, by `bun run build` or by the command
   `-build` gives. The command is split on spaces and run directly, not
   through a shell. It has to leave the build in the app's `dist/`. The
   environment passes through, so the app's build can read its settings
   from it: an API base such as `API_URL=/api`.
2. The build replaces the main package's `dist/`, all but `.gitkeep`. A
   directory without `.gitkeep` is refused, never emptied.
3. `go build -trimpath -ldflags='-s -w'` with `CGO_ENABLED=0` compiles a
   static binary, for this machine or for `-target <os>/<arch>`. The target
   is a flag rather than `GOOS`/`GOARCH` in the environment. Those would
   have `go run` build bake itself for the target, and fail to run it.
   `-version <version>` is written into the string variable that
   `-version-var` names, `main.version` unless given.

The three steps are fixed. A binary that needs cgo, or a build that differs
in another way, does the three steps itself.
