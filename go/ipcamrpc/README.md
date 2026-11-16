# greendrake.nz/go/ipcamrpc

Lets a browser app watch and configure the IP cameras that a Go program manages with
`greendrake.nz/go/ipcam`. The app calls methods on a camera to read and change its
settings (`greendrake.nz/go/rpcserver`), is told every camera's state as it changes, and
watches the cameras' live streams (`greendrake.nz/go/webcast`).

```sh
go get greendrake.nz/go/ipcamrpc
```

## Why

`greendrake.nz/go/ipcam` keeps a fleet — a program's set of cameras — connected inside a
Go program, and offers nothing over the network. A program that lets a page in a browser
watch and configure those cameras has to write the layer between the two: a method for
each thing the page does to a camera, `ipcam`'s failures as codes the page can tell
apart, every camera's state published on each change, and the streams served by key.

Written by hand in each program, that layer comes out a little different in each, and a
browser client written for one does not fit another. This package is the layer, written
once. The host — the program that serves the cameras — mounts it where it chooses.

## Trade-offs

- **Bound to `rpcserver` and `webcast`.** The methods are `rpcserver.MethodDef`s, the
  state is a document of an `rpcserver.Broadcast`, and the streams speak `webcast`'s
  protocol. A host with another RPC layer has no use for this package: it builds on
  `ipcam`'s `fleet` and `connection` itself.
- **One rule for every method and every camera.** `Methods` gives all six methods the
  same `Auth` and `Access`. Reading a setting and rebooting a camera ask the same of
  their caller, on every camera of the fleet. The state is one document too, sent to
  every socket of the `Broadcast` alike. It holds each camera's address, and why its last
  dial failed. There is no view of the fleet for one user.
- **Settings, a clock and a reboot.** Those are all the methods. None adds or removes a
  camera, changes a camera's credentials, or reaches the recordings.
- **Text in English.** A `Problem`'s message is in English, and so are the settings'
  labels and tips, which come from `ipcam`'s catalogue. Nothing translates them.

## Overview

The package is three functions over a fleet. The host mounts them where it chooses:

- `Methods` is the table of methods, for the host's `rpcserver` dispatcher. The host says
  who may call them.
- `Cameras` publishes the states as a document. A socket is sent the whole document as it
  opens, and each change after that as a patch.
- `Streams` is the `webcast` handler of the fleet's streams. `Upstream` gets a camera's
  streams from another host's `Streams`.

## Methods

`Methods(fleet, access)` is the dispatch table. Each method acts on one camera: the one
that `Camera` names in its arguments. Each asks of its caller what `access` says. `access`
has an `Auth` and an `Access`, as `rpcserver.MethodDef` has them, so the host decides who
may call. The zero `Access` lets anyone call.

- `settings.groups {Camera}`: the groups the camera has settings of.
- `settings.get {Camera, Group}`: the group's settings, described. Only what those
  settings need is read from the camera.
- `settings.set {Camera, Key, Value}` writes one setting. `Value` is taken as
  `encoding/json` decodes it, and checked by `ipcam.Check`. The answer is
  `{Restart, Changed}`. `Restart` says whether the camera applies the write only once it
  has restarted. `Changed` is every setting the write can have changed, as the camera has
  them.
- `camera.time {Camera}`, `camera.setTime {Camera, Time}` (`time.DateTime`, in no zone)
  and `camera.reboot {Camera}`.

Some failures are for the app to show as they are. They carry a `Problem {Message}` in
their details, and their codes are `UNKNOWN_CAMERA`, `NO_SETTINGS`, `CAMERA_OFFLINE`,
`CAMERA_REFUSED`, `UNKNOWN_SETTING`, `UNKNOWN_GROUP` and `INVALID_VALUE`. A `Time` that
does not have its form is `INVALID_ARGUMENT`. Any other failure is returned through
`rpcserver.Answer`, and the dispatcher logs it and answers `INTERNAL_ERROR`.

## State

`Cameras(broadcast, fleet)` publishes every camera's state, by name, as the `rpcserver`
document `cameras`. It returns the push function that the fleet is started with:
`fleet.New` first, then `Start` with that function. A change of one camera's state
reaches the app as a patch of that change alone. A camera's `Streams` is absent before
its first login.

## Streams

`Streams(ctx, fleet, opts...)` is the `webcast.Handler` of the fleet's streams. A stream's
key is `<camera>/<stream>`: `gate/main`. The host mounts the handler where it chooses, and
gives it the origins it admits (`webcast.WithOriginPatterns`). `Streams` takes no
`Access`: who may open the stream socket depends on where the host mounts it, on what the
host puts in front of it, and on those origins. Two kinds of subscription are refused:

- one to a stream that its camera encodes in another codec than `ipcam.Cast`, the one
  codec `ipcam` opens streams in;
- a relay (a raw subscription) of a camera whose own streams are relayed. That keeps
  relays from forming a circle.

`Upstream(url, camera, opts...)` gets a camera's streams from another host, rather than
from the camera. It is what a `fleet.Camera`'s `StreamsFrom` is set to. `camera` is the
camera's name on the other host. `url` is where that host's `Streams` serves its socket
(`ws://` or `wss://`). The streams come through `webcast.Relay`, which takes `opts`.
