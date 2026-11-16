# @greendrake/mse-video

Low-latency live camera video in the browser, with the camera's sound: a player for the
streams that a `greendrake.nz/go/webcast` server sends. It has a core that needs no
framework, and a thin Vue component.

```sh
bun add @greendrake/mse-video
```

The package ships its source as it is, so the consuming app's bundler has to compile
TypeScript, as Vite does.

## Why

An IP camera encodes H.265 video and G.711 audio. A browser plays neither as the camera
sends them, and the usual ways to bridge the gap each cost something. HLS plays
everywhere, but runs seconds behind the camera. WebRTC is quick, but takes signalling
and connection negotiation, and browsers that do not take H.265 over it leave the server
re-encoding every stream. Re-encoding on the server, to anything, costs a processor per
stream.

This player takes the camera's own H.265, which the server only repackages as fragmented
MP4, and feeds it to the browser's decoder through Media Source Extensions. Nothing is
re-encoded, and playback is held a few tenths of a second behind the newest frame.

## Trade-offs

- **H.265 only.** There is no fallback codec. A browser that cannot decode H.265 through
  Media Source Extensions cannot play a stream, and `canPlay()` says so beforehand.
- **One server protocol.** The socket speaks the protocol of
  `greendrake.nz/go/webcast`, whose README defines it. This is not a general player:
  it plays no HLS, DASH or WebRTC.
- **Live only.** There is no seeking, and nothing is kept: a viewer cannot go back.

## Overview

The rest of the package is what a page of camera tiles needs to keep running unattended:

- **One socket for the page.** Every stream of a page shares one WebSocket, so a page of
  tiles pays for one handshake, not one per tile.
- **Recovery without a reload.** A stalled stream is rebuilt in place, and a dropped
  socket reconnects and subscribes every stream again.
- **Constant memory.** Video that has played is removed from the buffer, so a tile can
  run for days.
- **Sound.** No browser plays G.711, so the package decodes it and plays it in step
  with the picture.

## Component

`Video`, from `@greendrake/mse-video/vue` (`vue` is an optional peer, needed for this
entry only), takes props `hubUrl` (the WebSocket URL the server serves its streams at),
`streamKey` (which stream on it, a key the server knows) and `audio` (whether the
camera's sound plays). It fills its box, stretched. A browser that cannot play the
stream shows a line saying so instead of a player. The component's root element carries
the player's state as `data-state`, or `unsupported`. A changed `streamKey` switches the
stream in place.

The video element stays muted: the sound plays through the page's audio context, which
browsers keep silent until a user gesture. Call `unlockAudio()` from the click that turns
sound on.

## Core

- `streamCodec` is the content type of every stream a webcast server sends, H.265 in
  fMP4. `canPlay()` says whether the browser can play it, through Apple's
  `ManagedMediaSource` where it exists (Safari 17+, iPhones) and `MediaSource` otherwise.
- `MSEVideo(videoEl, hubURL, key, options?)` plays one stream into a video element;
  construct one only where `canPlay()` holds. `setKey` switches the stream, `setAudio`
  turns the sound on or off, `close` releases the stream and the element. Its `state`
  (`closed`, `opening`, `listening`, `playing`, `resetting`, `closing`) is emitted as
  `state` on each change; `error` reports a failure it is recovering from. Video it has
  played is removed from its `SourceBuffer` up to the key frame the picture on screen
  depends on, so its memory use does not grow however long it plays.
- Every player given the same `hubURL` shares one socket, which closes with the last of
  them. A dropped socket reconnects with exponential backoff, from `reopenDelayMs` (1 s)
  to `maxReopenDelayMs` (30 s), and subscribes every stream again. Those two times are
  in `MSEVideoOptions.hub`, a `StreamHubOptions`, and are taken from whichever player
  opens the socket.

## Recovery

The times below are `MSEVideoOptions`, each with its default:

- When the player is playing and no packet comes for `noDataTimeoutMs` (3 s), it
  rebuilds its MediaSource in place and asks the server to reset the stream, which
  restarts it from a fresh initialisation segment.
- The player drops the subscription and makes a new one in four cases: nothing comes
  within `startTimeoutMs` (9 s) of subscribing or resetting; the subscription is not
  confirmed within `connectTimeoutMs` (10 s); the server refuses or stops the stream; a
  MediaSource cannot be built. It backs off from `reopenDelayMs` (3 s) to
  `maxReopenDelayMs` (30 s) until a packet arrives.
- Playback is held near the newest frame. After a stall the player speeds up slightly,
  or jumps ahead, to get back to it.

## Audio

Audio arrives as frames of its own, in G.711 A-law or µ-law. The package decodes them,
since no browser plays G.711, and schedules them against the video element's clock: each
frame plays when the video reaches it, and nothing plays while the video is not
advancing.
