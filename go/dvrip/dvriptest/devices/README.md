Recorded devices, a directory each. A device's replies are one file per query name,
holding what the reply carries under that name, as `dvrip config sweep <dir>` saves
them; a dvriptest camera answers queries with one device's, and its clock starts from
the device's `OPTimeQuery` where it has one. Addresses, MACs and serial numbers are replaced with
documentation ones (RFC 5737, RFC 7042) and zeros.

- `techage`: a TechAge camera, model IPC_NT98566_N8F, firmware
  V5.00.R02.H72749N8.10010.140300.0020000: the cameras this library is run against.
  Its replies are its factory settings.
- `techage-hi3516ev100`: an older TechAge camera, hardware HI3516EV100_50H20L_S38,
  firmware V4.02.R12.00035520.12012.047500.00200 (2018), in its settings in use. It
  writes hex values unpadded (`"0x0"`, `"0x02"`), switches as numbers where the newer
  one writes `true`/`false`, and has `AVEnc.SmartH264` but not `AVEnc.SmartH264V2`. Its
  replies include `AVEnc.VideoWidget.[0]`, the overlays on its picture.
- `xm530`: an XM530 camera, hardware XM530_85X50_8M, firmware
  V5.00.R02.0003068d.10010.349f00.0020000, in its settings in use. `extra.h265.bin` is
  what `dvrip monitor -stream extra -for 4500ms -record` saved of its extra stream, D1
  (800×448) at 15 frames a second, 256 kbit/s and a key frame every 2 s, with audio in
  20 ms frames. It is the one recording kept, which a dvriptest camera plays for either
  stream whatever its device.
