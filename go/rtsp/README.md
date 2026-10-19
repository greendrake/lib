# greendrake.nz/go/rtsp

An RTSP 1.0 client (RFC 2326), as thin as playing an IP camera's H.265 stream takes.
`ipcam/rtspcam` is its user.

```sh
go get greendrake.nz/go/rtsp
```

| Package | What |
|---|---|
| `rtsp` | The client (`Conn`), the description of a stream (`Description`), H.265 put back together from its RTP packets (`H265`), and RTP timestamps as time (`Clock`) |
| `rtsp/rtsptest` | A camera for tests (`Serve`), or for a fake camera to run on (`Listen`): streams described, set up and played over UDP or interleaved, authenticated with Digest |
| `rtsp/internal/wire` | Messages and their fields, interleaved packets, RTP packets and their pairs of UDP ports, and Digest responses, for both |
| `rtsp/cmd/rtsp` | A command that describes a camera's streams and plays one's video, printing what arrives each second |

## What it does

- One TCP connection a `Conn`, to port 554 where the address names none, its requests
  going out one at a time, each answered within 3 seconds or the connection ends.
- Authentication as the server asks, once it has refused a request: Digest (MD5, with or
  without qop `auth`) where offered, Basic otherwise.
- `Options`, as a probe; `Describe`, reading of the SDP what playing takes: each media's
  type, control URL, first format's encoding, clock rate and parameters, and a video's
  frame rate, from RFC 4566's `framerate` or BITVISION's `videoinfo`; `Video` is its first
  video media.
- `Play`: one media set up, then played, its packets coming by the `Transport` asked for:
  - `UDP`, as cameras send them: to a pair of the client's ports, RTP's and RTCP's. Once
    set up, the client sends a packet from each to the server's of its kind, so that a NAT
    between lets the server's in; a NAT has to keep the ports' numbers, which the server
    sends to. RTP packets from elsewhere are dropped. The RTP port holds up to 4 MiB
    unread, a key frame's burst several times over, as far as `net.core.rmem_max` allows.
  - `TCP`: interleaved on the connection.

  Its RTP packets arrive on `Packets`; RTCP is dropped. The session is kept alive with
  `OPTIONS` every half of its timeout, and `Close` tears it down.
- `H265` puts access units back together from single NAL unit packets, aggregation
  packets and fragmentation units (RFC 7798), a unit ending at its marked packet; a unit
  a packet of which went missing is dropped. `ParameterSets` are the VPS, SPS and PPS a
  description carries, an Annex-B start code some cameras put ahead of one left off.
  A media's `Clock` turns its 32-bit RTP timestamps into time since the first frame's,
  across their wrapping. Timestamps that stand still, a frame's the same as the one
  before's (a BITVISION camera's extra stream's are all 0), are replaced from then on by
  the frame rate: each frame one after the last, and an error where the description gives
  no frame rate.

## `cmd/rtsp`

`cmd/rtsp` exercises it against a camera:

```
go install greendrake.nz/go/rtsp/cmd/rtsp@latest
rtsp -addr 192.0.2.132 -user rtsp -pass secret describe /0
rtsp -addr 192.0.2.132 -user rtsp -pass secret play -for 30s /0
```

`play` prints, each second, the packets that arrived and those that went missing ahead
of them, the access units put together (random access ones among them), their bytes, and
the latest unit's time. `-tcp` plays interleaved on the connection instead of over UDP.

## What it leaves out

Multicast, RTCP reports, reordering, TLS, recording, and the server side. `rtsptest` is the
server for tests and fake cameras alone.
