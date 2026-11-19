# @greendrake/ipcam

The browser side of a camera system: Vue components that show a camera's live picture,
its state and its settings, and the typed API they run on. The app that uses them is the
host. The server is a Go program that serves `greendrake.nz/go/ipcamrpc` over
`greendrake.nz/go/ipcam`.

```sh
bun add @greendrake/ipcam vue pinia
```

The package ships its `src` as it is: Vue single-file components, with TypeScript and
SCSS in them. The host's bundler has to compile all three. `vue` and `pinia` are peer
dependencies.

## Why

The server offers its cameras to a browser: methods that read and change a camera's
settings, a document of every camera's state, and the live streams. An app that shows
those cameras has the same things to build each time: typed calls on a camera, the states
kept up to date, a live picture, and a settings form.

The settings form is the costly one. Cameras differ in the settings they have, and in the
options each setting takes, so a form written by hand for one camera fits no other. The
server describes every setting a camera has, and this package builds the form from that
description. The rest is left to the host: where a camera's picture goes and how big it
is, who may call, and the look.

## Trade-offs

- **One server.** The package speaks the protocol of `greendrake.nz/go/ipcamrpc` and
  nothing else. It is not a general camera interface: it cannot be pointed at a camera,
  or at a server of another kind.
- **H.265 in the browser.** The picture is `@greendrake/mse-video`'s, which plays H.265
  through Media Source Extensions and has no fallback. A browser that cannot do that
  shows a line saying so in place of the picture.
- **A change is written at once.** There is no Save button and no undo. A switch flipped,
  an option picked or a text committed goes to the camera as it is made. That includes a
  change of the camera's address.
- **Text in English, and fixed.** The components' own text (Sound, Stream, Settings,
  Reload, Saved, the banner's sentences) is in the source, and no prop changes it. The
  settings' labels and tips come from the server, in English too.

## Overview

- **Types for the server's messages.** `Methods`, `Pushes` and the data types say what
  the server takes and what it sends, with the keys the Go side uses.
- **Calls on one camera.** Every method names its camera. `cameraApi` binds a client to
  one camera, so a component that shows that camera never handles its name.
- **The cameras' states, kept up to date.** The server sends the states in full as a
  socket opens, and then only what changes. `useCameras` keeps them as one reactive map.
- **A settings form built from a description.** The server describes every setting a
  camera has: its kind, its bounds, its options, a label and a tip. `CameraSettings`
  builds its form from that description, so it has no code for any model of camera.
- **A tile.** `CameraTile` is the live picture with what goes with it: sound, a choice
  between the SD and the HD stream, fullscreen, and the way into the settings.
- **The state in words.** `StateBanner` says what state a camera is in, and offers the
  fix for what is wrong: a reboot when a setting waits for one, the settings to change
  when a stream is in a codec that is not played.

## API

- `Methods` and `Pushes` are the server's methods and pushes, with the keys the Go
  `ipcamrpc` uses. Every method names its camera (`On`). The cameras' states are the
  state stream `cameras`, a `StateFrame` of `@greendrake/rpc`.
- `cameraApi(client, name)` is a `CameraApi`: a `@greendrake/rpc` `Caller` of the
  `CameraMethods`, bound to the camera that `name` names. Each method takes its arguments
  without `Camera`. `cameraApi` adds `Camera` before it calls `client`, which is any
  `Caller<Methods>`. `settings.groups`, `camera.time` and `camera.reboot` take no
  arguments.
- `provideCameraApi(api)` is called in a component of the host. It gives the package's
  components below that component the API of the camera they show. The components show a
  call's progress and its failure where the call is made. So the client has to be one
  that shows neither itself. An `ApiClient` of `@greendrake/vue-api` is such a client, as
  its `with({ silent: true })` returns it.
- `useCameras(transport)` is every camera's state, by name, as a reactive map. It holds
  the document that the server sends each socket as it opens, and each patch is merged
  into it (`@greendrake/vue-api`'s `keepDocument`). It is kept for the page's life, so
  call it before the transport connects. A camera's `Streams` is absent before its first
  login.
- The data types are exported with the keys of the Go types: `CameraState`, `Cameras`,
  `StreamKey`, `StreamState`, `Codec`, `Setting` (a `Kind` described), `Option`, `Group`,
  `SetResult`, `Clock` and `Problem`. `Problem` is the details of a failure whose message
  the server writes. Such a failure is shown with the server's message, and any other
  failure with its own.
- `ENCODE_GROUP` and `MAINTENANCE_GROUP` are the IDs of the two groups of settings that
  the components refer to. `ENCODE_GROUP` is the group a stream's codec is set in, and
  the banner offers it. `MAINTENANCE_GROUP` is the group shown with the camera's clock
  and its reboot beside its settings.

## Components

- `CameraTile` is the camera's picture, with a menu button over it. Its props are `hubUrl`
  (the WebSocket URL the server serves its streams at), `name`, `state` and `contain`.
    - The picture is played by `@greendrake/mse-video`'s `Video`. A click on it makes the
      tile fill the screen.
    - The tile has no size of its own: the host sizes it. The picture fills the tile,
      stretched. With `contain` it is letterboxed on black instead. It is also letterboxed
      while the tile fills the viewport, which is what the click does in a browser that
      has no fullscreen mode.
    - The menu has a Sound switch. Where more than one stream can be watched, it has a
      choice of stream: SD, which is the extra stream and the one the tile opens on, and
      HD, which is the main stream.
    - Where the state says the camera has settings, the menu has Settings. It opens a
      modal, titled with the name: the `StateBanner` over the `CameraSettings`. The
      settings' group is kept while the modal is closed. Settings is not in the menu while
      the tile fills the screen.
    - A stream can be watched while its state's `Cast` is true. That means the camera
      encoded the stream, at its last login, in the codec the server serves. Before the
      first login, each stream can be watched. With no stream to watch, there is no
      picture.
- `CameraSettings` is a tab for each group of the camera's settings. Its props are
  `state` and `group` (`v-model:group`).
    - The groups are read once the state is online. Each group's settings are read once
      its tab first opens, and kept while another tab shows.
    - While the settings are being read, or one group's section is, the part being read
      shows nothing but the spinner.
    - `group` can name a group the camera does not have, or none. The component then shows
      the group it showed last, or the first, and says so through `update:group` with
      `picked` false. A host that keeps the group in the browser's history replaces the
      entry, rather than adding one.
- `StateBanner`, with the prop `state`, says what state the camera is in: online;
  offline, with why; waiting for a restart that a setting needs, with Reboot; or encoding
  a stream in a codec the server does not serve. For the last, `openGroup` asks for the
  group of settings that switches the codec back, `ENCODE_GROUP`.

`CameraTile`, `CameraSettings` and `StateBanner` use the camera's API that
`provideCameraApi` gives them.

## Styling

The components read the CSS custom properties below, which the host defines. A host on
`@greendrake/theme` has them from the theme and its dashboard palette. Some are read by
the components directly, and some through the `@greendrake/ui` controls they are built of
(`TabPanels`, `Checkbox`, `Field`, `InfoTip`, `SelectBox`, `TextField`, `MenuButton`,
`SwitchRow`, `SegmentedControl`).

| Property                                                  | Read by                                                                                              |
| --------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `--space-1` … `--space-4`                                 | spacing throughout                                                                                   |
| `--error-color`                                           | a failure's words; the banner while offline or on another codec                                      |
| `--warning-color`                                         | the banner while a restart is pending                                                                |
| `--border-color-light`                                    | the banner's and the tab strip's rules; the tile's menu's border and its choice of stream's          |
| `--accent-color`                                          | the active tab, a checked switch, the stream picked                                                  |
| `--accent-contrast`                                       | the stream picked's label, inherited where unset                                                     |
| `--form-background`                                       | the tile's menu                                                                                      |
| `--form-background-hover`                                 | a tab hovered; a menu item hovered                                                                   |
| `--font-color`                                            | a switch's label; the menu's items                                                                   |
| `--border-color`, `--border-radius`, `--input-background` | a switch's box; a tip's; the menu's corners (`--border-radius`)                                      |
| `--border-radius-pill`                                    | the Sound switch's track                                                                             |
| `--font-xs`, `--font-md`                                  | a tip's mark; a tip's words                                                                          |
| `--font-color-muted`                                      | a tip's mark, `--font-color` where unset                                                             |
| `--tippy-background-color`                                | a tip, `#5335e8` where unset                                                                         |
| `--vs-*`                                                  | a choice's select box, as `@greendrake/ui`'s README has them; their defaults are for a light surface |

Where the browser has no fullscreen mode, the tile fills the viewport instead. It then
stacks at `@greendrake/scss-kit`'s `sticky` layer: over the page, under a modal.

The settings' modal is `@greendrake/ui`'s `Modal`, and it reads what that package's README
has it read. The native controls in the modal take `@greendrake/theme`'s `form-controls`
skin, whatever skin the host's own controls take.

The spinner is the `spinner` class, set on the part being read. The host styles that
class globally. `@greendrake/theme` does, and so does its `spinner` entry alone.
