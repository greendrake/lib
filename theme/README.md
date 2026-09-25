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

## CSS custom properties

What this package's two entries define, and what its own rules and mixins read from the app. Components styled against custom properties of their own list them in their own packages; an app on this theme defines whichever of those it leaves open (typically in its palette maps or `:root`).

### Defined by `@greendrake/theme` (root entry)

| Property | Value | Notes |
| --- | --- | --- |
| `--space-1` … `--space-6` | 4/8/12/16/24/28px | spacing scale |
| `--font-xs` … `--font-2xl` | 11/13/16/20/24/32px | font-size scale |
| `--overlay-button-bg`, `--overlay-button-bg-hover` | `rgb(0 0 0 / 60%)`, `rgb(0 0 0 / 85%)` | consumed by the `overlay-icon-button` mixin |
| `--error-color` | `#ff4800` | semantic status colour |
| `--warning-color` | `#890095` | semantic status colour |
| `--accent-color` | `#0aa13e` | accent for active and checked states |

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
| `--modal-background`, `--modal-border-radius` | `var(--background-color)`, `var(--border-radius)` | same |
| `--modal-box-shadow`, `--modal-backdrop-filter`, `--modal-window-backdrop-filter` | a light glow, `brightness(0.4)`, `blur(8px)` | same |

The `--page-*` pair feeds the `page-vignette` mixin (a flat canvas tone with a box-shadow edge falloff — deliberately not a radial gradient, whose page-sized form Firefox can mis-render as a bright dot at its centre). The dashboard preset emits it; an app on a palette of its own defines the pair and includes the mixin itself. The `--modal-*` group is the dashboard look's modal chrome: a dark-glass window over a dimmed backdrop.

An app not on the dashboard palette defines the palette group itself: this package's own rules read `--font-color`, `--background-color`, `--form-background*`, `--border-color*`, `--link-color-hover` and `--spinner-color` with no fallback.

### Required from the consuming app (defined by neither entry, read with no fallback)

| Property | Read by |
| --- | --- |
| `--border-radius` | form-control styling, `overlay-icon-button` |
| `--spinner-side-color` | global `.spinner` rule (typically `var(--form-background)`) |
| `--link-color` | document link styling |
| `--blockquote-colour` | blockquote styling |
| `--button-background` | `button-colors` mixin |

## Global spinner

This package defines the `spinner` keyframes and the `.spinner` / `body.spinner` / `div.spinner` rules, for components that show progress by toggling a `spinner` class and for `@greendrake/scss-kit`'s `img-spinner` mixin. They consume `--spinner-color` (palette) and `--spinner-side-color` (app).

## Not included

Component skins (tooltips, select controls) ship with the components that need them; this package styles only plain document elements.
