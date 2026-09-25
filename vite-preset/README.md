# @greendrake/vite-preset

A Vite config factory and build-env helpers for Vue 3 single-page apps. `vueSpa()` owns the conventions an app would otherwise hand-copy: the Vue plugin, the strict `VITE_PORT` pin, the `@` alias, the dedupe list, unhashed font asset names and an HMR guard against circular imports. Apps pass app-specific config through `overrides`, which is deep-merged last.

The package is Node-only, except the `./api-target` entry, which is pure and also runs in the browser bundle. It ships TypeScript, and Node does not type-strip files under `node_modules`, so a `vite.config.ts` importing it runs under bun: `bun --bun vite …`.

## Install

```sh
bun add -d @greendrake/vite-preset vite
```

`@vitejs/plugin-vue` comes with the package; `vite` (≥ 8) is a peer dependency.

## `vueSpa(options)`

```ts
import { defineConfig } from 'vite'
import { vueSpa } from '@greendrake/vite-preset'

export default defineConfig(
    vueSpa({
        dirname: import.meta.dirname,
        define: { __API_URL__: process.env.API_URL },
        overrides: { plugins: [myPlugin()] }
    })
)
```

| Option | Effect |
| --- | --- |
| `dirname` | The app root (`import.meta.dirname` of the vite config); anchors the `@` alias at `<dirname>/src`. |
| `define` | Compile-time globals. Values are serialised for you: a string becomes a string literal, a boolean `true`/`false`, an object an object literal. |
| `scssLoadPaths` | Extra `@use` resolution roots for Sass (e.g. the app's `src/scss`). |
| `port` | `'env'` (default): `VITE_PORT` is required whenever a server starts. A number pins that port. `false` disables pinning, for an app served on any free port. |
| `htmlPlaceholders` | Literal substitutions applied to `index.html` before Vite processes it (see `htmlPlaceholders` below). |
| `staticRoots` | Directories outside the app served at public URL prefixes (see `staticRoots` below). |
| `overrides` | A `UserConfig` deep-merged last — the escape hatch for `server.proxy`, extra plugins, build tweaks. |

What the preset sets:

- plugins: `@vitejs/plugin-vue` and the HMR circular-import guard, plus the `staticRoots` and `htmlPlaceholders` plugins when their options are given;
- `server.port`/`preview.port` from `VITE_PORT` with `strictPort`, for `vite dev` and `vite preview` only — builds run in deploy contexts that never define a dev port;
- `server.fs.allow`: the parent of the app root (a workspace), and every static root's directory;
- `resolve.alias`: `@` → `<dirname>/src`; `resolve.dedupe`: `vue`, `pinia`, `vue-router` — each must also be a direct dependency of the app, or Vite fails to resolve it;
- `optimizeDeps`: every source-shipped dependency of the app is excluded from pre-bundling, and `vue`, `pinia` and `vue-router` (those the app declares) are pinned into it. A source-shipped package is TypeScript and SFC rather than a build; pre-bundling one from `node_modules` would inline its TypeScript modules and their imports while externalising its `.vue` files, leaving the app with two copies of Pinia and everything else. Served raw, a package resolves to one copy of itself and meets the app on the pre-bundled framework modules. Workspace-linked packages are never pre-bundled, so in a monorepo the exclusion decides no pre-bundling either way — it still matters for what the scanner sees, below. Which dependencies those are is the `sourcePackages` option — an entry ending in `/` matches a whole scope, anything else an exact package name. It defaults to `['@greendrake/']`; an app with source packages of its own lists them, and lists that scope again if it uses any:

  ```ts
  vueSpa({ dirname: import.meta.dirname, sourcePackages: ['@greendrake/', '@acme/ui', 'my-design-system'] })
  ```

  Excluding a package also hides it from Vite's dependency scanner, which externalises every excluded id and so never reads what that package imports. The third-party packages behind a source package are therefore pinned into the pre-bundle too, or the first browser request for one is a discovery: Vite re-runs the optimiser mid-session and full-reloads every open page — which under a parallel E2E run lands in the middle of tests in flight. The preset walks the app's runtime dependencies, follows the source packages among them, and pins each package they depend on, plus each peer they declare that the app itself supplies at runtime. Each is named in Vite's `parent > child` form unless the app declares it too, in which case its plain name — the id the scanner itself would produce — is used instead. Three things are left out: a package published only under subpaths, which no `optimizeDeps` entry can name and which Vite reaches through the subpath ids that import it anyway; a peer that is itself a source package, which the app's own declaration of it puts under the same rules as any other source package — excluded, and walked in its turn; and anything behind a source package the app keeps among its dev dependencies, which by the app's own reckoning is tooling (a build-tool integration shipped beside a package's components, a native-build helper) that browser code never reaches. That split is what an app is asserting when it puts a package in one list rather than the other, so put build-time source packages in `devDependencies`.

- `build`: font files (`woff2`, `woff`, `eot`, `ttf`, `otf`) are emitted unhashed under `assets/fonts/`, so `index.html` can prefetch them by a stable name; everything else is hashed.

`vueSpa` returns the *function* form of a Vite config (`ConfigEnv → UserConfig`). An app that needs the `ConfigEnv` before building its options wraps the call itself:

```ts
export default defineConfig(configEnv =>
    vueSpa({
        dirname: import.meta.dirname,
        define: { __API_TARGET_ENV__: apiTargetEnv(configEnv.command === 'serve') }
    })(configEnv)
)
```

### HMR circular-import guard

An HMR update whose accepted module sits inside an import cycle cannot be re-executed reliably; Vite's client masks that with a silent full-page reload. The preset intercepts such updates on the dev server and replaces them with an error overlay naming the offending modules, so the cycle gets broken rather than papered over. `vite --debug hmr` logs its path.

## Env helpers

```ts
import { loadAppEnv, requirePort, envBool, deriveImageBaseUrl, apiTargetEnv, buildHash } from '@greendrake/vite-preset'
```

- `loadAppEnv(mode, dirname)` — the app's `.env` / `.env.<mode>` files, every key (not only `VITE_`-prefixed ones): they carry plain build inputs, and the `export` prefix they may be written with is stripped so the same file can be shell-sourced. `process.env` outranks the file. Do not name the files `.env.development` / `.env.production`: bun auto-loads those into `process.env` at startup, and whichever one it picked would then outrank the mode's file.
- `requirePort()` — `VITE_PORT`, required; the single port convention.
- `envBool(value)` — `"1"`, `"t"`, `"true"` (any case) are true; `"0"`, `"false"`, unset and everything else are false. `Boolean(process.env.X)` would take the literal string `"0"` as true.
- `deriveImageBaseUrl(env = process.env)` — the prefix for object-storage image URLs. A defined `S3_PREFIX` wins outright, even when empty; otherwise `AWS_S3_ENDPOINT/AWS_S3_BUCKET`; otherwise `''`.
- `apiTargetEnv(dev, paths?)` — the `ApiTargetEnv` of `resolveApiTarget`, read from `API_PORT`, `PUBLIC_FE_HOST`, `PUBLIC_API_HOST`, with optional `apiPath`/`wsPath`. Bake it into one define and feed the same object to anything else that resolves the host.
- `buildHash()` — `git rev-parse --short HEAD`, for a `__BUILD_HASH__` define. If git fails the build fails.

## Plugins

### `htmlPlaceholders(map)`

Substitutes build-time values into `index.html` before Vite processes it — a generated inline script, a build hash, PWA metadata. Keys are matched literally (convention: `__NAME__`); an empty map is an error. `vueSpa`'s `htmlPlaceholders` option installs it.

### `staticRoots(roots)` and `staticRootFiles(root)`

Some assets belong to the product rather than to the frontend — a locale catalogue a backend renders from too, prose a content page shows. A `StaticRoot` is `{ dir, urlPath, transform? }`: an absolute directory, the URL prefix it appears under (e.g. `/i18n`), and an optional per-file rewrite applied on the way out. The plugin serves the root by middleware in dev and copies it into `dist/` at build, so the two modes publish the same bytes; `.yaml`/`.yml` and `.json` get their content types, anything else is `text/plain`. Requests cannot climb out of the root. `staticRootFiles(root)` lists what a root publishes as URL paths, for a service worker's precache list. Installed by the `staticRoots` option.

## `./api-target`

```ts
import { resolveApiTarget, browserRuntime } from '@greendrake/vite-preset/api-target'
```

The single source of truth for where the API lives, pure and environment-free, so the same rules serve a vite config and the browser bundle. `resolveApiTarget(env, runtime?)` returns `{ apiUrl, wsUrl }`:

- dev (`env.dev`): `http`/`ws` against `localhost:<env.apiPort>`; with `env.publicApiHost` set instead, `https`/`wss` against that host (a dev server pointed at a remote API). Neither set is an error.
- prod: the same bundle can be served on two hosts. On `env.publicFeHost` — where a CDN may not proxy WebSockets — the API is `env.publicApiHost`; on any other host it is same-origin, i.e. `runtime.host`. The scheme follows `runtime.https`. `runtime` is required, and `publicApiHost` is required when serving on `publicFeHost`.
- `env.apiPath` and `env.wsPath` replace the default `/v1/api` and `/v1/ws`; a backend that routes at the host root sets `apiPath: ''`.

`browserRuntime()` is the `runtime` argument as a browser bundle supplies it: `{ host: location.host, https: location.protocol === 'https:' }`.

## Sass: import shared style packages by bare specifier

`@use '@acme/font'` — never `@use 'pkg:@acme/font'`, and do not register Sass's `NodePackageImporter`.

Vite resolves bare specifiers with its own Sass importer and rebases relative `url()` references inside the imported package against that package's own source location, so `@font-face { src: url('./fonts/X.woff2') }` in a font package resolves, gets emitted into `dist/assets/`, and the emitted CSS points at the built asset.

`NodePackageImporter` resolves the same `@use` to a path Vite's CSS pipeline does not rebase against. The `url()` then survives verbatim into the output, no font asset is emitted, and the build warns `didn't resolve at build time, it will remain unchanged to be resolved at runtime` — every font 404s at runtime. This is a property of the importer, not of any Vite version.

The corollary: the `url()` literal must be written in the owning package's own `.scss`, because that is the file the rebasing is relative to — a shared `@font-face` mixin that builds the URL itself breaks it.

If a future Vite drops bare-specifier support, migrate to `loadPaths` or move the `@font-face` blocks into the consuming apps — not to `pkg:`.
