# @greendrake/mse-video

Live camera video in the browser over Media Source Extensions: H.265 in fragmented MP4
and the camera's G.711 audio, every stream of a page over one WebSocket, recovering on
its own from stalls and dropped sockets. A framework-free core and a thin Vue component.
The server end is `greendrake.nz/go/webcast`, whose README has the socket's protocol.

```sh
bun add @greendrake/mse-video
```

## Component

`Video`, from `@greendrake/mse-video/vue` (`vue` is an optional peer, needed for this
entry only), takes props `hubUrl` (the stream hub's WebSocket URL), `streamKey` (which
stream on it, a key the server knows) and `audio` (whether the camera's sound plays). It
fills its box, stretched; a browser that cannot play the stream gets a line saying so
instead of a player. Its root carries the player's state as `data-state`, or
`unsupported`. A changed `streamKey` switches the stream in place.

The video element stays muted: the sound plays through the page's audio context, which
browsers keep silent until a user gesture. Call `unlockAudio()` from the click that turns
sound on.

## Core

- `streamCodec` is the content type of every stream a webcast server casts, H.265 in
  fMP4. `canPlay()` says whether the browser can play it, through Apple's
  `ManagedMediaSource` where it exists (Safari 17+, iPhones) and `MediaSource` otherwise.
- `MSEVideo(videoEl, hubURL, key, options?)` plays one stream into a video element;
  construct one only where `canPlay()` holds. `setKey` switches the stream, `setAudio`
  turns the sound on or off, `close` lets go of the stream and the element. Its `state`
  (`closed`, `opening`, `listening`, `playing`, `resetting`, `closing`) is emitted as
  `state` on each change; `error` reports a failure it is recovering from.
- Every player pointed at one hub URL shares one socket, which goes with the last of
  them. A dropped socket reconnects with exponential backoff, from `reopenDelayMs` (1 s)
  to `maxReopenDelayMs` (30 s) — `MSEVideoOptions.hub`, a `StreamHubOptions`, taken from
  whichever player opens the socket — and subscribes every stream again.

## Recovery

`MSEVideoOptions`:

- Playing with no packet for `noDataTimeoutMs` (3 s), the player rebuilds its
  MediaSource in place and asks the server to reset the stream, which restarts it from a
  fresh initialisation segment.
- Nothing within `startTimeoutMs` (9 s) of subscribing or resetting, a subscription not
  confirmed within `connectTimeoutMs` (10 s), a stream the server refuses or stops, or a
  MediaSource that cannot be built: the player drops the subscription and takes out a
  new one, backing off from `reopenDelayMs` (3 s) to `maxReopenDelayMs` (30 s) until a
  packet arrives.
- Playback is held near the newest frame.

## Audio

Audio arrives as frames of its own, G.711 A-law or µ-law, decoded here (no browser media
pipeline plays G.711) and scheduled against the video element's clock: each frame plays
when the video reaches it, and nothing plays while the video stands still.
