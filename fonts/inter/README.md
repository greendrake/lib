# @greendrake/font-inter

The Inter typeface as self-hosted woff2 files (SIL Open Font License 1.1, `OFL.txt` in the package), with `@font-face` registration through `@greendrake/scss-kit`'s generator and an opt-in entry that makes Inter the document default.

## Install

```sh
bun add @greendrake/font-inter
```

`npm install @greendrake/font-inter` works equally. `@greendrake/scss-kit` is a dependency and installs with it.

## Usage

```scss
@use '@greendrake/font-inter'; // registers the faces
@use '@greendrake/font-inter/default'; // :root { font-family: Inter, Helvetica, Arial, sans-serif }
```

The root entry calls `kit.font-faces('Inter', …)` for weights 100–900 (Thin to Black) in normal and italic, 18 faces in all, each `format('woff2')` with `font-display: swap`. It does not touch `:root`.

The `./default` entry calls `kit.use-as-default('Inter', Helvetica, Arial, sans-serif)` and nothing else: it sets the document default without registering faces, so it goes alongside the root entry. Omit it to keep Inter as a `font-family` applied to selected elements only.

Both entries are consumed by bare specifier, as above, never a `pkg:` URL and never with Sass's `NodePackageImporter` registered: Vite rebases the font `url()`s inside `main.scss` against the package's own location only for bare-specifier imports, and any other path leaves them unresolved with no font asset emitted.
