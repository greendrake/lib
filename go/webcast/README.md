# greendrake.nz/go/webcast

Live camera streams to browsers: H.265 in fMP4 with the camera's G.711 audio beside it,
any number of streams over one WebSocket, pulled off the cameras only while someone
watches. The streams are `greendrake.nz/go/camstream` sources; `@greendrake/mse-video`
is the browser end.

```sh
go get greendrake.nz/go/webcast
```

`Handler` serves the socket, the host mounting it where it chooses (a `GET`), over a
`Lookup` of its sources by key. Its options:

| Option | What |
|---|---|
| `WithOriginPatterns` | The origins, beside the endpoint's own, whose pages may open the socket (`path.Match` patterns, of the host alone or with the scheme); any other is refused at the handshake. A client sending no `Origin`, another host relaying, is not checked |
| `WithLogger` | Where a refused handshake and a stream that cannot be cast are logged; `slog.Default()` unless given |

`Relay` is a `camstream.Monitor` over another host's socket.

The fMP4 muxer derives from [go2rtc](https://github.com/AlexxIT/go2rtc)'s `pkg/iso`,
`pkg/mp4` and `pkg/core`, copyright (c) 2022 Alexey Khit, under the MIT licence in
[`internal/muxer/LICENSE`](internal/muxer/LICENSE).

## Protocol

All streams a browser watches share one WebSocket. Browsers keep at most one connection
per host in the CONNECTING state (RFC 6455 4.1), so a socket per stream would make a page
of tiles pay one serialized handshake per tile.

The client drives the socket with JSON text frames:

```
-> {"op":"sub","id":7,"key":"<key>"}   <- {"op":"subbed","id":7,"ch":3}
                                          {"op":"error","id":7,"key":"..."}
-> {"op":"unsub","ch":3}
-> {"op":"reset","ch":3}
-> {"op":"audio","ch":3,"on":true}
                                          {"op":"gone","ch":3}
```

Keys are the host's own: `Handler` takes a `Lookup` of the sources by key. `subbed`
names the channel the stream will arrive on. `error` answers a key the lookup finds no
source of: one not served, or whose stream has nothing to give right now. `gone` says a
channel has stopped, because the camera stream behind it went away or sent what cannot be
cast (a key frame without the parameter sets that describe the stream, say, which is also
logged), and the viewer should subscribe again. `audio` turns the channel's audio on or off; it starts off.

Media arrives as binary frames of `[channel byte][kind byte][payload]`:

- Kind 0 is video: an fMP4 initialization segment, then media segments of one frame
  each. The timeline starts at 0 with the key frame the initialization segment was made
  from and runs on the camera stream's own clock.
- Kind 1 is a frame of the camera's audio, while it is on:
  `[u32 ms][u16 sample rate][u8 codec][samples]`, big-endian, due that many milliseconds
  into the video's timeline; codec 0 is G.711 A-law, 1 is µ-law. Browsers decode it
  themselves, since MSE plays no G.711.

A frame of just the channel byte acknowledges a `reset`: the next payload on that
channel starts a fresh initialization segment and timeline, which is how a viewer
recovers from a stalled decoder without dropping the socket.

Subscribing never waits for the camera. The stream's source connects to it on its own,
so a camera that is slow or offline costs its own subscription a blank picture and
nothing else.

### Relays

Another host takes a stream off this one, rather than off the camera, with a raw
subscription: `{"op":"sub","id":7,"key":"<key>","raw":true}`. It is answered, unsubscribed
and reported `gone` as any other. From the stream's first key frame on, every frame, video
and audio, arrives as it is, kind 2: `[u8 flags][frame]`, flag 1 set when the stream
carries audio, and the frame
`[u8 kind][u8 codec][u8 keyframe][i64 PTS ns][i64 duration ns][u32 sample rate][data]`,
big-endian: kind 1 video, 2 audio; codec 1 H.265, 2 G.711 A-law, 3 µ-law. A raw channel
takes no `reset` or `audio`. `Lookup` is told which a subscription is, so a host can serve
a stream to viewers and refuse to relay it.

`Relay` is the other end: a `camstream.Monitor` over a raw subscription, on a socket of
its own, dialled at the host's `ws://` or `wss://` URL. It opens once the stream's first
frame has come, which says whether it carries audio, waiting for as long as its context
lets it: the host is rightly silent while its camera is away, and a host gone meanwhile is
found by the socket's TCP keep-alives. Once it plays, it ends with the subscription:
`gone`, the socket closing, or its quiet window (`WithQuiet`, 10 seconds unless given)
without a message, since a host that is still there ends a stream gone quiet sooner and
says so. `ShutDown` closes the socket, which ends the subscription on the host, so its
source closes the stream once nothing else wants it.

A viewer is told `gone` when the monitor behind its channel ends (a `camstream.Source`
ends its subscriptions with their monitor), and subscribes again.
