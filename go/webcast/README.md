# greendrake.nz/go/webcast

Live camera video for browsers, without re-encoding. A camera's H.265 is repackaged as
fragmented MP4 and sent frame by frame, with the camera's G.711 audio beside it. Any
number of streams share one WebSocket, and a stream is read from its camera only while
someone watches.

```sh
go get greendrake.nz/go/webcast
```

## Why

An IP camera serves its streams over RTSP or a vendor's protocol, and a browser speaks
neither. The usual ways of putting a server between the two each cost something. HLS
delivers video in segments, so its viewers run seconds behind the camera. WebRTC is
quick, but takes signalling and connection negotiation, and where a browser does not take
H.265 over it, the server has to re-encode. Re-encoding costs processor time for every
stream.

webcast only repackages. Its host — the program that mounts its handler — gives it the
streams. Each H.265 frame goes out as a fragment of MP4, which a page feeds to the
browser's decoder through Media Source Extensions. No frame is decoded or encoded on the
way, and none waits for a segment to fill.

## Trade-offs

- **H.265 only, and the browser has to decode it.** Nothing is re-encoded, so there is no
  fallback: a browser that cannot play H.265 through Media Source Extensions shows no
  picture. The bit rate is the camera's too, for every viewer, whatever their connection.
- **A protocol of its own.** No standard player plays these streams. The page needs a
  client written for the protocol below: one that splits the socket into its channels,
  feeds the video to Media Source Extensions, and decodes the G.711 itself. Where the
  viewers have to be ordinary players, serve HLS or WebRTC instead.
- **Live only.** Nothing is kept, so a viewer cannot go back. A viewer starts at the
  stream's next key frame: the picture appears up to a key frame interval after
  subscribing.
- **No access control per stream.** The handler checks a browser's origin and nothing
  else, and `Lookup` is given a key, not the request. So authentication goes in front of
  the handler, and whoever it admits can ask for any key the host serves.

## Overview

The host gives the handler its streams as `greendrake.nz/go/camstream` sources: a
camera's frames, whatever protocol brought them. The rest is what a page of camera tiles
needs:

- **One socket for the page.** A browser opens its WebSockets to one server one handshake
  at a time. A socket for each tile would make a page of tiles wait in that queue. So
  every stream a page watches shares one socket.
- **A camera works only while watched.** A stream is opened on its camera for its first
  subscriber, and closed after its last.
- **No viewer waits for a camera or for another viewer.** Subscribing never waits for the
  camera. A viewer whose connection cannot keep up is dropped, rather than holding the
  others up.
- **Sound.** The camera's G.711 audio comes beside the video, as the camera encoded it,
  to the viewers that turn it on.
- **Recovery without a new socket.** A viewer whose decoder has stalled asks for a reset,
  and the stream starts afresh on the same channel.
- **Relays.** Another host gets a stream from this one, rather than from the camera. So a
  stream is read from its camera once, however many hosts serve it.

## API

`Handler(ctx, lookup, opts...)` is the `http.Handler` that serves the socket. The host
chooses where to mount it, as a `GET`. `lookup` is a `Lookup`: it finds the host's sources
by key. The options:

| Option | What |
|---|---|
| `WithOriginPatterns` | The origins, beside the endpoint's own, whose pages may open the socket (`path.Match` patterns, of the origin's host alone or with its scheme); any other is refused at the handshake. A client that sends no `Origin`, such as another host relaying, is not checked |
| `WithLogger` | Where a refused handshake and a stream that cannot be sent are logged; `slog.Default()` unless given |

`Relay` is a `camstream.Monitor` over another host's socket.

The fMP4 muxer derives from [go2rtc](https://github.com/AlexxIT/go2rtc)'s `pkg/iso`,
`pkg/mp4` and `pkg/core`, copyright (c) 2022 Alexey Khit, under the MIT licence in
[`internal/muxer/LICENSE`](internal/muxer/LICENSE).

## Protocol

All streams a browser watches share one WebSocket. Browsers keep at most one connection
per server in the CONNECTING state (RFC 6455 4.1). So with a socket per stream, a page of
tiles would open its sockets one handshake after another.

The client controls the socket with JSON text frames:

```
-> {"op":"sub","id":7,"key":"<key>"}   <- {"op":"subbed","id":7,"ch":3}
                                          {"op":"error","id":7,"key":"..."}
-> {"op":"unsub","ch":3}
-> {"op":"reset","ch":3}
-> {"op":"audio","ch":3,"on":true}
                                          {"op":"gone","ch":3}
```

- Keys are the host's own: `Handler` takes a `Lookup` of the sources by key.
- `subbed` names the channel the stream will arrive on.
- `error` answers a key the lookup finds no source for: a stream that is not served, or
  one that has nothing to give right now. It also answers a `sub` on a socket that has no
  channel left.
- `gone` says a channel has stopped, and the viewer should subscribe again. A channel
  stops when the camera stream behind it ends. It also stops when the stream sends what
  cannot be repackaged: a key frame without the parameter sets that describe the stream,
  say. That case is logged as well.
- `audio` turns the channel's audio on or off. It starts off.

Media arrives as binary frames of `[channel byte][kind byte][payload]`:

- Kind 0 is video: an fMP4 initialization segment, then media segments of one frame
  each. The timeline starts at 0 with the key frame the initialization segment was made
  from, and runs on the camera stream's own clock.
- Kind 1 is a frame of the camera's audio, sent while the channel's audio is on. Its
  payload is `[u32 ms][u16 sample rate][u8 codec][samples]`, big-endian. The frame plays
  that many milliseconds into the video's timeline. Codec 0 is G.711 A-law, and 1 is
  µ-law. The page decodes it itself, since Media Source Extensions plays no G.711.

A frame of just the channel byte acknowledges a `reset`. The next payload on that channel
starts a fresh initialization segment and a fresh timeline. That is how a viewer recovers
from a stalled decoder without dropping the socket.

Subscribing never waits for the camera. The stream's source connects to the camera on its
own. So a camera that is slow or offline leaves its own subscription without a picture,
and affects nothing else.

All the streams of a socket share one queue of 256 messages. When the queue fills, or a
write takes more than 5 seconds, the socket is closed with every stream on it, and the
client has to connect again. One socket carries at most 256 streams.

### Relays

Another host gets a stream from this one, rather than from the camera, with a raw
subscription: `{"op":"sub","id":7,"key":"<key>","raw":true}`. A raw subscription is
answered, unsubscribed and reported `gone` as any other is.

From the stream's first key frame on, every frame arrives as it is, video and audio, in a
binary frame of kind 2. Its payload is `[u8 flags][frame]`, with flag 1 set when the
stream carries audio. The frame is
`[u8 kind][u8 codec][u8 keyframe][i64 PTS ns][i64 duration ns][u32 sample rate][data]`,
big-endian. Kind 1 is video, and 2 is audio. Codec 1 is H.265, 2 is G.711 A-law, and 3 is
µ-law.

A raw channel takes no `reset` or `audio`. `Lookup` is told whether a subscription is raw,
so a host can serve a stream to viewers and refuse to relay it.

`Relay` is the other end: a `camstream.Monitor` over a raw subscription. It has a socket
of its own, dialled at the serving host's `ws://` or `wss://` URL.

- `Relay` opens once the stream's first frame has come. That frame says whether the
  stream carries audio. Until it comes, `Relay` waits for as long as its context lets it.
  The serving host sends nothing while its camera is offline, and that is correct. If the
  serving host itself becomes unreachable meanwhile, the socket's TCP keep-alives detect
  it.
- Once it plays, the monitor ends with the subscription: on `gone`, when the socket
  closes, or when no message comes for its quiet period. The quiet period is
  `WithQuiet`'s, 10 seconds unless given. A serving host that is still running ends a
  stream that has gone quiet sooner than that, and says so.
- `ShutDown` closes the socket. That ends the subscription on the serving host, whose
  source then closes the stream once it has no other subscriber.

When the monitor behind a viewer's channel ends, the viewer is told `gone`, and it
subscribes again. A `camstream.Source` ends its subscriptions with their monitor.
