# greendrake.nz/go/ipcam

The camera engine: IP cameras by name, each logged in to once for its settings and every
stream, the streams fanned out to whatever subscribes to them (`greendrake.nz/go/camstream`)
and recorded to MKV, and the settings read and written in terms of the engine's own rather
than any protocol's. `greendrake.nz/go/ipcamrpc` serves a fleet to a browser app;
`ipcam-app` (in this repository at `apps/ipcam-app`) runs it on one camera, and
`@greendrake/ipcam` is the browser end.

```sh
go get greendrake.nz/go/ipcam
```

## Packages

| Package | What |
|---|---|
| `ipcam` | The core: the ports, and the settings catalogue |
| `ipcam/dvripcam` | DVRIP (Sofia) cameras, over `greendrake.nz/go/dvrip`: streams and settings, the XMEye vendor's among them |
| `ipcam/rtspcam` | RTSP cameras, over `greendrake.nz/go/rtsp`: streams alone, at paths of their own (`BITVISION`'s at `/0` and `/1`) |
| `ipcam/protocols` | The adapters by the names a host's configuration gives their protocols: `dvrip`, and `rtsp` for `BITVISION`'s |
| `ipcam/connection` | One camera's login, kept up |
| `ipcam/fleet` | Cameras by name: their connections, and a source of each stream |
| `ipcam/record` | A stream recorded to MKV files, written by `record/mkv` |
| `ipcam/ipcamtest` | Cameras in memory behind the ports, for tests |

## Ports and adapters

The core knows no camera protocol; each protocol is an adapter behind its ports.

- `Driver` logs in to the cameras of one protocol: `Dial` gives a login, a `Camera`, or
  fails with `ErrCredentials`, `ErrUnreachable` or `ErrUnrecognised`. A driver whose
  cameras have settings is a `SettingsDriver` too, whose `DialSettings` gives a `Login`:
  a `Camera` and its `Settings`.
- `Camera` is one login, until `Done` (with `Err` saying why): its streams' formats as at
  login, and `OpenStream`, a `camstream.Monitor` of one of them, `main` or `extra`.
- `Settings` are what a `SettingsDriver`'s login has besides: its catalogue, their values,
  a write, its clock, and a reboot.

A setting has a key (`network.gateway`, `encode.main.resolution`), the group of its key's
first part, a kind (switch, integer, fraction, text, IPv4, choice, read only, or pinned to
one value), a value in its kind's form (a dotted quad, an option's value naming what it
means), a label and a tip. `Check` makes a written value, as `encoding/json` decodes it,
what the setting's kind takes before any adapter sees it.

The core's groups are `general`, `encode`, `network`, `ntp`, `camera`, `overlay` and
`maintenance`. Two of them are named by the browser end as well, so their IDs stay as they
are: `encode`, where a stream's codec is set (`@greendrake/ipcam`'s `ENCODE_GROUP`), and
`maintenance`, which shows the camera's clock and its reboot beside its settings
(`MAINTENANCE_GROUP`).

The core's catalogue (`catalogue.go`) has the settings any IP camera has. An adapter adds
its protocol's cameras' own as an `Extension`: settings in the core's groups, or in groups
of their own after the core's. A login's catalogue, `NewCatalogue` of its adapter's
extension and of what the camera adds to each setting it has (a choice's options, a note
on its own behaviour), has the groups, keys and descriptions of the settings that camera
has.

`dvripcam` binds each setting to its field in a device configuration through a table,
with a codec between the device's encoding and the setting's. A family's models and
firmwares differ in what they report of themselves, so a setting can have several
bindings, each for the cameras whose capabilities it holds for (the first that holds is
the setting's); a configuration the camera does not keep leaves its settings out. Its
extension (`dvripcam/catalogue.go`) has the XMEye vendor's settings: SmartEncode, IRSwap,
image styles, Gear Control, online upgrades, what recording does when storage is full, and
the device's number. A setting is added as a catalogue entry, the core's or an adapter's,
and a binding in each adapter whose cameras have it (`dvripcam/bindings.go`); a protocol,
as an adapter implementing the ports.

`rtspcam`'s `Driver` says where its cameras serve each stream, how their packets come (UDP
or TCP) and how often a login is probed. Its login is an RTSP control connection: it
describes every stream, which authenticates it (a refused password is a 401) and tells
each stream's codec, and is probed with `OPTIONS`, a probe that fails ending it. A stream
plays on a connection of its own, its video alone, with the parameter sets its description
carries ahead of its random access units until the stream sends its own.

`rtspcam/fakecam` runs a camera `BITVISION` logs in to, for developing and testing a host
end to end without the hardware: both its streams play a recorded camera's H.265 at its
frame rate, over and over (`greendrake.nz/go/rtsp`'s `rtsptest`).

```
go install greendrake.nz/go/ipcam/rtspcam/fakecam@latest
fakecam -listen 127.0.0.1:8554 -user admin -pass ''
```

## Connection

`connection` keeps one login per camera, which carries its settings and every stream.
`Dial` logs in at once, failing as the driver does; `Start` begins offline. `Run` keeps
the camera connected until its context ends, pushing the state as it starts and on every
change: an ended login is dialled again once a second until the camera answers, a reboot
for one, and a refused password leaves the camera offline with the refusal and dials no
more. Every settings read and write goes through it, checked by the core; a write that
moves the camera (its address or port) moves the dialling with it.

The state says whether the camera is `online` or `offline`, where, why the last dial
failed, whether it has settings (its driver's, so known offline too), whether a write
awaits a restart, and each stream's codec as at the last login and whether the engine
casts it.

## Fleet

`fleet` is a host's cameras by name, each with its connection, which the host Dials or
Starts and the fleet runs, and a `camstream.Source` of each stream, all living as long as
the fleet.

- A source opens its stream on the camera's login while it has subscribers, waiting out
  the camera being offline. An open that fails is logged (`WithLogger`, `slog.Default()`
  unless given), once for as long as it keeps failing for the same reason.
- A camera can take its streams from elsewhere instead (`StreamsFrom`, an attempt at
  opening one: another host relaying them, `ipcamrpc.Upstream`), which pulls each stream
  off its camera once for every host that wants it. The camera is still logged in to
  here, for its state, its settings and the codec wait below. `Source` says whether a
  camera's streams are relayed so, which a host serving them uses to keep a relay from
  leading back to where it started.
- The engine casts H.265 alone. A stream on another codec is not opened: it has no
  `Source`, and its recorder waits for the next login, where a codec change applies.
- A stop the camera leaves unanswered ends the login (the DVRIP session gives up on it),
  and with it every stream on that camera; the redial restores it within a second.
- `New` sets the fleet up, and `Start` logs its cameras in, pushing every change of a
  camera's state as its name and state. The split lets what the states are pushed
  through be built over the fleet before the first push.
- Each stream named under a camera's `Record` is recorded for as long as the fleet runs.

## Recording

`record` subscribes to a stream, and again at once whenever the subscription ends: the
new one waits for the source's next open. Each subscription starts a new file, with an
audio track (G.711 A-law at 8 kHz) if its monitor carries audio. Files are at
`<dir>/<camera>/YYYY/MM/DD/HH-mm-ss.<stream>.mkv`, each starting at a key frame: the
first one due once the file before is 10 minutes long, so a file holds 10 minutes and up
to a key frame interval more.

- No frame falls between two files. Every video frame before the key frame that starts a
  file goes in the file before. So does the audio due before it, which comes a little
  behind the video it goes with: the file before stays open for it until audio due in
  the new file comes, or the next key frame does, or the recording ends.
- A file's head is whole from the start: the decoder configuration and picture size come
  from the key frame it starts at, which has to carry the parameter sets (no file starts
  at one that does not), and each random access picture goes in as a key frame with them
  ahead of it.
- `record/mkv` writes each frame to the file as it comes, a block at a time, on the
  recorder's goroutine. A Cluster opens at each key frame, and wherever a block's time
  relative to the open Cluster's would leave its 16 bits. What is known only at close
  is written over places held for it: the sizes of the Clusters and the Segment, 8 bytes
  each, and Voids of their sizes for the duration and the SeekHead's entry for the Cues.
- A file is finished, the open Cluster's size, the index of its key frames (Cues) and the
  SeekHead's entry for it, its duration and the Segment's size written, once it has all
  it holds and as the recorder stops with its context: a process stopped by a signal it
  handles leaves every file whole.
- A file whose process is killed holds every frame up to the one being written. Its last
  Cluster and its Segment are of unknown size, which Matroska allows, and it says nothing
  of its duration, and has no index, until it is remuxed: the Voids hold their places,
  rather than values that would be wrong. Nothing is synced to disk, so a machine that
  goes down loses besides what the system had yet to write out.
- A disk that stalls a write for longer than the subscription's buffer of 256 frames
  (seconds of video and audio) holds the recorder up until the subscription is dropped
  for falling behind, as any subscriber's is: the recorder subscribes again, and the next
  key frame starts a new file.
- A file the writer fails at, a full disk say, is closed as it is, every write after the
  failure answering it, the failure logged, and the next key frame starts another. A file
  that cannot be started, its key frame carrying no parameter sets say, is logged once
  for as long as it fails for the same reason.
