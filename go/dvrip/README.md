# greendrake.nz/go/dvrip

The settings and the live streams of Xiongmai-based cameras and recorders, for a Go
program: a client for DVRIP, the devices' own protocol (also known as "Sofia",
NetSurveillance or XMEye; TechAge cameras speak it). It logs in, reads and writes
configuration, and pulls live streams with their audio. Pure Go, no dependencies.

```sh
go get greendrake.nz/go/dvrip
```

## Why

Cameras and recorders built on Xiongmai's boards are sold under many brand names. Their
settings, and their streams with audio, are reached in full only over the vendor's own
protocol, DVRIP. The vendor's own software does not suit a Go program: its apps are
made for a person at a screen, and its SDK is a native library, which a Go program can
call only through cgo. RTSP and ONVIF, where a device serves them, give the streams but
not the vendor's own settings.

This package implements the protocol in pure Go. The setting values that the vendor's
documents leave out are declared as types.

## Trade-offs

- **One vendor's protocol.** It works with devices that speak DVRIP, and no others. If
  the picture is all a program needs, RTSP or ONVIF is the better choice where a device
  serves them: they work with other makes of camera too.
- **No H.264, and no audio but G.711.** `Monitor` refuses a stream in H.264 as an
  unknown codec, and fails on audio in any codec but G.711. A device set to H.264 has
  to be switched to H.265 first. The switch is a write to `SimplifyEncode`, and the
  device applies it only after a restart. A device that encodes only H.264 cannot be
  watched with this package.
- **Live streams and settings, and nothing else.** There is no playback or download of
  recordings, no PTZ, no two-way talk, no alarms, no user management and no firmware
  upgrade. A message that the device sends without being asked ends the session, so a
  program cannot listen for such messages.
- **Run against a few cameras.** The library is run against TechAge cameras. Its tests
  use the recorded replies of three single-channel cameras: two of TechAge's and an
  XM530.

## Overview

A device serves DVRIP on TCP port 34567. The vendor's documents define the protocol, but
not the values of many settings: those are in the SDK's header, `netsdk.h`, and its
demo, and some fields are described nowhere.

- **One login for everything.** A session serves configuration and several streams at
  once. Each stream runs on a connection of its own, so stopping one stream ends neither
  the login nor the other streams.
- **Settings as types.** The configurations behind a camera's settings pages have Go
  types, with constants for the values of their choices. Any other configuration is
  read and written as JSON, by name.
- **Writes that keep the fields a type leaves out.** A typed write is merged into the
  device's current configuration. The fields a type leaves out stay as the device had
  them, and a switch or a hex value is written in the form the device uses for it.
- **Frames on one clock.** A stream arrives as whole frames, video and audio, each with
  a timestamp on one clock that starts at the first key frame.
- **A camera to test against.** `dvriptest` is a fake camera. It answers as a recorded
  device does and plays a recorded stream, so a client's tests need no hardware.

```go
s, err := dvrip.Dial(ctx, "192.0.2.10", "admin", "secret")
if err != nil {
	return err
}
defer s.Close()

// A setting, by its type.
location, err := dvrip.Location.Get(ctx, s)
if err != nil {
	return err
}
location.DateFormat = dvrip.DateDDMMYY
if _, err := dvrip.Location.Set(ctx, s, location); err != nil {
	return err
}

// The main stream of channel 0, frame by frame.
m, err := s.Monitor(ctx, 0, dvrip.Main)
if err != nil {
	return err
}
for {
	frame, err := m.Next()
	if err != nil {
		return err
	}
	handle(frame) // frame.Kind is dvrip.Video or dvrip.Audio, frame.PTS its time
}
```

## Wire format

The protocol is defined by the vendor's documents: the interface protocol
(雄迈数字视频录像机接口协议_V1.0.0.pdf), the configuration exchange format
(配置交换格式V2.0.pdf) and the stream frame format (码流帧格式文档.pdf). The values of
many settings come from the vendor's SDK: its `netsdk.h` and its configuration demo.

Every message is a 20-byte little-endian header followed by its data:

| Offset | Size | Field |
|---|---|---|
| 0 | 1 | head flag, `0xFF` |
| 1 | 1 | version: 0 is sent, devices take 0 or 1 |
| 2 | 2 | reserved |
| 4 | 4 | session ID, 0 until logged in |
| 8 | 4 | sequence number, per connection |
| 12 | 1 | control: packets in the message; media: channel |
| 13 | 1 | control: this packet's number from 0; media: end of data flag |
| 14 | 2 | message ID |
| 16 | 4 | data length |

Control data is JSON ending in `\n\0`. Every request carries its name and the session's
ID: `{"Name":"General.Location","SessionID":"0x00000057"}`. A write also carries the
value, under its name. Every reply carries a status code, `Ret`, which is 100 on
success.

## Sessions

`Dial` logs in on a control connection. That connection then carries the keep-alive, at
the interval the device sets, and every request, one at a time. A session ends in one
of four ways:

- its owner closes it;
- the device stays silent for three keep-alive intervals;
- a request is abandoned, because its late reply could not be told apart from the reply
  to the next request;
- the device sends a message that no request asked for.

Media runs on connections of its own. A monitor is one live stream. It claims a new
connection for the session and waits for the claim to be granted. It then starts the
stream on the control connection. Stopping the stream, also on the control connection,
makes the device close the media connection. That is the vendor's sequence, and it is
the one that keeps the login when a stream stops. So one session can serve
configuration and several streams at once.

## Streams

A monitor's packets carry one continuous byte stream of frames. Each frame starts with
`00 00 01` and a type byte. Packet boundaries carry no meaning: a frame ends where the
length in its header says.

| Type | Frame | Header |
|---|---|---|
| `FC` | video key frame | 16 bytes: codec and size bits, frame rate, width/8, height/8, date and time, length |
| `FD` | video frame | 8 bytes: length |
| `FA` | audio | 8 bytes: codec, sample rate, 16-bit length |
| `FE` | picture | as a key frame |
| `F9` | info | 8 bytes: type, 16-bit length |

Video is H.265 in Annex B, with the parameter sets in band in key frames. The format's
code for MPEG-4 is read too, and such a stream's frames arrive as `MPEG4`. A stream in
H.264, or under any other code, is refused as unknown. Audio is G.711, usually A-law
at 8 kHz in 40 ms frames.

`Monitor.Next` delivers video and audio from the first key frame on, with timestamps on
one clock that starts at 0. The device's own timestamps are no finer than a second. So
video advances one frame period per frame, and audio advances by its sample count.
Cameras send audio in batches of three or four frames, but the audio's clock does not
drift from the video's. Audio is set back to the video's clock only when its timestamp
is more than 250 ms outside the video frame it arrives with. Only lost audio moves it
that far.

`Monitor.Record` saves the stream's frames as they came. `NewReplay` plays such a
recording back as `Monitor.Next` delivered it.

## Configuration

`Session.Get` and `Session.Set` read and write any configuration as JSON, by name: a
group (`General`), one configuration (`General.Location`), or one channel's
(`Camera.Param.[0]`). `Session.Ability` reads what the device supports.

`Config`, `ChannelConfig` and `AbilityOf` bind names to types for the settings a
camera's own management software edits:

| Settings page | Configurations |
|---|---|
| General | `General`, `Location`, `TimeZone`; `Session.Time`, `Session.SetTime` |
| Encode | `SimplifyEncode`, `SmartH264`, `SmartH264V2`; `EncodeCapability` |
| Network | `NetCommon`, `IPAdaptive`, `NetDHCP`, `NetDNS`, `OnvifPwdCheckout`, `Correspondent` |
| NetService, NTP | `NetNTP` |
| Camera param. | `CameraParam`, `CameraParamEx`, `ClearFog` |
| DISPLAY | `VideoWidget`; `Session.DrawTitle` |
| AutoMaintain | `AutoMaintain`, `OnlineUpgrade`; `Session.Reboot` |

The types declare the fields those settings need, not everything a firmware keeps.
`Config.Set` therefore reads the device's configuration and merges the value into it,
so that fields the type leaves out are written back as the device had them. Some
switches are `true`/`false` in one configuration and `1`/`0` in another. `Flag` reads
either, and `Config.Set` writes it back in the device's form.

`SimplifyEncode` is kept per channel but read and written whole: devices refuse
`Simplify.Encode.[0]`. A change of a stream's codec applies only after a restart, which
`Set` reports. `EncodeCapability` says which sizes a stream can be encoded at.
`StreamCapability.Resolutions` names those sizes, in the strings `SimplifyEncode`
takes.

The device also keeps `AVEnc.EncodeStaticParam`: the profile and level that the CMS,
the vendor's desktop program, shows as "Static configuration of". It applies to H.264
only. An H.265 stream keeps its Main profile whatever that configuration holds, so the
library leaves it out.

The device writes some enumerations as hex strings. Each of those is a `HexOf` type of
its own, with constants: `DayNightMode`, `BacklightMode`, `WhiteBalanceMode`,
`SlowShutter`.

`VideoWidget` has the two overlays on the picture: the channel title and the time
title. For each it says whether the overlay is drawn, and where: its left and top, each
on a scale of 0–8192. It also keeps the channel title's text, but the device draws no
text of its own for it. What shows is a 1-bit `Bitmap` that a client renders and sends
with `Session.DrawTitle`. The device keeps that bitmap across a reboot and scales it
with each stream. The device draws the time title's text itself, in `Location`'s
formats.

## Command line

`cmd/dvrip` exercises all of it against a device:

```
go install greendrake.nz/go/dvrip/cmd/dvrip@latest
dvrip -addr 192.0.2.10 -pass secret info
dvrip -addr 192.0.2.10 -pass secret config get Camera.Param.[0]
dvrip -addr 192.0.2.10 -pass secret config sweep dvriptest/devices/techage
dvrip -addr 192.0.2.10 -pass secret config watch
dvrip -addr 192.0.2.10 -pass secret monitor -stream main,extra:30s -for 1m -record cam
```

`config sweep` with a directory saves each reply to a file there. The tests' fake
camera answers from such files. `config watch` prints every value that changes while a
settings page is used. That is how a field no document describes is found.

## A camera to test against

`dvriptest` is a fake camera that runs inside the test's process.
`dvriptest.Start(device, report, Options{})` serves a recorded device's replies on a
loopback port. The recorded devices are in `dvriptest/devices`, which has a README of
its own. The camera:

- keeps what is written to it;
- runs its own clock;
- reboots, staying unreachable for `RebootPause`;
- streams a recorded stream: all at once, or with `Realtime` at its frame rate, over
  and over.

Main and extra monitors of one session run side by side, as on the cameras. Whatever a
client sends that the protocol does not allow goes to `report`; tests pass `t.Errorf`.

To test how a client handles failures, `Options` also make the camera refuse or ignore
a command, send a reply in small packets, replace a reply, or restart on a write.
`Overlay` changes a configuration while the camera runs: it merges a partial
configuration into the one kept, as `Config.Set` does. `Hold` leaves a command
unanswered until `Release`. `Title` is the title bitmap the camera was sent to draw
last. `Replies` and `Recording` give tests the recorded replies and the recorded stream
themselves.

`cmd/fakecam` runs one as a program of its own, for developing and testing a client end
to end without the hardware:

```
go install greendrake.nz/go/dvrip/cmd/fakecam@latest
fakecam -device techage -listen 127.0.0.1:34567 -reboot 3s -restart Simplify.Encode -realtime -control 127.0.0.1:34580
```

`-restart` names configurations. The fake camera answers a write to one of them as the
cameras answer a codec change: accepted, but applied only after a restart. `-control`
serves an HTTP endpoint. Through it a test changes the camera and asks it what it was
sent:

- `PATCH /configs/{name}` merges its body into that configuration;
- `GET /commands/{name}` answers how many commands the camera has received under the
  name;
- `PUT /holds/{name}` leaves those commands unanswered until `DELETE /holds/{name}`;
- `GET /title` answers the width and height of the title bitmap it was sent last.
