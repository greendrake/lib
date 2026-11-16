# greendrake.nz/go/ipcam

A camera engine to embed in a Go program. It keeps IP cameras logged in, fans each
camera's live streams out to whatever subscribes to them, records them to MKV files, and
reads and writes the cameras' settings under one set of names, whatever protocol a camera
speaks.

```sh
go get greendrake.nz/go/ipcam
```

## Why

A program that shows, records and configures IP cameras could have each part connect for
itself: the viewer for a stream, the recorder for the same stream again, the settings
page for the settings. That goes wrong in three ways. Some cameras serve one connection
at a time. A camera reboots or changes its address, and every part has to notice and
reconnect. And each protocol names and encodes settings its own way, so a settings page
written for one serves no other.

A restreaming server shares the streams, but it is one more process to run, and it knows
nothing of settings. This engine does the work once, inside the program that embeds it —
the host: one login per camera, each stream shared among its subscribers, and settings
under one set of names.

## Trade-offs

- **H.265 only.** The engine opens and records H.265 streams, and re-encodes nothing. A
  stream that its camera encodes in another codec is not opened: nothing watches it and
  nothing records it until the camera is switched to H.265 and has restarted. Where the
  cameras must stay on another codec, this engine is the wrong choice.
- **Two protocols, and settings over one of them.** There are adapters for DVRIP (Sofia)
  cameras and for RTSP cameras, and none for ONVIF. Only the DVRIP cameras have settings.
  An RTSP camera gives its video alone, without sound. Its driver has to be told the
  paths its streams are at, and the `protocols` package knows those of BITVISION's
  cameras only.
- **Streams, settings, a clock and a reboot.** That is all the engine asks of a camera.
  There is no pan and tilt, no motion or alarm events, no snapshots, and no playback of
  what a camera recorded itself. A camera has two streams, `main` and `extra`, on one
  video channel, so a recorder with several channels is not modelled.
- **Recording is continuous, and nothing is pruned.** A stream named under `Record` is
  recorded for as long as the engine runs. There is no schedule and no recording on
  events. Old files are never deleted, so making room is the host's job.

## Overview

- **One login per camera.** The login carries the camera's settings and every stream. A
  stream is opened on it once, however many subscribe, and only while something does: a
  viewer, a recorder, another host relaying it. The streams are
  `greendrake.nz/go/camstream` sources.
- **The login is restored.** A login that ends is dialled again until the camera answers.
  When a settings write changes the camera's address or port, the camera is dialled at
  the new one from then on. Every change of a camera's state is pushed to the host.
- **One set of names for settings.** The core has a catalogue of settings: for each a
  key, a kind, a value in a plain form, a label and a tip. A protocol's adapter ties them
  to the camera's own configurations. A settings page written against the catalogue
  serves any camera that has settings, and it is given only the settings that camera has.
- **Recording to plain files.** A stream is recorded to MKV files of ten minutes, each
  starting at a key frame. A file cut short by a killed process plays as far as it was
  written.
- **Protocols as adapters.** The core knows no camera protocol. DVRIP and RTSP are
  adapters that implement its interfaces, and another protocol is another adapter.

## Packages

| Package | What |
|---|---|
| `ipcam` | The core: the interfaces, and the settings catalogue |
| `ipcam/dvripcam` | DVRIP (Sofia) cameras, over `greendrake.nz/go/dvrip`: streams and settings, the XMEye vendor's among them |
| `ipcam/rtspcam` | RTSP cameras, over `greendrake.nz/go/rtsp`: streams alone, at paths of their own (`BITVISION`'s at `/0` and `/1`) |
| `ipcam/protocols` | The adapters by the names a host gives their protocols in its configuration: `dvrip`, and `rtsp` for `BITVISION`'s |
| `ipcam/connection` | One camera's login, restored whenever it ends |
| `ipcam/fleet` | Cameras by name: their connections, and a source of each stream |
| `ipcam/record` | A stream recorded to MKV files, written by `record/mkv` |
| `ipcam/ipcamtest` | Cameras in memory that implement the interfaces, for tests |

## Interfaces and adapters

The core knows no camera protocol. Each protocol is an adapter that implements the core's
interfaces:

- `Driver` logs in to the cameras of one protocol. `Dial` gives a login, a `Camera`, or
  fails with `ErrCredentials`, `ErrUnreachable` or `ErrUnrecognised`. A driver whose
  cameras have settings is a `SettingsDriver` too. Its `DialSettings` gives a `Login`: a
  `Camera` and its `Settings`.
- `Camera` is one login. It lasts until `Done`, and `Err` then says why it ended. It has
  its streams' formats, as they were at login. Its `OpenStream` opens one of the streams,
  `main` or `extra`, as a `camstream.Monitor`.
- `Settings` are what a `SettingsDriver`'s login has besides its streams: the catalogue
  of its settings, their values, a write, the camera's clock, and a reboot. A write
  changes one setting.

A setting has:

- a key, such as `network.gateway` or `encode.main.resolution`;
- a group, which is the first part of its key;
- a kind: switch, integer, fraction, text, IPv4, choice, read only, or pinned to one
  value;
- a value in its kind's form. An IPv4 address is a dotted quad. A choice's value is one of
  its options' values, which names what the option means;
- a label and a tip.

`Check` takes a value to be written, as `encoding/json` decodes it, and converts it to
the form the setting's kind takes. That happens before any adapter sees the value.

The core's groups are `general`, `encode`, `network`, `ntp`, `camera`, `overlay` and
`maintenance`. Clients know two of them by ID, so those IDs stay as they are. `encode` is
where a stream's codec is set. A client shows the camera's clock and its reboot beside
the settings of `maintenance`.

The core's catalogue (`catalogue.go`) has the settings any IP camera has. An adapter adds
the settings of its own protocol's cameras as an `Extension`. Those settings go in the
core's groups, or in groups of the extension's own, which come after the core's.

Each login has a catalogue of its own, made by `NewCatalogue`. It is made from the
adapter's extension, and from what the camera adds to each setting it has: a choice's
options, or a note on the camera's own behaviour. The login's catalogue has the groups,
keys and descriptions of the settings that camera has.

`dvripcam` keeps a table of bindings. A binding ties a setting to its field in one of the
device's configurations, with a converter between the device's encoding and the
setting's. The models and firmwares of one family differ in what they report of
themselves. So a setting can have several bindings. Each applies to cameras of certain
capabilities, and the first binding that applies to a camera is the setting's. When a
camera does not keep a configuration, the settings bound to that configuration are left
out. Each write reads the configuration the setting is in, writes it, and reads it back.

`dvripcam`'s extension (`dvripcam/catalogue.go`) has the XMEye vendor's settings:
SmartEncode, IRSwap, image styles, Gear Control, online upgrades, what recording does when
storage is full, and the device's number.

To add a setting, add a catalogue entry, the core's or an adapter's, and a binding in
each adapter whose cameras have the setting (`dvripcam/bindings.go`). To add a protocol,
write an adapter that implements the interfaces.

`rtspcam`'s `Driver` says where its cameras serve each stream, how their packets come (UDP
or TCP) and how often a login is checked. A login is one RTSP connection, and it carries
everything:

- Every stream is described on it at login. That authenticates the login (a refused
  password is a 401) and tells each stream's codec.
- The login is checked with `OPTIONS` requests. A check that fails ends the login.
- Every stream plays on it, as a session of its own. Only the stream's video is played.
  Its random access units go out with parameter sets ahead of them: the ones the stream's
  description carries, until the stream sends its own.

A login needs one connection only. That suits cameras that serve one connection at a
time, as some of BITVISION's do. A stream that is shut down leaves the login, and the
other stream on it, as they are.

`rtspcam/fakecam` is a command that runs a fake camera, one that the `BITVISION` driver
logs in to. It is for developing and testing a host end to end without the hardware. Both
of its streams play a recorded camera's H.265 at its frame rate, over and over. It serves
one connection at a time. It is built on `greendrake.nz/go/rtsp`'s `rtsptest`.

```
go install greendrake.nz/go/ipcam/rtspcam/fakecam@latest
fakecam -listen 127.0.0.1:8554 -user admin -pass ''
```

## Connection

`connection` keeps one login per camera. The login carries the camera's settings and
every stream.

- `Dial` logs in at once, and fails as the driver does. `Start` begins offline.
- `Run` keeps the camera connected until its context ends. It pushes the state as it
  starts, and on every change.
- A login that has ended is dialled again once a second, until the camera answers. A
  reboot is one thing that ends a login. A dial is given 5 seconds. There is no backoff,
  and neither time can be configured: the engine is made for cameras on the host's own
  network.
- A refused password leaves the camera offline, with the refusal, and it is dialled no
  more.
- Every read and write of the settings goes through the connection, checked by the core.
  When a write changes the camera's address or port, the camera is dialled at the new
  one from then on.

The state says:

- whether the camera is `online` or `offline`, and at which address;
- why the last dial failed;
- whether the camera has settings. That is its driver's to say, so it is known while the
  camera is offline too;
- whether a write is waiting for the camera to restart;
- each stream's codec, as it was at the last login, and whether the engine opens streams
  in that codec (`Cast`).

## Fleet

`fleet` is a host's cameras, by name. Each camera has its connection, which the host makes
with `Dial` or `Start` and the fleet runs. Each camera also has a `camstream.Source` of
each of its streams. All of them live as long as the fleet. The set of cameras is fixed
when the fleet is made: to add or remove one, the host makes a new fleet.

- A source opens its stream on the camera's login while it has subscribers. While the
  camera is offline, the open waits for it. An open that fails is logged, once for as
  long as it keeps failing for the same reason. The logger is `WithLogger`'s, or
  `slog.Default()` unless one is given.
- A camera can take its streams from elsewhere instead of from its own login: from
  another host that relays them, say. Its `StreamsFrom` says where. It is a function
  that makes one attempt at opening a stream. Each stream is then read from its camera
  once, however many hosts want it. The camera is still logged in to here, for its state,
  its settings and the codec check below. `Source` says whether a camera's streams are
  relayed in this way. A host that serves the streams uses that to keep relays from
  forming a circle.
- The engine opens H.265 streams only (`ipcam.Cast`). A stream in another codec is not
  opened. It has no `Source`, and its recorder waits for the next login, which is where a
  codec change applies.
- A camera can leave a request to stop a stream unanswered. That ends the login, because
  the DVRIP session gives up on the request, and every stream on that camera ends with
  the login. The redial restores the login within a second.
- `New` sets the fleet up. `Start` logs its cameras in, and pushes every change of a
  camera's state as the camera's name and its state. They are two steps so that whatever
  receives the states can be built with the fleet in hand, before the first push.
- Each stream named under a camera's `Record` is recorded for as long as the fleet runs.

## Recording

`record` subscribes to a stream. Whenever the subscription ends, it subscribes again at
once, and the new subscription waits for the source's next open. Each subscription starts
a new file. The file has an audio track if the subscription's monitor carries audio.
Sound is recorded only as G.711 A-law at 8 kHz: a stream with any other audio is recorded
without it.

Files are at `<dir>/<camera>/YYYY/MM/DD/HH-mm-ss.<stream>.mkv`. Each file starts at a key
frame: the first one that comes once the file before it is 10 minutes long. So a file
holds 10 minutes, and up to a key frame interval more.

- No frame falls between two files. Every video frame before the key frame that starts a
  file goes in the file before. So does the audio timed before that key frame. Audio
  arrives a little later than the video it belongs with, so the file before stays open
  for it. It closes when audio that belongs in the new file comes, when the next key
  frame comes, or when the recording ends.
- A file's header is complete from the start. The decoder configuration and the picture
  size come from the key frame the file starts at. That key frame has to carry the
  parameter sets: no file starts at one that does not. Each random access picture goes in
  as a key frame, with the parameter sets ahead of it.
- `record/mkv` writes each frame to the file as it comes, a block at a time, on the
  recorder's goroutine. A Cluster opens at each key frame. One also opens wherever a
  block's time, relative to the open Cluster's, would not fit in its 16 bits. What is
  known only at close is written into space reserved for it. The sizes of the Clusters
  and of the Segment have 8 bytes each. The duration, and the SeekHead's entry for the
  Cues, each have a Void of their size.
- Finishing a file writes the open Cluster's size, the index of the file's key frames
  (Cues) and the SeekHead's entry for that index, the file's duration, and the Segment's
  size. A file is finished once it has all it holds, and as the recorder stops with its
  context. So a process stopped by a signal it handles leaves every file complete.
- A file whose process is killed holds every frame up to the one being written. Its last
  Cluster and its Segment are of unknown size, which Matroska allows. Until the file is
  remuxed, it states no duration and has no index: the Voids fill those places, rather
  than values that would be wrong. Nothing is synced to disk. So a machine that goes down
  also loses what the system had yet to write out.
- A disk can stall a write for longer than the subscription's buffer of 256 frames
  (seconds of video and audio) lasts. The recorder is held up until its subscription is
  dropped for falling behind, as any subscriber's is. The recorder then subscribes again,
  and the next key frame starts a new file.
- When the writer fails at a file, on a full disk say, the file is closed as it is and
  the failure is logged. Every write to that file after the failure answers with the
  failure. The next key frame starts another file. When a file cannot be started, because
  its key frame carries no parameter sets say, that is logged once for as long as it
  fails for the same reason.
