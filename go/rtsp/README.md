# greendrake.nz/go/rtsp

An IP camera's H.265 stream, played in a Go program: an RTSP 1.0 client (RFC 2326) with
only what that needs. It connects, authenticates, plays any number of a camera's streams
on one connection, and reassembles their H.265 frames. No dependencies.

```sh
go get greendrake.nz/go/rtsp
```

## Why

IP cameras serve their live streams over RTSP. The protocol is large. A camera implements
a small part of it, and not always as the standard says. A program that records or relays
cameras needs that part, done the way cameras do it.

Running ffmpeg takes a process per stream, and its output has to be parsed back into
frames. A general RTSP library has both ends of the protocol, more transports and many
codecs: a program that only plays cameras uses a small part of it. This client is only
that part. It allows for what cameras do: some serve one connection at a time, and some
send timestamps that do not advance.

## Trade-offs

- **Only H.265 is reassembled into frames.** A session delivers the RTP packets of
  whichever media it is asked to play. Only `H265` turns packets into frames. A program
  that wants H.264, or audio, unpacks those payloads itself.
- **A lost packet costs its frame.** Packets are taken in the order they arrive: there is
  no reordering. A packet out of order counts as a loss, and the access unit being
  reassembled is dropped. On a network that loses or reorders UDP packets, play with
  `TCP` instead.
- **A client for playing cameras, and nothing else.** TLS, RTCP, multicast, recording and
  the server side are left out. `rtsptest` is a server for tests and fake cameras only.
  For any of these, use a general RTSP library, such as gortsplib.

## Packages

| Package | What |
|---|---|
| `rtsp` | The client (`Conn`), a stream played on it (`Session`), the description of a stream (`Description`), H.265 reassembled from its RTP packets (`H265`), and RTP timestamps as time (`Clock`) |
| `rtsp/rtsptest` | A camera for tests (`Serve`), or for a fake camera to run on (`Listen`): streams described, set up and played over UDP or interleaved, each in a session of its own, any number on a connection, authenticated with Digest; with `Serial`, one connection served at a time; with a stream's `Still`, timestamps that do not advance |
| `rtsp/internal/wire` | Messages and their fields, interleaved packets, RTP packets and their pairs of UDP ports, and Digest responses, for both the client and `rtsptest` |
| `rtsp/cmd/rtsp` | A command that describes a camera's streams and plays their video, printing what arrives each second |

## What it does

A client connects, describes a stream, and plays its video:

```go
c, err := rtsp.Dial(ctx, "192.0.2.132", "user", "secret")
if err != nil {
	return err
}
defer c.Close()

d, err := c.Describe(ctx, "/0")
if err != nil {
	return err
}
video, found := d.Video()
if !found {
	return errors.New("the stream has no video")
}
session, err := c.Play(ctx, d, video, rtsp.UDP)
if err != nil {
	return err
}
var units rtsp.H265
for packet := range session.Packets() {
	if nalus, timestamp := units.Push(packet); nalus != nil {
		handle(nalus, timestamp) // one access unit, and its RTP timestamp
	}
}
return session.Err()
```

### The connection

A `Conn` is one TCP connection, without TLS. `Dial` connects to port 554 where the
address names no port. Requests go out one at a time. Each is answered within 3 seconds,
or the connection ends. A request whose context ends is abandoned, and the connection
stays open for the next request. If the abandoned request's answer comes, it is dropped.
A play's setup is the exception, described below.

Every stream a client plays shares that one connection. That is for cameras that serve
one connection at a time, as some of BITVISION's do. Such a camera accepts another
connection and leaves it unanswered. It keeps that connection queued after the client
has given up on it.

### Authentication

The client authenticates as the server asks, once the server has refused a request. It
answers with Digest (MD5, with or without qop `auth`) where the server offers it, and
with Basic otherwise. With Basic, the password crosses the network unencrypted.

### Describing

`Options` asks the server what it supports of a stream, which checks that the connection
still answers. `Describe` reads from a stream's SDP what playing needs: each media's
type, its control URL, and its first format's encoding, clock rate and parameters. For a
video it also reads the frame rate, from RFC 4566's `framerate` or BITVISION's
`videoinfo`. `Video` is the description's first video media.

### Playing

`Play` sets one media up as a `Session` of its own, then plays it. The packets come by
the `Transport` asked for:

- `UDP`, as cameras send them. The packets go to a pair of the session's ports, RTP's
  and RTCP's. Once the session is set up, the client sends a packet from each port to the
  server's port of the same kind, so that a NAT between them lets the server's packets
  in. The server sends to the ports' numbers, so the NAT has to keep those numbers. Where
  it does not, or where a firewall stops the packets, nothing arrives, and the session
  reports no error. Play with `TCP` there. RTP packets from any address but the server's
  are dropped. The RTP port holds up to 4 MiB of unread packets, which is several times a
  key frame's burst, as far as `net.core.rmem_max` allows.
- `TCP`: on the RTSP connection itself, which RTSP calls interleaved. The packets take
  the lowest pair of channels no other session has. If the server answers with another
  session's channel, the play fails.

Once set up, a session stays on the server until it is torn down or times out, and only
the setup's answer names it. So the client waits for that answer whatever the context
does, up to the 3 seconds. If the context ends meanwhile, the play tears the session down
rather than leave it on the server, and returns only then.

The session's RTP packets arrive on its `Packets`. RTCP that arrives is dropped, and the
client sends no RTCP reports of what it received. A session whose reader falls behind
loses packets rather than hold up the connection's other sessions. Interleaved, it loses
them once it holds 256 unread. Over UDP, it loses them once its port's buffer is full
too.

The session is kept alive with an `OPTIONS` request that names it, sent every half of its
timeout. `Close` tears that session down alone. `Conn.Close` tears down every session on
the connection.

### H.265 and time

`H265` reassembles access units from RTP packets: single NAL unit packets, aggregation
packets and fragmentation units (RFC 7798). A unit ends at the packet that has the RTP
marker set. A unit that lost a packet is dropped.

`ParameterSets` are the VPS, SPS and PPS that a description carries. Some cameras put an
Annex-B start code ahead of a set. It is removed.

A media's `Clock` turns its 32-bit RTP timestamps into time since the first frame, and
follows them when they wrap around. Some timestamps do not advance: a frame's timestamp
is the same as that of the frame before. On a BITVISION camera's extra stream, every
timestamp is 0. From the first such frame on, the frame rate replaces the timestamps:
each frame comes one frame period after the last. Where the description gives no frame
rate, the clock returns an error.

Each `Clock` counts from its own media's first frame. No RTCP is read, so nothing relates
one media's clock to another's.

## `cmd/rtsp`

`cmd/rtsp` runs the client against a camera:

```
go install greendrake.nz/go/rtsp/cmd/rtsp@latest
rtsp -addr 192.0.2.132 -user rtsp -pass secret describe /0
rtsp -addr 192.0.2.132 -user rtsp -pass secret play -for 30s /0 /1
```

`play` plays every path given at once, each in a session of its own on the one
connection. Each second it prints, for each stream, the packets that arrived, the packets
that went missing ahead of them, the access units reassembled and how many of those were
random access units, their bytes, and the latest unit's time. `-tcp` plays interleaved on
the connection instead of over UDP.
