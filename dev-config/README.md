# @greendrake/dev-config

TypeScript, ESLint and Prettier configuration for a Vue 3 + Vite project written in TypeScript. Each entry is a config file a consumer extends or re-exports, so a package carries no tooling opinions of its own beyond the one line that points here — and a set of them agrees by construction rather than by anyone diffing rule lists.

## Install

```sh
bun add -d @greendrake/dev-config
```

`typescript` (`^6`) is a peer dependency: the consumer owns its patch version, within the major these configs are written for. ESLint and the plugin set the flat config uses (`@eslint/js`, `typescript-eslint`, `vue-eslint-parser`, `eslint-plugin-vue`, `eslint-config-prettier`, `@stylistic/eslint-plugin`, `globals`) ship as regular dependencies of this package, so a consumer lists none of them.

## TypeScript

Three tsconfig entries, each with `noEmit`, and an overlay for any of them: they configure type-checking (`tsc --noEmit`, `vue-tsc --noEmit`), not emit — bundling is Vite's job.

`./tsconfig/base.json` is the common ground: `target` and `module` `ESNext`, `moduleResolution: "bundler"`, `lib: ["ESNext", "DOM", "DOM.Iterable"]`, `strict`, `noUnusedLocals`, `noUnusedParameters`, `noFallthroughCasesInSwitch`, `verbatimModuleSyntax`, `isolatedModules`, `esModuleInterop`, `skipLibCheck`, `resolveJsonModule`, `noEmit`. Framework-free browser libraries extend it directly.

`./tsconfig/vue.json` extends `base.json` with `jsx: "preserve"` and `jsxImportSource: "vue"`, and sets `vueCompilerOptions` for vue-tsc: `strictTemplates: true` plus `dataAttributes: ["data-*"]`. Strict templates reject `data-*` attributes by default; they are standard HTML and several `@greendrake/ui` components rely on them. The allowance lives in the shared config rather than in `@greendrake/ui` because a package consuming `@greendrake/ui` source type-checks its SFCs under the consumer's own `vueCompilerOptions`. Vue apps and Vue component libraries extend it.

`./tsconfig/node.json` extends `base.json` with `lib: ["ESNext"]` (no DOM) and `types: ["node"]`; the consumer supplies `@types/node`. Node-only packages — tooling, scripts, servers — extend it.

`./tsconfig/strict.json` is the overlay: `exactOptionalPropertyTypes`, `noUncheckedIndexedAccess`, `noImplicitOverride` and `noImplicitReturns`, and nothing else, extended after one of the three (`"extends": ["@greendrake/dev-config/tsconfig/node.json", "@greendrake/dev-config/tsconfig/strict.json"]`). A package that ships source is compiled under whatever options its consumer runs, since TypeScript checks every file in a program under one set; the packages a service imports — `util`, `rpc`, `rpc-server`, `service-state` — and what a vite config imports — `vite-preset`, and `@greendrake/dash/vite` — check themselves under this overlay, so a consumer that turns these on compiles them as they are.

### Ambient types

TypeScript 6 includes no `@types` package unless the project names one, so a package states what its own sources and tests need — `bun` for `bun:test` and the `Bun` global, `vite/client` for `import.meta.env` and the modules a side-effect asset import resolves to, `node` for `process` and the `node:` builtins. `types` in an extending config replaces the inherited list rather than adding to it, so a package needing two names both of them:

```json
{ "compilerOptions": { "types": ["vite/client", "bun"] } }
```

A package that ships source carries this further: an ambient declaration its own sources depend on must reach whatever compiles them, which a `types` entry in its private `tsconfig.json` does not. `@greendrake/ui` and `@greendrake/dash` import a stylesheet for effect, so each ships the declaration beside the source and pulls it in with a path reference from the importing file.

An app's `tsconfig.json`:

```json
{
    "extends": "@greendrake/dev-config/tsconfig/vue.json",
    "compilerOptions": {
        "paths": {
            "@/*": ["./src/*"]
        }
    },
    "include": ["src"]
}
```

`@/*` → `./src/*` is the convention: it is the type-checker side of the `@` alias `@greendrake/vite-preset` configures for the bundler, so both resolve `@/…` imports to the same files. A library without such imports needs only `extends` and `include`.

## ESLint

`./eslint` is an ESLint flat config (an array). Re-export it as the consumer's whole config, or spread it and append overrides:

```js
// eslint.config.js
import greendrake from '@greendrake/dev-config/eslint'

export default [
    ...greendrake,
    { rules: { /* … */ } }
]
```

Layers, in order: `@eslint/js` recommended, `typescript-eslint` recommended, `eslint-plugin-vue` `flat/recommended`, `eslint-config-prettier`, then this package's own block, then its ignores.

Language options: `ecmaVersion: 'latest'`, `sourceType: 'module'`, the `typescript-eslint` parser as `parserOptions.parser` with `extraFileExtensions: ['.vue']`, so `<script lang="ts">` blocks in SFCs parse as TypeScript. Globals are browser and node together: one workspace mixes app code with its tooling (Vite and Playwright configs, workers, tests), and TypeScript files get real undefined-name checking from tsc regardless.

Rules the block sets:

- `@stylistic/object-curly-newline` (`ObjectExpression: { minProperties: 3, multiline: true, consistent: true }`) and `@stylistic/object-property-newline` (`allowAllPropertiesOnSameLine: true`) — structural line-breaking. Prettier at `printWidth: 9999` never wraps for width, so the readability of large literals is governed by shape: an object literal with three or more properties, or any multiline content, breaks open one property per line. Array breaking falls out of Prettier's own printer (an array holding a multiline element goes one element per line), so there are no array rules, and none may be added — their forbid direction collapses deliberately hand-broken lists that Prettier preserves. Both rules are require-only (`consistent: true` tolerates objects expanded by hand below the property threshold), so `eslint --fix` and `prettier -w` reach a stable fixpoint. They come after `eslint-config-prettier`, which switches `@stylistic` rules off wholesale; only width-independent, require-only rules belong there.
- `no-empty` with `allowEmptyCatch: true` — `catch {}` is the deliberate ignore-errors idiom (probing a JSON body); an accidental empty block anywhere else is still an error.
- `@typescript-eslint/no-explicit-any`: error.
- `@typescript-eslint/consistent-type-imports`: error.
- `@typescript-eslint/no-unused-vars` with `ignoreRestSiblings: true` and `^_` as the ignore pattern for arguments, variables and caught errors — destructure-to-omit (`const { drop: _drop, ...rest } = obj`) is the idiomatic way to strip keys, so the omitted bindings are not reported; a leading underscore marks a binding kept only to satisfy a shape (a positional parameter holding its slot, a destructured name documenting what the source provides). tsc exempts `_`-prefixed parameters under `noUnusedParameters`, and matching it stops the two checkers disagreeing about the same name.
- `vue/no-mutating-props` with `shallowOnly: true` — deep mutation of a reactive object prop is a deliberate shared-state contract in places (optimistic updates on a parent-owned reactive); only reassigning the prop itself is an error.
- `vue/attribute-hyphenation` `'always'`, ignoring `ariaLabel` — `:ariaLabel` stays camelCase on components. The rule already ignores the kebab `aria-*` form, but hyphenating the camelCase one to `:aria-label` breaks vue-tsc, which matches a component's typed `ariaLabel` prop by exact key.
- Off, as deliberate patterns whose warnings would be permanent noise: `vue/no-v-html` (`v-html` renders trusted HTML only — i18n, content pipeline), `vue/require-default-prop` (an optional TS prop legitimately models `T | undefined` without a default), `vue/one-component-per-file` (small render-helper components live at module scope beside their SFC). Also off: `vue/multi-word-component-names`, `vue/html-indent`, `vue/max-attributes-per-line`, `vue/singleline-html-element-content-newline`, `vue/html-self-closing`.

Ignores: `**/dist*/**`, `**/node_modules/**`, `**/playwright-report/**`, `**/test-results/**`, `**/.playwright-mcp/**`. `dist*` rather than `dist` because apps emit sibling build outputs beside it, and every one of them is generated code no rule has an opinion about — so no tracked source directory may have a name starting with `dist`.

## Prettier

`./prettier` is consumed through the `prettier` key in the consumer's `package.json`:

```json
"prettier": "@greendrake/dev-config/prettier"
```

Settings: `printWidth: 9999`, `tabWidth: 4`, `arrowParens: 'avoid'`, `singleQuote: true`, `trailingComma: 'none'`, `semi: false`. Width never governs line-breaking; the `@stylistic` pair in the ESLint config governs it by shape instead.

One override: `*.jsx` and `*.tsx` get `printWidth: 120`. JSX is the one place the no-wrapping default fails. A Vue template is its own block, so width never governs it; a JSX element is an expression nested inside the code that returns it, and at 9999 a single component with a handful of props and a class list becomes one 500-character line. Structural breaking cannot help — it governs object literals, not elements — so JSX gets a real width and Prettier breaks it.
