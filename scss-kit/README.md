# @greendrake/scss-kit

Framework-free SCSS primitives: viewport breakpoints (single-sourced with a TypeScript twin), a z-index layer map, a data-URI icon-mask registry, an `@font-face` generator and a handful of mixins. `@use`-ing the root entry emits no CSS; everything is a map, function or mixin the consumer includes.

## Install

```sh
bun add @greendrake/scss-kit
```

`npm install @greendrake/scss-kit` works equally. The package ships SCSS and TypeScript source with no build step and no `.d.ts`. It is consumed via the bare specifier `@use '@greendrake/scss-kit'`, which Vite's Sass importer resolves from `node_modules`.

## Usage

```scss
@use '@greendrake/scss-kit' as kit;

.sidebar {
    z-index: kit.z('sticky');
    @include kit.respond-below('narrow') {
        display: none;
    }
}

.close-button {
    @include kit.icon;
    @include kit.i('close');
}
```

Every name-keyed function below (`breakpoint`, `z`, `icon-svg`) raises a Sass `@error` listing the known keys when given an unknown name. `_lookup.scss`, which implements that, is internal and not forwarded.

## Breakpoints

`$breakpoints` in `_breakpoints.scss` and the TypeScript module `src/breakpoints.ts` are both generated from `src/breakpoints.json` by `scripts/generate.ts` (`bun run generate` in the package source, runs under Bun), so the values and the boundary semantics cannot drift apart. The JSON is the only editable source; the two generated files carry a GENERATED header and both ship in the package.

```json
{
    "tooNarrow": 320,
    "narrow": 600
}
```

A breakpoint value is the inclusive upper bound of the viewport range below it: "below" matches `width <= value`, "above" matches `width >= value + 1px`. `narrow` (600) is the content/wide-screen boundary: a viewport is wide from 601px up.

SCSS (root entry):

- `$breakpoints` — `('tooNarrow': 320px, 'narrow': 600px)`.
- `breakpoint($name)` — the value for a name.
- `respond-below($name) { … }` — emits `@media (max-width: <value>)`.
- `respond-above($name) { … }` — emits `@media (min-width: <value + 1px>)`.
- `respond-above-height($name) { … }` — emits `@media (min-height: <value + 1px>)`.

TypeScript (`@greendrake/scss-kit/breakpoints`):

```ts
import { breakpoints, mediaAbove, mediaBelow, type BreakpointName } from '@greendrake/scss-kit/breakpoints'

breakpoints.narrow // 600
mediaBelow('narrow') // '(max-width: 600px)'
mediaAbove('narrow') // '(min-width: 601px)'
window.matchMedia(mediaAbove('narrow')).matches
```

`breakpoints` is `as const`; `BreakpointName` is `keyof typeof breakpoints`; `mediaAbove`/`mediaBelow` return media-query strings with the same semantics as the SCSS mixins.

## Z layers

- `$z-layers` — `('below': -1, 'base': 0, 'raised': 1, 'spinner': 10, 'sticky': 50, 'modal': 100, 'toast': 200)`.
- `z($name)` — the value for a name.

```scss
.toast {
    z-index: kit.z('toast');
}
```

## Icons

Data-URI SVG icons rendered through `mask-image`, so the element's `background-color` paints them.

- `$icons` — map of name → `url("data:image/svg+xml,…")`. Names: `eye`, `eye-off`, `copy`, `delete`, `close`, `menu`, `expand`, `collapse`, `download`, `chevron-left`, `chevron-right`, `link`, `refresh`, `edit`, `quote`, `qr`, `logout`, `user`, `settings`, `sun`, `moonstar`.
- `icon-svg($name)` — the `url()` for a name.
- `icon-mask($svg)` — emits `mask-image: $svg`. The extension point for icons outside the registry: wrap it in an app mixin with your own data-URI SVG.
- `i($name)` — `icon-mask(icon-svg($name))`.
- `icon-base` — emits `border: 0; display: inline-block; mask-repeat: no-repeat; background-color: var(--font-color); background-image: none`.
- `icon` — `icon-base` plus `mask-position: center center` and `width`/`height` of `var(--icon-size)`.

`--font-color` and `--icon-size` are read with no fallback; the consuming app defines them (`@greendrake/theme` documents the custom-property contract).

```scss
.eye-toggle {
    @include kit.icon;
    @include kit.i('eye');
    &[aria-pressed='true'] {
        @include kit.i('eye-off');
    }
}

@mixin i-foo {
    @include kit.icon-mask(url('data:image/svg+xml,…'));
}
```

## Font faces

- `font-faces($family, $faces, $display: swap)` — emits one `@font-face` per (weight, style) entry with `font-family`, `src: <url> format('woff2')`, `font-weight`, `font-style` and `font-display: $display`. `$faces` is a map `(weight: (style: url))`.
- `use-as-default($family, $fallbacks...)` — emits `:root { font-family: $family, <fallbacks> }`. Registering faces deliberately does not touch `:root`; a family becomes the document default only through this mixin.

```scss
@use '@greendrake/scss-kit' as kit;

@include kit.font-faces(
    'Example',
    (
        400: (
            normal: url('./fonts/Example-Regular.woff2'),
            italic: url('./fonts/Example-Italic.woff2')
        ),
        700: (
            normal: url('./fonts/Example-Bold.woff2')
        )
    )
);
@include kit.use-as-default('Example', Helvetica, Arial, sans-serif);
```

The `url()` literals must be written in the font package's own `.scss` file: Vite rebases them relative to that file when the package is consumed from `node_modules`, and only when the package is imported by bare specifier (never a `pkg:` URL, never with Sass's `NodePackageImporter` registered). `@greendrake/font-inter` is a font package built on exactly these two mixins.

## Mixins

- `monospace` — `font-family: monospace, monospace`.
- `disabled` — `pointer-events: none; opacity: 0.5; cursor: default; user-select: none`.
- `apply-props($props)` — emits each `key: value` pair of a map as a declaration.
- `control-clear` — the × clear button rendered inside a select-shaped control, before the chevron: an inline-flex box held at the WCAG 2.2 target-size minimum (24×24, SC 2.5.8) with a negative block margin (computed from `var(--line-height-tight, 18px)`) so it grows into the control's padding instead of its height; glyph size `var(--control-clear-font-size, 18px)`; opacity 0.5, 1 on hover.
- `control-chevron($point: down)` — a dropdown/drill chevron built from two borders (no icon dependency): 7×7px, `currentColor`, opacity 0.6, `transform` transition; `$point: right` rotates it to point right.
- `img-spinner($size: 24px)` — a centred spinner `::before` on an `img` container. Requires the global `spinner` keyframes (provided by `@greendrake/theme`) and the `--spinner-color`/`--spinner-side-color` custom properties.

```scss
.select-clear {
    @include kit.control-clear;
}
.select-chevron {
    @include kit.control-chevron;
}
.drill {
    @include kit.control-chevron(right);
}
.thumb {
    @include kit.img-spinner(32px);
}
.code {
    @include kit.monospace;
}
.card {
    @include kit.apply-props((padding: 8px, gap: 4px));
}
```
