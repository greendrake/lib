# @greendrake/theme

Design tokens, document-level element styling (links, lists, quotes, body defaults), the global spinner rules, the `light-dark` theming mixin, and an opt-in default dashboard palette.

## Usage

```scss
// Base: css-reset + tokens + document styling (side-effectful)
@use '@greendrake/theme' as *;

// Opt-in: the default dashboard light/dark palette
@use '@greendrake/theme/dashboard-palette';

// App-specific palette instead:
@include light-dark(
    (
        --font-color: #222
        // ...
    ),
    (
        --font-color: #eee
        // ...
    )
);
```

Mixins exported by the root entry: `light-dark($light, $dark)`, `button-colors`, `overlay-icon-button($size)`, `wider-than-max-content` (plus `$max-content-width`). Framework-free SCSS primitives (breakpoints, z-layers, icon masks, font-face generation) live below this package in `@greendrake/scss-kit`.

## CSS custom property contract

`@greendrake/ui` components render correctly only when the custom properties below are defined. This package's two entries cover part of the contract; the rest is the consuming app's responsibility (typically set in its palette maps or `:root`).

### Defined by `@greendrake/theme` (root entry)

| Property | Value | Notes |
| --- | --- | --- |
| `--space-1` … `--space-6` | 4/8/12/16/24/28px | spacing scale; `--space-1`…`--space-4` are read by ui components |
| `--font-xs` … `--font-2xl` | 11/13/16/20/24/32px | font-size scale; `--font-xs` and `--font-md` are read by ui components |
| `--overlay-button-bg`, `--overlay-button-bg-hover` | `rgb(0 0 0 / 60%)`, `rgb(0 0 0 / 85%)` | consumed by the `overlay-icon-button` mixin |
| `--error-color` | `#ff4800` | semantic status color for app styling (not read by ui components) |
| `--warning-color` | `#890095` | semantic status color for app styling (not read by ui components) |
| `--accent-color` | `#0aa13e` | read by ui components (checkboxes, highlights) |

### Defined by `@greendrake/theme/dashboard-palette` (opt-in, light and dark values)

| Property | Light | Dark |
| --- | --- | --- |
| `--font-color` | `#222` | `#eee` |
| `--background-color` | `#ddd` | `#222` |
| `--form-background` | `#f5f5f5` | `#222` |
| `--form-background-hover` | `#fff` | `#000` |
| `--border-color` | `#555` | `#aaa` |
| `--border-color-light` | `#999` | `#666` |
| `--border-color-hover` | `#000` | `#fff` |
| `--link-color-hover` | `#000` | `#fff` |
| `--light-switch` | moonstar icon SVG | sun icon SVG |
| `--spinner-color` | `#ccc` | `#ccc` |
| `--page-background` | `#e6e6e6` | `#202020` |
| `--page-vignette-color` | `#cdcdcd` | `#0c0c0c` |

The `--page-*` pair feeds the `page-vignette` mixin (a flat canvas tone with a box-shadow edge falloff — deliberately not a radial gradient, whose page-sized form Firefox can mis-render as a bright dot at its centre). The dashboard preset emits it; an app on a palette of its own defines the pair and includes the mixin itself.

Apps not using the dashboard palette must define at least the `--font-color`/`--background-color`/`--form-background*`/`--border-color*`/`--spinner-color` group themselves — ui components read them with no fallback.

### Required from the consuming app (defined by neither entry, read with no fallback)

| Property | Read by |
| --- | --- |
| `--border-radius` | form controls, modals, tables, `overlay-icon-button` |
| `--icon-size` | `@greendrake/scss-kit` `icon` mixin, icon-bearing ui components |
| `--input-background` | text inputs |
| `--line-height-normal` | text layout in ui components |
| `--modal-background` | modal window |
| `--modal-border-radius` | modal window |
| `--spinner-side-color` | global `.spinner` rule, `img-spinner` mixin (typically `var(--form-background)`) |
| `--link-color` | document link styling in this package |
| `--blockquote-colour` | blockquote styling in this package |
| `--button-background` | `button-colors` mixin |

### Optional (ui components read them with a fallback)

`--cell-padding`, `--circular-progress-arc`, `--circular-progress-label`, `--circular-progress-track`, `--list-row-padding`, `--modal-backdrop-filter`, `--modal-box-shadow`, `--modal-button-min-width`, `--modal-max-width`, `--modal-window-backdrop-filter`, `--row-border-bottom`, `--row-select-color`, `--row-select-color-hover`, `--tree-line-color`.

Component-scoped properties with local defaults (`--checkbox-size`, `--switch-width`, `--switch-height`, `--switch-knob`) are documented per component in `@greendrake/ui`.

## Global spinner contract

This package defines the `spinner` keyframes and the `.spinner` / `body.spinner` / `div.spinner` rules that `@greendrake/ui`'s async controls and `@greendrake/scss-kit`'s `img-spinner` mixin rely on. They consume `--spinner-color` (palette) and `--spinner-side-color` (app).

## Not included

Tooltip (tippy) skin and select-control skin ship with the components that need them in `@greendrake/ui`; this package styles only plain document elements.
