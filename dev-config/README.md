# @greendrake/dev-config

TypeScript and Prettier configuration for a Vue 3 + Vite project written in TypeScript. Each entry is a config file a consumer extends or re-exports, so a package carries no tooling opinions of its own beyond the one line that points here — and a set of them agrees by construction rather than by anyone diffing option lists.

## Install

```sh
bun add -d @greendrake/dev-config
```

`typescript` (`^6`) is a peer dependency: the consumer owns its patch version, within the major these configs are written for. The package has no dependencies.

## TypeScript

Three tsconfig entries, each with `noEmit`, and an overlay for any of them: they configure type-checking (`tsc --noEmit`, `vue-tsc --noEmit`), not emit — bundling is Vite's job.

`./tsconfig/base.json` is the common ground: `target` and `module` `ESNext`, `moduleResolution: "bundler"`, `lib: ["ESNext", "DOM", "DOM.Iterable"]`, `strict`, `noUnusedLocals`, `noUnusedParameters`, `noFallthroughCasesInSwitch`, `verbatimModuleSyntax`, `isolatedModules`, `esModuleInterop`, `skipLibCheck`, `resolveJsonModule`, `noEmit`. Framework-free browser libraries extend it directly.

`./tsconfig/vue.json` extends `base.json` with `jsx: "preserve"` and `jsxImportSource: "vue"`, and sets `vueCompilerOptions` for vue-tsc: `strictTemplates: true` plus `dataAttributes: ["data-*"]`. Strict templates reject `data-*` attributes by default; they are standard HTML, and components carry them. The allowance lives in the shared config rather than in a component library because a package consuming a library's source type-checks its SFCs under the consumer's own `vueCompilerOptions`. Vue apps and Vue component libraries extend it.

`./tsconfig/node.json` extends `base.json` with `lib: ["ESNext"]` (no DOM) and `types: ["node"]`; the consumer supplies `@types/node`. Node-only packages — tooling, scripts, servers — extend it.

`./tsconfig/strict.json` is the overlay: `exactOptionalPropertyTypes`, `noUncheckedIndexedAccess`, `noImplicitOverride` and `noImplicitReturns`, and nothing else, extended after one of the three (`"extends": ["@greendrake/dev-config/tsconfig/node.json", "@greendrake/dev-config/tsconfig/strict.json"]`). A package that ships source is compiled under whatever options its consumer runs, since TypeScript checks every file in a program under one set; one that checks itself under this overlay compiles as it is for a consumer that turns these on.

### Ambient types

TypeScript 6 includes no `@types` package unless the project names one, so a package states what its own sources and tests need — `bun` for `bun:test` and the `Bun` global, `vite/client` for `import.meta.env` and the modules a side-effect asset import resolves to, `node` for `process` and the `node:` builtins. `types` in an extending config replaces the inherited list rather than adding to it, so a package needing two names both of them:

```json
{ "compilerOptions": { "types": ["vite/client", "bun"] } }
```

A package that ships source carries this further: an ambient declaration its own sources depend on must reach whatever compiles them, which a `types` entry in its private `tsconfig.json` does not. A package that imports a stylesheet for effect, say, ships the declaration beside the source and pulls it in with a path reference from the importing file.

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

`@/*` → `./src/*` is the convention: it is the type-checker side of an `@` → `src` alias configured for the bundler, so both resolve `@/…` imports to the same files. A library without such imports needs only `extends` and `include`.

## Prettier

`./prettier` is consumed through the `prettier` key in the consumer's `package.json`:

```json
"prettier": "@greendrake/dev-config/prettier"
```

Settings: `printWidth: 9999`, `tabWidth: 4`, `arrowParens: 'avoid'`, `singleQuote: true`, `trailingComma: 'none'`, `semi: false`. Width never governs line-breaking; lint rules that break literals by shape govern it instead.

One override: `*.jsx` and `*.tsx` get `printWidth: 120`. JSX is the one place the no-wrapping default fails. A Vue template is its own block, so width never governs it; a JSX element is an expression nested inside the code that returns it, and at 9999 a single component with a handful of props and a class list becomes one 500-character line. Structural breaking cannot help — it governs object literals, not elements — so JSX gets a real width and Prettier breaks it.
