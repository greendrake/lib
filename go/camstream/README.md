# greendrake.nz/go/camstream

The types shared by the code that brings a camera's stream in and the code that sends it
on. There is one type for a frame and one interface for a connected stream. There is also
a fan-out: it shares one stream among its subscribers — viewers, recorders, relays — and
keeps the camera connected only while it has any.

```sh
go get greendrake.nz/go/camstream
```

## Why

A program that serves camera streams has two kinds of code. One brings a stream in, over
whatever protocol the camera speaks. The other sends it on: to a browser, to a file, to
another machine. Without types they share, every consumer has to know every protocol's
client. `Frame` and `Monitor` are those types.

One camera also has many consumers. If each opens the stream for itself, the camera sends
it once for each, and a camera cannot tell apart two openings of one stream on a shared
login. If one loop writes each frame to every consumer, one slow consumer stalls the
rest. A `Source` opens the stream once, for its first subscriber, closes it after the
last, and drops a subscriber that falls behind.

## Trade-offs

- **A slow subscriber is dropped, not waited for.** A subscriber may fall 256 frames
  behind. Past that its subscription ends, and it has to subscribe again, having missed
  frames. The size of the buffer is fixed.
- **The source keeps no frames.** A subscriber that joins an open stream gets its frames
  from that moment on. No key frame is kept for it, so a decoder has to wait for the next
  one. And the stream closes as soon as its last subscriber leaves: a viewer who leaves
  and comes back a moment later waits for a new open.
- **H.265 video and G.711 audio only.** A `Frame` has no other codec.

## Overview

A protocol's client is wrapped once as a `Monitor`: a connected stream that yields
`Frame`s. Everything that sends a stream on is written against those two types, and works
with any camera.

A `Source` is the fan-out:

- **Open on demand.** The stream is opened on the first subscriber and closed after the
  last. A camera nobody is watching is not connected.
- **Open once.** One goroutine owns the stream, and every change of its subscribers goes
  through that goroutine. However subscribers come and go, the stream is open at most
  once at a time.
- **No subscriber holds up another.** Each has a buffer of its own. One that falls a
  buffer behind is dropped.
- **A camera that cannot be reached is tried again.** A failed open is tried again,
  backing off from one second to five, while the subscribers wait.
- **A slow camera delays nothing else.** Opening and shutting down run outside the
  source's goroutine, so subscribers still come and go while the camera is slow to
  answer.

```go
// open is a camstream.Open: one attempt at connecting the camera.
source := camstream.NewSource(ctx, open)

// A viewer, a recorder or a relay:
sub := source.Subscribe(viewerCtx)
for frame := range sub.Frames() {
	send(frame) // frame.Kind, frame.Codec, frame.PTS, frame.Data
}
// The subscription has ended. Subscribe again to go on.
```

## Packages

| Package | What |
|---|---|
| `camstream` | `Frame`, video or audio, with its codec and timing; `Monitor`, a connected stream, and `Open`, an attempt at connecting one; `Source`, a stream fanned out to its subscribers |
| `camstream/h265` | An H.265 stream's description from a key frame's NAL units: its decoder configuration record and picture size |
| `camstream/camstreamtest` | For tests: a `Monitor` in memory, a camera that opens the monitors handed to it, a camera's recorded stream, and H.265 fixtures (a NAL unit of a type, an access unit in Annex-B) |

`camstream/h265` derives in part from [go2rtc](https://github.com/AlexxIT/go2rtc)'s
`pkg/h265`, copyright (c) 2022 Alexey Khit, under the MIT licence in
[`h265/LICENSE`](h265/LICENSE). NAL unit types are mediacommon's
(`github.com/bluenviron/mediacommon/pkg/codecs/h265`).

## Frames

A `Frame` is one video frame or one audio frame. A video frame is an Annex-B access unit
of H.265. An audio frame is G.711 samples, A-law or µ-law. A frame's `PTS` is its
presentation time on its stream's own clock. A video frame also has a `Duration`, and
says whether it is a `Keyframe`. An audio frame has a `SampleRate`.

## Monitors

A `Monitor` is a connected stream. `Next` waits for the stream's next frame. `HasAudio`
reports whether the camera puts audio in the stream. `ShutDown` disconnects, which is
also what unblocks a `Next` that is waiting on the camera.

An `Open` is a function that makes one attempt at opening a stream, and returns its
`Monitor`. Its context bounds the attempt only, and ends early once the source no longer
needs the stream. The monitor lasts until it is shut down.

## Sources

A `Source` is one stream fanned out to its subscribers: a browser's view of it, a
recorder, another program relaying it. `Subscribe` takes a subscription, which lasts
until its context ends. The subscription's frames arrive on a channel of its own,
`Frames`, which closes when the subscription ends.

- The source is made with an `Open`. It opens the stream through it on the first
  subscriber, and closes the stream after the last. A subscriber that arrives while the
  stream is open joins it. One that arrives at any other time waits for the next open.
- A failed open leaves the subscriptions waiting, and is tried again. The wait between
  tries starts at a second and doubles, up to five. An open that succeeds resets the
  wait. Subscribers coming and going do not.
- When a monitor ends, its subscriptions end, and their subscribers subscribe again. The
  source opens the stream again only for those who do. Handling the gap is the
  subscriber's job: a recorder starts a new file, say.
- Each subscriber has a buffer of its own, of 256 frames. One that falls a buffer behind
  is dropped rather than holding up the others.
- Every subscriber receives the same `*Frame`, not a copy, so none of them may change it.
- One goroutine owns the monitor, so the stream is open at most once at a time. It opens
  again only once the last monitor has been shut down, because a camera cannot tell two
  monitors of one stream on a shared login apart. A shutdown runs outside that goroutine,
  so a camera that is slow to answer it delays nothing else.

A source runs until its context ends. `Done` says when it has stopped, with its monitor
shut down.

## The recording

`camstreamtest.Recording` is 4.2 s of an XM530 camera's extra stream: 800×448 H.265 at
15 frames a second, with 8 kHz A-law audio. It is embedded in the package in a file
format of the package's own, which `WriteRecording` writes.
