# @greendrake/ipcam

The browser end of the camera engine, `greendrake.nz/go/ipcamrpc` over
`greendrake.nz/go/ipcam`: its API bound to one camera, the cameras' states, and the
components that show a camera, its state and its settings. `ipcam-app` is a host of one
camera.

```sh
bun add @greendrake/ipcam vue pinia
```

## API

- `Methods` and `Pushes` are the engine's methods and pushes, keyed as the Go
  `ipcamrpc` is: every method names its camera (`On`), and the cameras' states are the
  state stream `cameras` (`@greendrake/rpc`'s `StateFrame`).
- `cameraApi(client, name)` is a `CameraApi`, a `@greendrake/rpc` `Caller` of the
  `CameraMethods` bound to the camera `name` names: each method takes its arguments less
  `Camera`, which is added on the way to `client`, any `Caller<Methods>`.
  `settings.groups`, `camera.time` and `camera.reboot` take none.
- `provideCameraApi(api)` gives the components below a host's component the API of the
  camera they show. The components show a call's progress and its failure where the call
  is made, so the client is one that shows neither itself — an `ApiClient` of
  `@greendrake/vue-api`, say, as its `with({ silent: true })`.
- `useCameras(transport)` is every camera's state, by name, as a reactive map. It holds
  the document the engine greets each socket with, and each patch is merged in
  (`@greendrake/vue-api`'s `keepDocument`), for the page's life: call it before the
  transport connects. A camera's `Streams` is absent before its first login.
- The wire types are exported as the Go types are keyed: `CameraState`, `Cameras`,
  `StreamKey`, `StreamState`, `Codec`, `Setting` (a `Kind` described), `Option`, `Group`,
  `SetResult`, `Clock`, and `Problem`, the details of a failure the engine words itself —
  shown in its words, any other failure by its message.
- `ENCODE_GROUP` and `MAINTENANCE_GROUP` are the IDs of the two groups of settings the
  components name: the one a stream's codec is set in, which the banner offers, and the
  one shown with the camera's clock and its reboot beside its settings.

## Components

- `CameraTile`, props `hubUrl` (the stream hub's WebSocket URL), `name`, `state` and
  `contain`: the camera's picture, played by `@greendrake/mse-video`'s `Video`, which a
  click has fill the screen, and a menu button over it. The host sizes the tile, which
  has no size of its own, and the picture fills it, stretched, or with `contain`
  letterboxed on black, as it is while filling the screen. The menu has a Sound switch,
  a choice of stream (SD, the extra one, which the tile opens on, and HD, the main one)
  where more than one can be watched, and, where the state says the camera has
  settings, Settings, which goes while the tile fills the screen: a modal, titled with
  the name, of the `StateBanner` over the `CameraSettings`, whose group is kept while
  it is closed. A stream can be watched while the engine casts it, as at the camera's
  last login, and each can before the first; with none to watch there is no picture.
- `CameraSettings`, props `state` and `group` (`v-model:group`): a tab per group of the
  camera's settings, read once the state is online, each group's read once its tab first
  opens and kept while another shows. What is being read, the settings or a group's
  section, shows nothing but the spinner meanwhile. A group the camera does not have, or none, gives
  way to the one shown last, or the first, through `update:group` with `picked` false: a
  host keeping the group in history replaces rather than adds to it.
- `StateBanner`, prop `state`: how the camera is (online, offline with why, waiting for
  a restart a setting needs, with Reboot, or encoding a stream in another codec than the
  engine casts); `openGroup` asks for the group of settings that switches it back,
  `ENCODE_GROUP`.

`CameraTile`, `CameraSettings` and `StateBanner` take the camera's API as provided.

## Styling

The components read these CSS custom properties, which the host defines (a host on
`@greendrake/theme` has them from it and its dashboard palette), either directly or
through the `@greendrake/ui` controls they are built of (`TabPanels`, `Checkbox`,
`Field`, `InfoTip`, `SelectBox`, `TextField`, `MenuButton`, `SwitchRow`,
`SegmentedControl`):

| Property | Read by |
| --- | --- |
| `--space-1` … `--space-4` | spacing throughout |
| `--error-color` | a failure's words; the banner while offline or on another codec |
| `--warning-color` | the banner while a restart is pending |
| `--border-color-light` | the banner's and the tab strip's rules; the tile's menu's border and its choice of stream's |
| `--accent-color` | the active tab, a checked switch, the stream picked |
| `--accent-contrast` | the stream picked's label, inherited where unset |
| `--form-background` | the tile's menu |
| `--form-background-hover` | a tab hovered; a menu item hovered |
| `--font-color` | a switch's label; the menu's items |
| `--border-color`, `--border-radius`, `--input-background` | a switch's box; a tip's; the menu's corners (`--border-radius`) |
| `--border-radius-pill` | the Sound switch's track |
| `--font-xs`, `--font-md` | a tip's mark; a tip's words |
| `--font-color-muted` | a tip's mark, `--font-color` where unset |
| `--tippy-background-color` | a tip, `#5335e8` where unset |
| `--vs-*` | a choice's select box, as `@greendrake/ui`'s README has them; their defaults are for a light surface |

A tile standing in for fullscreen, where the browser has none to give, stacks at
`@greendrake/scss-kit`'s `sticky` layer: over the page, under a modal.

The settings' modal, `@greendrake/ui`'s `Modal`, reads what its README has it read; the
native controls in it take `@greendrake/theme`'s `form-controls` skin, whatever the
host's own take.

The spinner is the `spinner` class on what is being read, which the host styles
globally: `@greendrake/theme` does, and so does its `spinner` entry alone.
