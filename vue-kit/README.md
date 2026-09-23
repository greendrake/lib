# @greendrake/vue-kit

Vue/Pinia building blocks with no API or UI dependency: the global loading state, reactive media queries, light/dark colour mode, and a factory for lazily-loaded reference collections.

## Install

```sh
bun add @greendrake/vue-kit vue pinia
```

`vue` (^3.5.34) and `pinia` (^3.0.4) are peers. TypeScript source ships as-is (no build step, no `.d.ts`), for Vite + Vue 3 apps written in TypeScript. `@greendrake/util` and `@greendrake/scss-kit` install as dependencies.

## App loading state

`useAppState()` is a Pinia store around a loading counter:

- `loadingCounter`, and the `isLoading` getter (`counter > 0`).
- `setLoading(on)` — increments or decrements the counter.
- `isFirstLoading` — `true` until the counter first empties; distinguishes the boot splash from later in-app spinners.
- `hasNavigated` — a one-way latch set once a navigation has committed, i.e. once something usable is on screen (`@greendrake/vue-app` sets it from the router's `afterEach`). Distinct from `isFirstLoading`, which can flip with the screen still bare.

```ts
import { useAppState } from '@greendrake/vue-kit'

const appState = useAppState()
appState.setLoading(true)
try {
    await loadEverything()
} finally {
    appState.setLoading(false)
}
```

`@greendrake/vue-app` holds one slot for the boot sequence and releases it when the first render settles; `@greendrake/vue-api`'s `ApiClient` holds one per pending call.

## Media queries

```ts
import { useMediaQuery, useWideScreen } from '@greendrake/vue-kit'

const reducedMotion = useMediaQuery('(prefers-reduced-motion: reduce)')
const wide = useWideScreen()
```

`useMediaQuery(query)` returns a `Ref<boolean>` bound to `matchMedia(query)` and detaches its listener on `onUnmounted`, so it is called from component setup. `useWideScreen()` is `useMediaQuery(mediaAbove('narrow'))`: `true` from 601px up, the same boundary `@greendrake/scss-kit`'s `respond-above('narrow')` mixin uses.

## Colour mode

The palette binds to the `light`/`dark` classes on `<html>` and falls back to `prefers-color-scheme` when neither is set (`@greendrake/theme`'s `light-dark` mixin). This module owns those classes, the saved preference (`localStorage` key `colorMode`) and the reactive `isDark` ref.

```ts
// main.ts, before mount — so an explicit choice never flashes the OS palette first
import { initColorMode } from '@greendrake/vue-kit'

initColorMode()
```

```vue
<script setup lang="ts">
import { isDark, toggleColorMode } from '@greendrake/vue-kit'
</script>
<template>
    <button @click="toggleColorMode">{{ isDark ? 'Light' : 'Dark' }}</button>
</template>
```

`initColorMode()` applies the saved class, sets `isDark`, and keeps it following the OS preference while no explicit choice has been made. `toggleColorMode()` flips between the two classes (from the OS default it picks the opposite of the current preference), updates `isDark` and saves the choice.

## Collection store

`defineCollectionStore(id, options)` defines a Pinia store for a shared, lazily-loaded, request-deduplicated reference collection.

```ts
import { byProp } from '@greendrake/util'
import { defineCollectionStore } from '@greendrake/vue-kit'
import { api } from '@/api'

export const useVlans = defineCollectionStore('vlans', {
    load: () => api.call('vlan.list', [{}]),
    compare: byProp('name'),
    indexBy: vlan => vlan.id
})
```

`CollectionStoreOptions<T, K>`: `load: () => Promise<T[]>`; `compare?: Comparator<T>` (`@greendrake/util`'s type), applied after every load so lists are stable across mounts; `indexBy?: (item: T) => K`, which enables the `byKey` index.

The store exposes `items` (a plain reactive array — row surgery is ordinary array mutation), `byKey` (computed `Record<K, T>`, empty without `indexBy`), `ensure()` (loads once per session, deduplicating concurrent callers through the in-flight promise) and `invalidate()` (the next `ensure()` re-fetches — call it after a mutation).

```ts
const vlans = useVlans()
await vlans.ensure()
const name = vlans.byKey[id]?.name
```
