# ipcam-app

This is a tool for testing and configuring IP cameras: it connects to a camera and serves a web page which allows to see/hear its stream(s) and update settings (DVRIP cameras only). I use this tool to avoid having to run vendor-built apps for camera configuration.

The tool leverages reusable components which can be used to build comlpex multi-camera systems, including the ability to save streams to MKV.

```
./ipcam-app [-protocol dvrip] [-user admin] [-pass ''] [-listen 127.0.0.1:0] [-origin <origin>]... <host[:port]>
./ipcam-app -version
```

`-protocol` is what the camera speaks:

| Protocol | Cameras | What the app has of them | Port unless given |
|---|---|---|---|
| `dvrip` (the default) | DVRIP (Sofia) | The streams with their sound, and the settings | 34567 |
| `rtsp` | RTSP, serving the main stream at `/0` and the extra at `/1`, as BITVISION's do | The streams alone | 554 |

It logs in to the camera at host within 5 seconds, or exits 1 saying why: unreachable,
the password refused, or not answering over the protocol. The password can be given in
`IPCAM_PASS` instead of `-pass`, which keeps it out of the process list. Then it
serves the app and prints where, `http://127.0.0.1:<port>` on a free port unless `-listen`
says otherwise, until interrupted. `-version` prints the version it was built as, `dev`
for a build without one.

The app's sockets admit pages of the app's own origin alone, unless `-origin` (repeated
for each) or `IPCAM_ORIGIN` (comma-separated) admits others: an origin
(`http://192.0.2.5:5173`), or a pattern of them (`http://*:5173`), its host matched with
`path.Match`. A page of any other origin cannot reach the camera through it.

## Download

Each release is a GitHub Release of
[github.com/greendrake/lib](https://github.com/greendrake/lib/releases) on the tag
`apps/ipcam-app/v<version>`, carrying a static binary for each platform:

| Binary | Platform |
|---|---|
| `ipcam-app-linux-amd64` | Linux, x86-64 |
| `ipcam-app-linux-arm64` | Linux, ARM64 |
| `ipcam-app-darwin-amd64` | macOS, Intel |
| `ipcam-app-darwin-arm64` | macOS, Apple silicon |
| `ipcam-app-windows-amd64.exe` | Windows, x86-64 |

## The app

The camera's tile, the whole page: its main and extra streams (HD/SD), with their sound
where they carry it, and fullscreen, picked in its menu, as is **Settings**, which opens
in a modal, on a camera with settings.

- **Settings**: a tab per group of settings the camera has (General, Encode, Network,
  NTP, Camera, Overlay, Maintenance), opening on the group shown last. A group's
  settings are read from the camera when its tab first opens, the tab showing the
  spinner and nothing else meanwhile, and kept while other groups show, until the
  settings close; each group reloads on its own. A change is written to the camera at
  once, a text once committed (leaving the field, or Enter), and read back as the
  camera then has it. Overlay has the camera's name and its date and time over the
  picture: whether each shows, and where, in percent of the picture across and down; a
  new name is drawn by the app, in Go Regular, and sent to the camera as the bitmap it
  shows. Maintenance also reads and sets the camera's clock, from the browser's local
  time, and reboots it.

A banner above the settings says how the camera is: online, offline with why, waiting
for a restart a setting needs (with Reboot), or encoding a stream in other than H.265,
which the app does not play (with a link to the encoding settings, which switch it).

## How it works

`server/cmd/ipcam-app` runs the camera engine, `greendrake.nz/go/ipcam`, whose README has
how it works, on a fleet of the one camera, named `camera`, through the engine's adapter
of its protocol (`ipcam/protocols`), and serves it with `greendrake.nz/go/ipcamrpc`. When
the login ends, a reboot for one, the camera is offline and dialled again each second
until it answers; API calls made meanwhile fail with `CAMERA_OFFLINE`, and streams wait.

On one listener:

- `/api`: the engine's RPC WebSocket (`rpcserver.Broadcast`): its methods, each on the
  camera named `camera`, which anyone the socket admits may call, and the `cameras` push
  of the camera's state, which is where the web half takes the camera's name from.
- `/streams`: the `greendrake.nz/go/webcast` socket, with keys `camera/main` and
  `camera/extra`. A stream is opened on the camera while someone watches it.
- `/`: the SPA, `web/`, embedded.

## Build

Building takes Go and bun. In a clone of this repository, `bun install` at its root
installs the web half's dependencies; its `go.work` puts the Go modules beside the server.

```
server/build [-target <os>/<arch>] [-version <version>] [-o <file>]
```

bakes the SPA, `web/`, in and leaves a static `server/ipcam-app` for this machine, or for
the `-target` given (`server/build -target linux/amd64`), reporting the `-version`
given. It runs `greendrake.nz/go/spa`'s `cmd/bake`, which builds `web/` with bun and then
the binary with Go.

## Tests

The browser suite is Playwright's, in `web/`, run there once `server/build` has left the
binary:

```
bun --bun playwright test
```

`playwright.config.js` starts what it tests: a fake camera of each protocol
(`greendrake.nz/go/dvrip/cmd/fakecam`, `greendrake.nz/go/ipcam/rtspcam/fakecam`), then
the built `server/ipcam-app` logged in to each. `tests/stack.js` has their settings, each
overridable through the environment, so that a second run on one machine takes ports of
its own:

| Variable | Default | What |
|---|---|---|
| `CAMERA_LISTEN` | `127.0.0.1:8096` | Where the app on the DVRIP camera serves |
| `FAKECAM_LISTEN` | `127.0.0.1:34568` | The fake DVRIP camera's address |
| `FAKECAM_CONTROL` | `127.0.0.1:34581` | The endpoint the specs change the fake DVRIP camera through |
| `FAKECAM_DEVICE` | `techage-hi3516ev100` | The recorded device it answers as |
| `FAKECAM_REBOOT_SECONDS` | `3` | How long a reboot keeps it unreachable |
| `FAKECAM_RESTART` | `Simplify.Encode` | The configurations whose writes take a restart |
| `RTSP_CAMERA_LISTEN` | `127.0.0.1:8097` | Where the app on the RTSP camera serves |
| `RTSP_FAKECAM_LISTEN` | `127.0.0.1:8554` | The fake RTSP camera's address |
