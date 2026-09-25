# @greendrake/eslint-config

An ESLint flat config for a TypeScript + Vue 3 project. A consumer re-exports or spreads it, so a set of packages linted by it agrees by construction rather than by anyone diffing rule lists.

## Install

```sh
bun add -d @greendrake/eslint-config eslint typescript
```

`eslint` (`^9`) and `typescript` (`^6`) are peer dependencies: the consumer runs ESLint and owns both versions. The plugin set the config uses (`@eslint/js`, `typescript-eslint`, `eslint-plugin-vue`, `eslint-config-prettier`, `@stylistic/eslint-plugin`, `globals`) ships as regular dependencies of this package, so a consumer lists none of them.

## Usage

The default export is a flat config (an array). Re-export it as the consumer's whole config, or spread it and append overrides:

```js
// eslint.config.js
import greendrake from '@greendrake/eslint-config'

export default [
    ...greendrake,
    { rules: { /* … */ } }
]
```

Layers, in order: `@eslint/js` recommended, `typescript-eslint` recommended, `eslint-plugin-vue` `flat/recommended`, `eslint-config-prettier`, then this package's own block, then its ignores.

Language options: `ecmaVersion: 'latest'`, `sourceType: 'module'`, the `typescript-eslint` parser as `parserOptions.parser` with `extraFileExtensions: ['.vue']`, so `<script lang="ts">` blocks in SFCs parse as TypeScript. Globals are browser and node together: one workspace mixes app code with its tooling (Vite and Playwright configs, workers, tests), and TypeScript files get real undefined-name checking from tsc regardless.

## Rules

- `@stylistic/object-curly-newline` (`ObjectExpression: { minProperties: 3, multiline: true, consistent: true }`) and `@stylistic/object-property-newline` (`allowAllPropertiesOnSameLine: true`) — structural line-breaking. Prettier at `printWidth: 9999` (`@greendrake/dev-config/prettier`) never wraps for width, so the readability of large literals is governed by shape: an object literal with three or more properties, or any multiline content, breaks open one property per line. Array breaking falls out of Prettier's own printer (an array holding a multiline element goes one element per line), so there are no array rules, and none may be added — their forbid direction collapses deliberately hand-broken lists that Prettier preserves. Both rules are require-only (`consistent: true` tolerates objects expanded by hand below the property threshold), so `eslint --fix` and `prettier -w` reach a stable fixpoint. They come after `eslint-config-prettier`, which switches `@stylistic` rules off wholesale; only width-independent, require-only rules belong there.
- `no-empty` with `allowEmptyCatch: true` — `catch {}` is the deliberate ignore-errors idiom (probing a JSON body); an accidental empty block anywhere else is still an error.
- `@typescript-eslint/no-explicit-any`: error.
- `@typescript-eslint/consistent-type-imports`: error.
- `@typescript-eslint/no-unused-vars` with `ignoreRestSiblings: true` and `^_` as the ignore pattern for arguments, variables and caught errors — destructure-to-omit (`const { drop: _drop, ...rest } = obj`) is the idiomatic way to strip keys, so the omitted bindings are not reported; a leading underscore marks a binding kept only to satisfy a shape (a positional parameter holding its slot, a destructured name documenting what the source provides). tsc exempts `_`-prefixed parameters under `noUnusedParameters`, and matching it stops the two checkers disagreeing about the same name.
- `vue/no-mutating-props` with `shallowOnly: true` — deep mutation of a reactive object prop is a deliberate shared-state contract in places (optimistic updates on a parent-owned reactive); only reassigning the prop itself is an error.
- `vue/attribute-hyphenation` `'always'`, ignoring `ariaLabel` — `:ariaLabel` stays camelCase on components. The rule already ignores the kebab `aria-*` form, but hyphenating the camelCase one to `:aria-label` breaks vue-tsc, which matches a component's typed `ariaLabel` prop by exact key.
- Off, as deliberate patterns whose warnings would be permanent noise: `vue/no-v-html` (`v-html` renders trusted HTML only — i18n, content pipeline), `vue/require-default-prop` (an optional TS prop legitimately models `T | undefined` without a default), `vue/one-component-per-file` (small render-helper components live at module scope beside their SFC). Also off: `vue/multi-word-component-names`, `vue/html-indent`, `vue/max-attributes-per-line`, `vue/singleline-html-element-content-newline`, `vue/html-self-closing`.

## Ignores

`**/dist*/**`, `**/node_modules/**`, `**/playwright-report/**`, `**/test-results/**`, `**/.playwright-mcp/**`. `dist*` rather than `dist` because apps emit sibling build outputs beside it, and every one of them is generated code no rule has an opinion about — so no tracked source directory may have a name starting with `dist`.
