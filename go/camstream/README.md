# greendrake.nz/go/camstream

A camera's stream, whatever brings it and wherever it goes: its frames, a connected
stream, and a stream fanned out to its subscribers while it has any. Protocol adapters
produce it (`greendrake.nz/go/ipcam`'s, over DVRIP and RTSP); `greendrake.nz/go/webcast`
casts it to browsers, and `ipcam`'s recorder writes it to files.

```sh
go get greendrake.nz/go/camstream
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

A `Frame` is one video frame, an Annex-B access unit of H.265, or one audio frame of
G.711 A-law or µ-law samples. Its `PTS` is when it is due on its stream's own clock; a
video frame has a `Duration` and says whether it is a `Keyframe`, an audio frame has a
`SampleRate`.

## Sources

A `Source` is one stream fanned out to its subscribers: a browser's view of it, a
recorder, another host relaying it. `Subscribe` takes a subscription until its context
ends; its frames arrive on a channel of their own, which closes when the subscription
ends.

- The stream is opened, through the `Open` the source is made with, on the first
  subscriber, and closed after the last. A subscriber taken while it is open joins it; one
  taken otherwise waits for the next open.
- A failed open leaves the subscriptions waiting and is tried again, backing off from a
  second, doubling, to five; an open that succeeds resets the backoff, which subscribers
  coming and going do not.
- A monitor that ends ends its subscriptions, whose subscribers subscribe again.
- Each subscriber has a buffer of its own, and one that falls a buffer behind is dropped
  rather than holding up the others.
- One goroutine owns the monitor, so the stream is open at most once at a time: it opens
  again only once the last monitor has been shut down, since a camera cannot tell two
  monitors of one stream on a shared login apart. A shutdown runs off that goroutine,
  so a camera slow to answer it holds nothing else up.

A source runs until its context ends, and `Done` says when it has, its monitor shut
down.

## The recording

`camstreamtest.Recording` is 4.2 s of an XM530 camera's extra stream, 800×448 H.265 at
15 frames a second with 8 kHz A-law audio, embedded in the package in a framing of its
own. `greendrake.nz/go/ipcam` writes it from the DVRIP recording `greendrake.nz/go/dvrip`'s
`dvriptest` plays (its `dvripcam/internal/genrecording`), and its tests check that the two
agree.
