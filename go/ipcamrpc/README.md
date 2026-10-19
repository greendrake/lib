# greendrake.nz/go/ipcamrpc

A `greendrake.nz/go/ipcam` fleet over the wire, to a browser app: its methods
(`greendrake.nz/go/rpcserver`), the state it pushes, and its streams
(`greendrake.nz/go/webcast`). `@greendrake/ipcam` is the browser end.

```sh
go get greendrake.nz/go/ipcamrpc
```

## Methods

`Methods(fleet, access)` is the dispatch table. Each method is on the camera its
arguments' `Camera` names, and asks of its caller what `access` says, `Auth` and `Access`
as `rpcserver.MethodDef` has them: the host decides who may call. The zero `Access` lets
anyone.

- `settings.groups {Camera}`: the groups the camera has settings of.
- `settings.get {Camera, Group}`: the group's settings, described, read from the camera as
  they alone need.
- `settings.set {Camera, Key, Value}`: one setting written, its value as the wire has it,
  checked by the core; answering `{Restart, Changed}`, whether the camera applies it once
  restarted and every setting the write can have changed, as the camera has them.
- `camera.time {Camera}`, `camera.setTime {Camera, Time}` (`time.DateTime`, in no zone)
  and `camera.reboot {Camera}`.

Failures the app shows as they are carry a `Problem {Message}` in their details:
`UNKNOWN_CAMERA`, `NO_SETTINGS`, `CAMERA_OFFLINE`, `CAMERA_REFUSED`, `UNKNOWN_SETTING`,
`UNKNOWN_GROUP` and `INVALID_VALUE`. A time of no form is `INVALID_ARGUMENT`. Any other
failure goes through `rpcserver.Answer`, which logs it and answers `INTERNAL_ERROR`.

## State

`Cameras(broadcast, fleet)` publishes every camera's state by name as the `rpcserver`
document `cameras`, and returns the push the fleet is started with (`fleet.New`, then
`Start`). A change of one camera's state reaches the app as a patch of that change alone.
A camera's `Streams` is absent before its first login.

## Streams

`Streams(ctx, fleet, opts...)` is the `webcast.Handler` of the fleet's streams, keyed
`<camera>/<stream>`: `gate/main`. The host mounts it where it chooses, and gives it the
origins it admits (`webcast.WithOriginPatterns`). A stream its camera encodes in another
codec than the cast one is refused, and so is a relay (a raw subscription) of a camera
whose streams are relayed themselves, so that a relay never leads back to where it
started.

`Upstream(url, camera)` is a `fleet.Camera`'s `StreamsFrom` taking the camera's streams off
another host's: the camera named so there, on the socket its `Streams` serves at `url`
(`ws://` or `wss://`), with `webcast.Relay`.
