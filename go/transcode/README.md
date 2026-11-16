# greendrake.nz/go/transcode

Audio and video transcoding by ffmpeg, done on whichever machine has the encoder for it. One process, the **master**,
has a media file and wants it transcoded. Another, the **slave**, runs ffmpeg with the encoders its machine has. The
source is never sent whole: the slave reads from the master only the byte ranges ffmpeg asks for, and sends the output
back, all over one connection that the master opens.

```sh
go get greendrake.nz/go/transcode
```

## Why

The program that holds the media is often the wrong place to encode it: a container or a small server with no hardware
encoder. Encoding in software there is slow, and takes the processor from everything else. The machine that has the
encoder — an Apple silicon Mac with VideoToolbox, say — sits elsewhere.

The usual way to use that machine is to hand it the file, through a shared filesystem, or through shared storage and a
job queue. That moves every source whole, even when only its first seconds are wanted. And the encoding machine has to
reach the storage and the queue, or be reachable itself. This library needs one connection, which the master opens.
Over it the slave reads only the bytes of the source that ffmpeg asks for, and returns the output.

## Trade-offs

- **The source must be seekable.** A source is an `io.ReaderAt` of known size: a file, an object in a blob store. A
  live stream or a pipe cannot be one.
- **A job lasts only as long as its connection.** There is no job store and no resuming. If the connection drops, or
  either process dies, the job is gone, and running it again is up to the caller. The master stays connected for as
  long as the job takes.
- **The output arrives when it is complete.** The slave writes the output to its own disk and sends it once ffmpeg has
  finished. Nothing can play while it is being made, and the slave needs room for the outputs of its running jobs.
- **One file out, described narrowly.** One video stream, one audio stream, one container file. There are no subtitles
  or chapters, no segmented output such as HLS, no filters beyond scaling and tone mapping, and no ffmpeg options from
  the master. That narrowness is what lets a slave treat a source as untrusted content.

## Overview

- **No shared storage and no queue.** The slave reads the source from the master as ffmpeg asks for it, and returns the
  output over the same connection.
- **One outbound connection.** The master dials the slave. So the master may sit where nothing can reach it: in a
  container, behind NAT.
- **Only the bytes needed.** ffmpeg seeks, and the slave fetches just the ranges ffmpeg reads. A job for the first
  seconds of a long recording does not transfer the recording. Every read crosses the network, though. The slave reads
  ahead to hide the connection's latency, but a slow connection between master and slave makes a slow job.
- **The master says what to make, the slave how.** The master names a container and codecs. The slave picks the encoder
  its machine has for each. The same job runs on any slave whose ffmpeg encodes those codecs, and a master has no way
  to pass ffmpeg an argument.
- **The same interface on one machine.** `Master` and `Slave` both do jobs, as a `Transcoder`. A program written against
  that interface transcodes on another machine or on its own, by configuration alone.

The library knows nothing of what is transcoded or why. A job is a source (an `io.ReaderAt` and its size), a
description of the output wanted, and a writer for the result.

## Packages

Each side is a package to embed in a program of your own. That program is the side's host. The slave also comes as a
ready command.

| Package | What |
|---|---|
| `transcode` | What both sides share: a `Job` and what to make of it (`Output`), the `Transcoder` interface, the protocol's messages, and `AllowFrom` |
| `transcode/master` | `Master`, a `Transcoder` doing its jobs on a remote slave |
| `transcode/slave` | `Slave`, a `Transcoder` doing its jobs with ffmpeg on this machine, and the `http.Handler` doing them for masters |
| `transcode/cmd/transcode-slave` | A dedicated slave service |

## A job

```go
job := transcode.Job{
    Label:  "holiday.mov", // names the job in the slave's log
    Source: file,          // an io.ReaderAt: an *os.File, a blob store's object, …
    Size:   size,
    Output: transcode.Output{
        Format: "mp4",
        Video:  &transcode.Video{Codec: "hevc", MaxShortSide: 720, Bitrate: 2_000_000, PixelFormat: "yuv420p"},
        Audio:  &transcode.Audio{Codec: "aac", Bitrate: 128_000},
    },
    Progress: func(p transcode.Progress) { log.Printf("%+v", p) },
}

var tr transcode.Transcoder = master.Master{URL: "ws://192.0.2.1:8486/transcode"}
n, err := tr.Transcode(ctx, job, out) // out: an io.Writer, receiving the n bytes of the output
```

`Progress` reports three things: the job's place in the slave's queue (`Queued`), that the job has its slot
(`Running`), and how far into the source the output has come (`Seconds`). `Seconds` is reported every couple of
seconds.

A job that cannot be done fails with a `*transcode.Failure`: the output is invalid, the slave's ffmpeg does not encode
a codec, or ffmpeg cannot read or encode the source. Its `Message` says what failed. Its `Detail` is the end of
ffmpeg's standard error, where there is one. Any other error is the transport's or the caller's: the source's, the
writer's, the context's or the connection's. Cancelling the context ends the job at once, on the slave too. It does so
even while the master is waiting on a read of the source.

## Output

An `Output` says what to make, never how. The master names the container and the codecs: any that ffmpeg has, by
ffmpeg's names. The slave chooses the encoder that makes each codec on its machine. One source goes in and one output
comes out: video with or without audio, or audio alone.

| Field | What |
|---|---|
| `Format` | The container, as ffmpeg names its muxer: `mp4`, `matroska`, `webm`, `ipod` (M4A), `mp3`, `ogg`, `flac`, … |
| `Video` | `nil` for no video. `Codec` (`h264`, `hevc`, `vp9`, `av1`, `prores`, …); `MaxShortSide`, an even number bounding the short side, 0 for the source's size; `Bitrate` in bits per second, 0 for the encoder's default rate control; `PixelFormat` (`yuv420p`, …), empty for the encoder's default; `ToneMap`, true for an HDR source to be made SDR |
| `Audio` | `nil` for no audio. `Codec` (`aac`, `opus`, `mp3`, `flac`, …); `Bitrate` in bits per second, 0 for the encoder's default |
| `MaxSeconds` | 0 for the whole source; else the output is of its first `MaxSeconds` seconds |

Every slave makes an output the same way:

- **Format:** the muxer writes the output as one file. The MOV family (`mp4`, `mov`, `ipod`, `3gp`, `3g2`, `psp`,
  `f4v`) is written with its index first (`-movflags +faststart`), so that a player starts before the whole file has
  loaded. HEVC in that family is tagged `hvc1` (`-tag:v hvc1`), because Safari, iOS and QuickTime play no HEVC tagged
  `hev1`, which is ffmpeg's default. A format that writes more than one file, such as a segmenting muxer, fails the
  job. So does an output device: a format that plays or shows the output rather than writing a file.
- **Streams:** the source's first video stream, where `Video` is set, and its first audio stream, where `Audio` is.
  The source must have the output's main stream: video, or audio for an audio-only output. A source without it fails
  the job. Audio beside video is taken where the source has it, so a silent video makes a silent output. Nothing else
  is carried: no subtitles, data streams, chapters, or cover art.
- **Length:** the whole source, or its first `MaxSeconds` seconds.
- **Orientation:** as the source is shown. The rotation is in the pixels, and the output carries no rotation of its
  own.
- **Size:** both sides are even, whatever the source's, because 4:2:0 encoders require it. The short side is bounded by
  `MaxShortSide` and never enlarged. The long side keeps the aspect ratio, to the nearest even number. With
  `MaxShortSide` 0, each side is rounded down to even.
- **Dynamic range:** the source's, with its colour tags carried over. An HDR source makes an HDR output, and a player
  that ignores the tags shows it with muted colour and dull highlights. With `ToneMap`, an HDR source (HLG or PQ) is
  made SDR: BT.709, with the highlights compressed (`zscale` and `tonemap`, mobius), in `yuv420p` unless `PixelFormat`
  says otherwise. That needs a slave whose ffmpeg has `zscale` (zimg). Only the master knows whether its source is HDR:
  ffprobe reports a `color_transfer` of `arib-std-b67` (HLG) or `smpte2084` (PQ). Tone mapping an SDR source darkens
  it, and tone mapping a source that carries no colour tags fails the job. `slave.ToneMapping(pixelFormat)` is the
  filter chain. A host uses it in its own ffmpeg commands — for a still of an HDR source, say — to show the source as
  a slave's output does.
- **Codecs:** each is encoded by the slave's encoder for it. Where a `Bitrate` is given, the encoder runs at it in its
  average-bitrate mode. Where a `PixelFormat` is given, the output is in it. If the encoder lacks that pixel format,
  the output is in the nearest one it has, as ffmpeg picks.

`Output.Validate` refuses:

- an output with neither video nor audio;
- a format, codec or pixel format that is not a plain name: lowercase letters, digits and underscores, so a name and
  never an option;
- an odd or negative `MaxShortSide`;
- a negative bitrate;
- a negative `MaxSeconds`.

The refusal is a `*Failure` wherever it happens: the master checks before it dials, and the slave checks again. A slave
also refuses a codec its ffmpeg does not encode. An encoder's name (`libx264`, `h264_videotoolbox`) is not a codec, so
a master names what to make and never the encoder that makes it. Whether ffmpeg has the muxer is for ffmpeg to say: a
job it refuses is a `*Failure`, with the end of ffmpeg's standard error as its `Detail`.

For an output that every browser plays, a master asks for `mp4` with `h264` in `yuv420p` and `aac`, and tone-maps an
HDR source.

## The slave

```go
s, err := slave.New(slave.Config{
    FFmpeg: "/usr/bin/ffmpeg",
    Spool:  "/var/tmp/transcode",
    Jobs:   1,
    Encoders: map[string]slave.Encoder{
        "hevc": {Name: "hevc_videotoolbox", Args: []string{"-g", "250", "-bf", "3"}},
    },
})
defer s.Close()

http.Handle("GET /transcode", transcode.AllowFrom(masters, s)) // masters: []netip.Prefix
```

| Field | What |
|---|---|
| `FFmpeg` | The ffmpeg binary |
| `Spool` | Where the slave makes a directory of its own, holding outputs while their jobs run |
| `Jobs` | Jobs run at once, at least 1; the others wait their turn, in arrival order |
| `HWAccel` | ffmpeg's `-hwaccel` for decoding a source with video, e.g. `videotoolbox`; a job that fails under it is decoded again in software; empty for none |
| `Encoders` | Codec → the encoder making it on this machine (`Name`, and its own options besides the bitrate in `Args`, e.g. `["-profile:v", "high"]`), where it is not ffmpeg's default encoder for the codec |
| `Logger` | Where jobs are logged, a line each as they end; `slog.Default()` when nil |

`New` refuses a configuration that its ffmpeg cannot carry out: an ffmpeg that does not run, an `HWAccel` it does not
have, an `Encoders` key that is not a codec it encodes, or an encoder it does not have for that codec. `New` makes the
slave's directory in `Spool` (`transcode-*`). The slave touches nothing else there, so several slaves may share one
`Spool`. A slave that dies leaves its directory behind, for its operator to clean up.

A codec with a configured encoder is always made by that encoder. An encoder meant for hardware should be configured
to fail a job it cannot encode in hardware, rather than encode it in software. That is VideoToolbox's default
(`-allow_sw 0`). A slave then never makes such a codec slowly without saying so. Any other codec is made by ffmpeg's
default encoder for it.

When an encoder sets no defaults of its own, ffmpeg leaves it at ffmpeg's generic ones: a key frame every 12 frames
and no B-frames. VideoToolbox's encoders are such encoders. At the same bitrate their output then looks clearly worse
than libx264's or libx265's: smeared texture, and VMAF up to 12–14 points lower at 720p. Given libx264's GOP,
`-g 250 -bf 3`, they come within a few points of it, in a fraction of the time. So configure `h264_videotoolbox` with
`-profile:v high -g 250 -bf 3`, and `hevc_videotoolbox` with `-g 250 -bf 3`. A job for such a codec should give a
`Bitrate`. Without one, VideoToolbox's own rate control spends well above libx264's default for much the same quality.

On a Mac, `HWAccel: videotoolbox` decodes in hardware what VideoToolbox decodes: H.264 and HEVC, 10-bit too, among
others. Anything else is decoded in software (MPEG-4 Part 2, VP8, FFV1, …). Either way the frames are rotated and
scaled as described under Output.

A hardware decoder gives up on damage that a software decoder conceals. After a broken frame it loses every frame up
to the next key frame. In a long GOP that is many frames: a surveillance recording, say, may have a key frame only
every few minutes. ffmpeg fails the job once most of the frames are lost. So when ffmpeg fails a job while decoding in
hardware, the slave runs the job again, decoding in software. The job fails only if that fails too. The slave logs a
warning naming the job when it runs one again. The output is the same either way.

`Slave` is the `http.Handler` that serves the protocol to masters. It is also a `Transcoder`, which does a job in its
own process (`Transcode`). The two share one queue. `Health` answers ffmpeg's version, the configured encoders, and
the numbers of jobs running and waiting, for the host to serve as it likes. `Close` ends the jobs, running and
waiting, and closes each master's connection with 1001, going away. It refuses any more jobs, and deletes the slave's
directory.

Each job's log line has the master's address (or `local`), the label, the source's size, the bytes read of it, the
time taken, the output's size, and the outcome: `ok`, `failed: …` or `cancelled`.

### `AllowFrom`

`AllowFrom(prefixes, next)` serves `next` to requests from addresses within the prefixes, and answers `403` to any
other. The address is the connection's remote address, never a forwarded header. `Master` sends no credentials, so
`AllowFrom` admits a master by its address alone. A host with another way of admitting its masters uses that in place
of `AllowFrom`.

### What a slave guards against

A source's bytes are untrusted content, and a master names only what to make:

- **The command is built from the `Output` alone.** A master contributes names: a muxer's, codecs', a pixel format's.
  It never contributes arguments, paths or URLs. The slave names the output's path, and takes no muxer options from
  the master.
- **The source is ffmpeg's only input.** ffmpeg reads it over HTTP, from a server that the job runs on a loopback port
  of its own. The path holds a random token of the job's (128 bits), so that no other process on the machine reads the
  source by guessing. ffmpeg's input protocols are confined to what that URL needs (`-protocol_whitelist http,tcp`).
  Its input formats are confined to plain media containers and streams (`-format_whitelist`). A playlist format such
  as HLS or concat would open the URLs and paths written in it, so it is refused. The sources a slave reads are in one
  of these formats, as ffmpeg names its demuxers: `mov` (MP4, MOV, M4A, 3GP), `matroska` (MKV, WebM), `avi`, `flv`,
  `mpegts`, `mpeg`, `ogg`, `asf` (WMV, WMA), `wav`, `aiff`, `caf`, `mp3`, `flac`, `aac`, `ac3`, `eac3`, `amr`, `wv`.
  Any other — an MXF, a GIF, a raw H.264 stream — fails the job, with ffmpeg's "Format not on whitelist" as its
  `Detail`.
- **The output is one file in the job's directory.** It is written there under the file protocol alone
  (`-protocol_whitelist file`). A format that writes anything else there fails the job. An output device is refused
  before ffmpeg starts.

## The standalone slave

```sh
go install greendrake.nz/go/transcode/cmd/transcode-slave@latest
transcode-slave config.yaml
```

```yaml
Listen: 192.0.2.1:8486
Path: /transcode                  # the WebSocket endpoint; GET <Path>/health answers the slave's health as JSON
Allow: [192.0.2.0/24]             # the masters' addresses
FFmpeg: /opt/homebrew/bin/ffmpeg
Spool: /var/tmp/transcode-slave
Jobs: 1
HWAccel: videotoolbox             # optional
Encoders:                         # optional: codecs made by the hardware; any other by ffmpeg's default encoder
  h264: {Name: h264_videotoolbox, Args: [-profile:v, high, -g, 250, -bf, 3]}
  hevc: {Name: hevc_videotoolbox, Args: [-g, 250, -bf, 3]}
```

An unknown key, or a required one missing or empty, fails the start. The health route answers
`{"ffmpeg": "<version line>", "encoders": {"hevc": {"name": "hevc_videotoolbox", "args": ["-g", "250", "-bf", "3"]}}, "running": 0, "waiting": 0}`.
Both routes refuse addresses outside `Allow`. The command serves plain `ws://`. It logs a line as it starts and one
per job. It stops on SIGTERM or SIGINT: it shuts the server down, then ends the running jobs.

## Protocol

This section is for writing a master or a slave in another language. A connection carries one job. It is a WebSocket
(RFC 6455) from the master to the slave, with the subprotocol `transcode.v1`. The job lasts exactly as long as the
connection: either side closing it, or the connection dropping, ends the job.

- **Text messages** are one JSON object each, and its `type` says which message it is. An unknown type is a protocol
  error. So is a field the type does not have, at any depth. So a peer never accepts a message of another version of
  the protocol as one it knows while ignoring the parts it does not know.
- **Binary messages** are bytes: from the master, the answers to the slave's reads; from the slave, the output.
  Direction alone tells them apart.
- The connection is closed with `1002` and a reason after a message out of place, a malformed one, or an answer of the
  wrong length. It is closed with `1011` after a failure inside either side that no message covers. A slave that is
  closing closes it with `1001`.

Master → slave:

| Message | When | Fields |
|---|---|---|
| `start` | First, once | `label`; `size`, the source's length; `output`: `format`, `video` (`codec`, `max_short_side`, `bitrate`, `pixel_format`, `tone_map`) or `null`, `audio` (`codec`, `bitrate`) or `null`, `max_seconds` |
| binary | Answering each `read`, in the order the reads came | The read's `id` as an 8-byte big-endian unsigned integer, then exactly `length` bytes of the source from `offset` |
| `abort` | The source cannot be read | `error`. The master closes after sending it |

Slave → master:

| Message | When | Fields |
|---|---|---|
| `queued` | The job waits for a slot; again as its place changes | `ahead`, the jobs before it |
| `running` | The job has its slot; reads follow | — |
| `read` | ffmpeg needs source bytes | `id`, unique within the job; `offset`; `length`, at most 1 MiB, never past `size` |
| `progress` | Every couple of seconds while ffmpeg runs | `seconds`, how far into the source the output has come |
| `result` | ffmpeg has made the output, which follows | `size` |
| binary | After `result` | The output, in order, chunks of at most 1 MiB adding up to `size` |
| `failed` | The job failed | `error`; `detail`, the end of ffmpeg's standard error, where there is one |

A job ends with `failed`, or with `result` and all of the output. The slave then closes with `1000`. An example:

```
→ {"type":"start","label":"a.mov","size":10485760,"output":{"format":"ipod","video":null,"audio":{"codec":"aac","bitrate":96000},"max_seconds":0}}
← {"type":"running"}
← {"type":"read","id":0,"offset":0,"length":1048576}
→ <00 00 00 00 00 00 00 00, then 1048576 bytes of the source>
  …
← {"type":"progress","seconds":12.5}
← {"type":"result","size":524288}
← <524288 bytes of output>
```

The slave may have several reads outstanding, up to 4 MiB ahead of what ffmpeg has taken. That keeps the source
flowing despite the connection's latency. When ffmpeg seeks — to an MP4's index at its end, say — the reads already
asked for are still answered, and the slave drops their answers by `id`. The master answers reads in order and never
knows about seeks. Flow control is left to WebSocket over TCP: a side that does not read holds the other back.

## Requirements

Go 1.27. The slave's machine needs an ffmpeg with the encoders it is to use: for VideoToolbox, a Mac and an ffmpeg
built with it. The master needs no ffmpeg. The module's tests run the ffmpeg and the ffprobe on `PATH`. That ffmpeg
has to be 6.0 or later, with libx264, libx265 and zimg. On a Mac the tests also check VideoToolbox, configured as
above.
