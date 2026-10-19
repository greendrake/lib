# greendrake.nz/go/dvrip

A client for DVRIP, the protocol of Xiongmai-based cameras and recorders (also known as
"Sofia", NetSurveillance or XMEye; TechAge cameras speak it): logging in, reading and
writing configuration, and pulling live streams with their audio. Pure Go, no
dependencies.

```sh
go get greendrake.nz/go/dvrip
```

The protocol is defined by the vendor's documents: the interface protocol
(雄迈数字视频录像机接口协议_V1.0.0.pdf), the configuration exchange format
(配置交换格式V2.0.pdf) and the stream frame format (码流帧格式文档.pdf). The values of
many settings come from the vendor's SDK: its `netsdk.h` and its configuration demo.

## Wire format

Every message is a 20-byte little-endian header and the data it announces:

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

Control data is JSON ending in `\n\0`. Every request names itself and the session
(`{"Name":"General.Location","SessionID":"0x00000057"}`, a write carrying the value
under its name), and every reply carries a status code, `Ret`, 100 on success.

## Sessions

`Dial` logs in on a control connection, which then carries the keep-alive at the
interval the device sets, and every request, one at a time. A session ends when its
owner closes it, when the device stays silent for three keep-alive intervals, when a
request is given up on (its late reply could not be told from the next one's), or
when the device sends something nobody asked for.

Media runs on connections of its own. A monitor claims a new connection for the
session, waits for the claim to be granted, and then starts the stream on the control
connection; stopping it on the control connection makes the device close the media
connection. That is the vendor's flow, and the one that leaves the login standing
when a stream stops: one session can serve configuration and several streams at once.

## Streams

A monitor's packets carry one continuous byte stream of frames, each starting
`00 00 01` and a type byte, and packet boundaries mean nothing. Frames are cut by the
length their headers state:

| Type | Frame | Header |
|---|---|---|
| `FC` | video key frame | 16 bytes: codec and size bits, frame rate, width/8, height/8, date and time, length |
| `FD` | video frame | 8 bytes: length |
| `FA` | audio | 8 bytes: codec, sample rate, 16-bit length |
| `FE` | picture | as a key frame |
| `F9` | info | 8 bytes: type, 16-bit length |

Video is H.265 in Annex B, parameter sets in band in key frames; a stream in any other
codec, H.264 included, is refused as unknown. Audio is
G.711, usually A-law at 8 kHz in 40 ms frames. `Monitor.Next` delivers video and audio
from the first key frame on, stamped on one clock starting at 0: the device stamps
nothing finer than a second, so video advances one frame period per frame and audio by
its sample count. Cameras send audio in batches of three or four frames, but its clock
does not drift from the video's; audio is pulled back onto the video's clock only when
it wanders more than 250 ms outside the video frame it arrives with, which takes lost
audio. `Monitor.Record` saves the stream's frames as they came, and `NewReplay` plays
such a recording back as `Monitor.Next` delivered it.

## Configuration

`Session.Get` and `Session.Set` move any configuration as JSON by name: a group
(`General`), one configuration (`General.Location`), or one channel's
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
`Config.Set` therefore reads the device's configuration and lays the value over it, so
that fields the type leaves out are written back as the device had them. Some switches
are `true`/`false` in one configuration and `1`/`0` in another; `Flag` reads either,
and `Config.Set` writes it back in the device's form.

`SimplifyEncode` is kept per channel but read and written whole: devices refuse
`Simplify.Encode.[0]`. Changing a stream's codec takes a restart (`Set` reports it).
`StreamCapability.Resolutions` names the sizes `EncodeCapability` says a stream can be
encoded at, in the strings `SimplifyEncode` takes. The device's `AVEnc.EncodeStaticParam`
(the CMS's "Static configuration of" profile and level) is H.264's only: an H.265
stream keeps its Main profile whatever it holds, so the library leaves it out.

Enumerations the device writes as hex strings are each a `HexOf` type of their own,
with constants: `DayNightMode`, `BacklightMode`, `WhiteBalanceMode`, `SlowShutter`.

`VideoWidget` has the overlays on the picture, the channel title and the time title:
whether each is drawn, and its left and top on 0–8192 across and down. It keeps the
title's text, but the device draws no text of its own for it: what shows is the 1-bit
`Bitmap` a client renders and sends with `Session.DrawTitle`, which the device keeps
across a reboot and scales with each stream. The time title's text is the device's, in
`Location`'s formats.

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

`config sweep` with a directory saves the replies the tests answer with; `config watch`
prints every value that changes while a settings page is used, which is how a field no
document describes is found.

## A camera to test against

`dvriptest` is a camera in-process: `dvriptest.Start(device, report, Options{})` serves
a recorded device's replies (`dvriptest/devices`, see its README) on a loopback port,
keeps what is written to it, runs its own clock, reboots (unreachable for
`RebootPause`), and streams a recorded stream, all at once or, with `Realtime`,
at their frame rate over and over. Main and extra monitors of one session run side by
side, as on the cameras. What a client sends that the protocol does not allow goes to
`report`; tests pass `t.Errorf`. `Options` also make it refuse, ignore, chunk or
replace what it answers, or restart on a write, for the failure paths. `Overlay` changes a configuration
while it runs, laying a partial one over it as `Config.Set` does, and `Hold` leaves a
command unanswered until `Release`. `Title` is the title bitmap it was sent to draw
last. `Replies` and `Recording` give tests the recorded replies and stream themselves.

`cmd/fakecam` runs one on its own, for developing and testing a client end to end
without the hardware:

```
go install greendrake.nz/go/dvrip/cmd/fakecam@latest
fakecam -device techage -listen 127.0.0.1:34567 -reboot 3s -restart Simplify.Encode -realtime -control 127.0.0.1:34580
```

`-restart` names the configurations whose writes it answers as the cameras answer a
codec change: taken, but applied only once restarted. `-control` serves an HTTP
endpoint for a test to change the camera through and ask it what it was sent:
`PATCH /configs/{name}` overlays its body on that configuration,
`GET /commands/{name}` answers how many commands it has received under the name,
`PUT /holds/{name}` leaves those commands unanswered until `DELETE /holds/{name}`, and
`GET /title` answers the width and height of the title bitmap it was sent last.
