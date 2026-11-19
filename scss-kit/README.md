# @greendrake/scss-kit

The SCSS that an app's stylesheets keep needing: viewport breakpoints shared with TypeScript, named z-index layers, icons drawn as masks, an `@font-face` generator and a handful of mixins. It is framework-free, and `@use`-ing it emits no CSS: everything is a map, function or mixin the consumer includes.

```sh
bun add @greendrake/scss-kit
```

## Why

Some values in a design have to be the same in many places, and CSS has no one place to keep them. Breakpoints are needed in stylesheets and in scripts, and z-index values in every stylesheet that puts one thing over another. Written by hand in each place, they drift apart. A Sass map can give such values names, but `map.get` returns `null` for a mistyped name, and the declaration is dropped with no warning.

Here one JSON file generates the breakpoints for both SCSS and TypeScript. The z-index values come from one ordered map of names. A lookup of an unknown name stops the build.

## Trade-offs

- **The breakpoints and layers are the package's own.** There are two breakpoints, at 320px and 600px, and seven z layers. The breakpoints are fixed when the package is generated, and a consumer cannot give the generator other values. A design with other breakpoints has no use for that part of the package.
- **Icons are one colour.** An icon is applied as a mask, and a mask has no colour of its own.

## Overview

- **Breakpoints in two languages.** A stylesheet tests the viewport with a media query, and a script tests it with `matchMedia`. Written by hand in both, the numbers drift apart. So does the boundary: `(max-width: 600px)` and `(min-width: 600px)` both match at exactly 600px. Here one JSON file generates both the SCSS map and a TypeScript module, and both sides apply one rule for the boundary.
- **Named z-index layers.** `z-index: 100` in one file and `99` in another do not say what was meant to cover what. `z('modal')` and `z('toast')` come from one ordered map.
- **A mistyped name stops the build.** Sass's `map.get` returns `null` for a key the map lacks, and Sass leaves out a declaration whose value is `null`. The declaration is dropped with no warning. Every lookup here raises an error that lists the known names instead.
- **Icons that take the colour of the text.** Each icon is an SVG in a data URI, applied as a mask. The element's background colour paints it, so one icon works in every colour and every theme, and needs no request of its own.

The `@font-face` generator and the mixins are smaller pieces of the same kind: declarations that would otherwise be written again in each stylesheet.

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

## Usage

The package installs with `bun add`, as above. `npm install @greendrake/scss-kit` works equally.

The package ships SCSS and TypeScript source with no build step and no `.d.ts`. It is consumed via the bare specifier `@use '@greendrake/scss-kit'`, which Vite's Sass importer resolves from `node_modules`. The `./breakpoints` entry is TypeScript, so the consuming app's bundler has to compile it.

Every function below that takes a name (`breakpoint`, `z`, `icon-svg`) raises a Sass `@error` when it is given an unknown name. The error lists the known keys. `_lookup.scss` implements that. It is internal and not forwarded.

## Breakpoints

`$breakpoints` in `_breakpoints.scss` and the TypeScript module `src/breakpoints.ts` are both generated from `src/breakpoints.json`. The generator is `scripts/generate.ts`: `bun run generate` in the package source, which runs under Bun. So the values and the rule for the boundary cannot drift apart. The JSON is the only editable source. The two generated files start with a GENERATED comment, and both ship in the package.

```json
{
    "tooNarrow": 320,
    "narrow": 600
}
```

A breakpoint value is the inclusive upper bound of the viewport range below it: "below" matches `width <= value`, "above" matches `width >= value + 1px`. `narrow` (600) is where a wide screen starts: a viewport is wide from 601px up.

Below and above leave a 1px gap. A viewport whose width falls in between, 600.5px say, matches neither. Browsers can report such widths under zoom or display scaling.

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

`breakpoints` is `as const`. `BreakpointName` is `keyof typeof breakpoints`. `mediaAbove` and `mediaBelow` return media-query strings that match the same widths as the SCSS mixins.

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

- `$icons` — map of name → `url("data:image/svg+xml,…")`. Names: `eye`, `eye-off`, `copy`, `delete`, `close`, `menu`, `expand`, `collapse`, `download`, `chevron-left`, `chevron-right`, `link`, `refresh`, `edit`, `quote`, `qr`, `logout`, `user`, `settings`, `sun`, `moonstar`, `info`, `shield-check`, and a sheet of paper for a file: `file`, and `file-image`, `file-video`, `file-audio`, `file-document`, `file-spreadsheet`, `file-presentation`, `file-archive`, each with its kind's glyph on it.
- `icon-svg($name)` — the `url()` for a name.
- `icon-mask($svg)` — emits `mask-image: $svg`. It is the way to add an icon that is not in `$icons`: wrap it in a mixin of the app's, with your own data-URI SVG.
- `i($name)` — `icon-mask(icon-svg($name))`.
- `icon-base` — emits `border: 0; display: inline-block; mask-repeat: no-repeat; background-color: var(--font-color); background-image: none`.
- `icon` — `icon-base` plus `mask-position: center center` and `width`/`height` of `var(--icon-size)`.

`--font-color` and `--icon-size` are read with no fallback. The consuming app defines them.

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
- `use-as-default($family, $fallbacks...)` — emits `:root { font-family: $family, <fallbacks> }`. Registering faces deliberately does not touch `:root`. A family becomes the document default only through this mixin.

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

The `url()` literals must be written in the font package's own `.scss` file. Vite rebases them relative to that file when the package is consumed from `node_modules`. It does so only when the package is imported by bare specifier: never through a `pkg:` URL, and never with Sass's `NodePackageImporter` registered.

## Mixins

- `monospace` — `font-family: monospace, monospace`.
- `disabled` — `pointer-events: none; opacity: 0.5; cursor: default; user-select: none`.
- `dragging($cursor)` — for the element that contains a pointer drag in progress: `user-select: none`, and `$cursor` (`!important`) on every descendant.
- `drop-marks` — for the rows of a list a drag moves items in. A row with `data-drop="before"` or `"after"` gets a 2px line on that edge, and one with `data-drop="inside"` a 1px outline: where the dragged item would land. A row with `data-dragged`, the one being dragged, is faded to 0.4 opacity. The lines and outline are inset box-shadows in `var(--accent-color)`, which the app defines.
- `apply-props($props)` — emits each `key: value` pair of a map as a declaration.
- `control-clear` — the × clear button rendered inside a control shaped like a select box, before the chevron. It is an inline-flex box kept at the WCAG 2.2 target-size minimum (24×24, SC 2.5.8). A negative block margin, computed from `var(--line-height-tight, 18px)`, lets the box extend into the control's padding, so that it does not make the control taller. The glyph size is `var(--control-clear-font-size, 18px)`. Opacity is 0.5, and 1 on hover.
- `control-chevron($point: down)` — a chevron for a dropdown or a drill-down item, built from two borders, so it needs no icon: 7×7px, `currentColor`, opacity 0.6, rotated with `transform`. `$point: right` rotates it to point right.
- `img-spinner($size: 24px)` — a centred spinner `::before` on an `img` container. It requires global `spinner` keyframes and the `--spinner-color`/`--spinner-side-color` custom properties. The app provides all three.
- `column-filled-by-last` — a flex column whose last child takes the room the others leave (`flex: 1`). The last child can shrink below its content, so that it can scroll that content itself (`min-height: 0`). The column has `min-height: 0` too, so that it can be the last child of another such column.
- `hover-grow` — scales the element 1.2× under the pointer, with a 0.3 s `transform` transition. It raises the element to `z-index: 1` while grown, and drops it back only once the shrink has finished. The element must take part in stacking: positioned, or a flex/grid item.

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
    @include kit.apply-props(
        (
            padding: 8px,
            gap: 4px
        )
    );
}
```
