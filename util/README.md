# @greendrake/util

Framework-free, Node-safe TypeScript utilities: a payload-typed event emitter, timing primitives, retry orchestration, a staleness detector, comparators, `fetch` helpers, locale and browser helpers, and small misc functions. Ships as TypeScript source (`src/main.ts`) with no build step and no `.d.ts`; `sideEffects: false`.

## Install

```sh
bun add @greendrake/util
```

`npm install @greendrake/util` works equally.

## Emitter

`Emitter<E extends EventMap>`, where `EventMap = Record<string, unknown[]>` maps each event name to its argument tuple. Meant for composition (`Retrier` and `StalenessDetector` below extend it). `on`/`once` return the unsubscribe function; `off` detaches a listener from both kinds; `emit` calls the `on` listeners, then drains the `once` set.

```ts
import { Emitter } from '@greendrake/util'

interface Events {
    hit: [count: number]
    flat: []
    [key: string]: unknown[]
}

const e = new Emitter<Events>()
const off = e.on('hit', n => console.log(n))
e.once('flat', () => {})
e.emit('hit', 1)
off()
```

## Timing

- `TimeOutPromise(ms)` — `promise` resolves after `ms`; `isResolved`; `cancel()` clears the timer and rejects the promise (with `undefined`) while it is pending. `TimeOutPromise.wait(ms)` is the one-shot form.
- `Debouncer(ms)` — `fire(ms?)` waits the delay (the argument overrides the constructed default for that call only) and resolves with a `proceed` predicate that is true only if no later `fire()` or `cancel()` superseded this one. `cancel()` invalidates every in-flight `fire()`.
- `Delayed` — named-timeout registry: `go(name, fn, ms)` schedules `fn`, replacing a pending timeout of the same name; `cancel(name)`; `cancelAll()`. Instance-scoped, so equal names in unrelated instances cannot collide.

```ts
import { Debouncer, Delayed, TimeOutPromise } from '@greendrake/util'

const debouncer = new Debouncer(300)
const proceed = await debouncer.fire()
if (proceed()) {
    // commit
}

const delayed = new Delayed()
delayed.go('save', () => save(), 1000)
delayed.cancel('save')

await TimeOutPromise.wait(50)
```

## Retry

`Retrier<T>` (extends `Emitter<RetrierEvents>`): `run()` resolves with the first accepted result, rethrows an error its `shouldRetry` gate declares fatal, and rejects with `RetryStopped` once `stop()` is called (`stopped` getter). Status is observable via events; rendering it is the consumer's concern.

`RetrierOptions<T>`:

- `attempt: () => Promise<T>`
- `accept?: (result: T) => boolean` — a resolved attempt whose result is not accepted counts as a failure and schedules a retry.
- `shouldRetry?: (error: unknown) => boolean` — return false to make the error fatal.
- `delaySeconds?: number | ((retryIndex: number) => number)` — seconds between attempts, a constant or a function of the 0-based retry index; default 4.

`RetrierEvents`: `trying: []` before each attempt; `waiting: [secondsLeft: number]` once per second while waiting, counting down.

```ts
import { fetchJson, NetworkError, Retrier, RetryStopped } from '@greendrake/util'

const retrier = new Retrier({
    attempt: () => fetchJson<Status>('/status'),
    accept: status => status.ready,
    shouldRetry: e => e instanceof NetworkError,
    delaySeconds: i => Math.min(30, 2 ** i)
})
retrier.on('waiting', s => console.log(`retry in ${s}s`))
try {
    const status = await retrier.run()
} catch (e) {
    if (e instanceof RetryStopped) {
        // stop() was called
    }
}
```

## Staleness

`StalenessDetector(ttlMs)` (extends `Emitter<StalenessEvents>`): a renewable freshness flag for push data streams. `renew()` on every inbound datum keeps `isFresh` true; when no renewal arrives within `ttlMs` the flag drops. `fresh` and `stale` fire only on transitions. `dispose()` clears the pending timer.

```ts
import { StalenessDetector } from '@greendrake/util'

const staleness = new StalenessDetector(10_000)
staleness.on('stale', () => showOfflineBadge())
staleness.on('fresh', () => hideOfflineBadge())
socket.onmessage = () => staleness.renew()
```

## Comparators

`Comparator<T> = (a: T, b: T) => number`, for `Array.prototype.sort`.

- `byProp(prop, descending?)` — orders by a property using `===`/`>`.
- `byPreset(prop, preset: string[])` — orders by position in a preset list, case-insensitive; values missing from the preset tie with everything and so defer to the next comparator.
- `composeComparators(...comparators)` — tie-breaking composition: later comparators apply only where earlier ones tie.

```ts
import { byPreset, byProp, composeComparators } from '@greendrake/util'

interface Item {
    kind: string
    n: number
}

items.sort(composeComparators<Item>(byPreset('kind', ['grid', 'solar']), byProp('n', true)))
```

## Fetch

- `safeFetch(...args)` — same signature as `fetch`; a rejection (DNS failure, connection refused, TLS/CORS rejection, device offline) is rethrown as `NetworkError`. A non-2xx status resolves normally, as with `fetch`.
- `fetchJson<T>(url, init?)` — `safeFetch`, then `HttpError` on non-2xx, else the parsed JSON body as `T`.
- `NetworkError` — `name` `'NetworkError'`; message taken from the wrapped error, which is kept as `cause`. Transport-level failures elsewhere (an abnormal WebSocket close, say) can extend it so one `instanceof` check classifies all of them.
- `HttpError` — `status`, `statusText`; message `"<status> <statusText>"`.

```ts
import { fetchJson, HttpError, NetworkError } from '@greendrake/util'

try {
    const user = await fetchJson<User>('/api/me')
} catch (e) {
    if (e instanceof NetworkError) {
        // transport failure, e.cause holds the browser error
    }
    if (e instanceof HttpError && e.status === 401) {
        // sign in
    }
}
```

## Locale

- `RTL_LANGUAGES: readonly string[]` — ISO 639-1 subtags of right-to-left scripts: `ar`, `fa`, `he`, `ur`, `ps`, `sd`, `ug`, `yi`.
- `isRTLLocale(tag)` — true when the tag's language subtag (the text before the first `-`) is in `RTL_LANGUAGES`.

```ts
import { isRTLLocale } from '@greendrake/util'

document.documentElement.dir = isRTLLocale(navigator.language) ? 'rtl' : 'ltr'
```

## DOM and geolocation

Importable in Node; these touch `document`, `window` and `navigator` only when called.

- `getReadyStatePromise()` — resolves when `document.readyState` reaches `'complete'` (immediately if it already has); one shared promise per module instance.
- `hasFinePointer()` — `matchMedia('(hover: hover) and (pointer: fine)')`: true where the primary input is a mouse or trackpad rather than a finger. Serves as a proxy for "focusing a field will not raise an on-screen keyboard", which no platform exposes directly.
- `toggleFullscreen(el)` — requests fullscreen for `el`, or exits fullscreen if `el` is already the fullscreen element. Returns false without acting where `document.fullscreenEnabled` is false (iPhone Safari), leaving the caller to pick a substitute.
- `getUserLocation(): Promise<UserLocation>` — `navigator.geolocation.getCurrentPosition` with `timeout: LOCATION_TIMEOUT_MS` (5000) and `maximumAge: LOCATION_MAX_AGE_MS` (60000); resolves `{ latitude, longitude }`, rejects with the geolocation error, or with `Error('Location unknown')` when the position carries no coords. The two constants are exported so an app that obtains the position from a platform API instead uses the same numbers.

```ts
import { getReadyStatePromise, getUserLocation, hasFinePointer, toggleFullscreen } from '@greendrake/util'

await getReadyStatePromise()
if (hasFinePointer()) input.focus()
if (!toggleFullscreen(viewer)) openOverlayInstead()
const { latitude, longitude } = await getUserLocation()
```

## Misc

- `randomString(prefix?)` — `prefix` followed by a base-36 fragment of `Math.random()`.
- `dClone(data)` — deep clone via a JSON round-trip (JSON-representable data only).
- `isEmpty(o, emptyArraysAreNotEmpty?)` — true for `null`, `undefined`, `''`, `NaN`, an empty array (unless the flag is set) and an empty plain object.
- `arrayRemove(a, el)` — removes the first occurrence of `el` in place.
- `capitalise(s)` — upper-cases the first character.
- `humanSeconds(seconds)` — compact duration, two most significant units: `3661` → `'1h 1m'`.
- `humanFileSize(bytes, si = false, dp = 1)` — binary units (`KiB`, `MiB`, …) by default, decimal units (`kB`, `MB`, …) with `si`; `dp` decimal places.
- `fileMime(file)` — `File.type`, falling back to an extension map (`heic`, `heif`, `avif`, `webp`) when the OS reports none, then `application/octet-stream`.
- `createDeferred<T = void>()` — a `Deferred<T>`: `{ promise, resolve, reject }`.

```ts
import { createDeferred, humanFileSize, humanSeconds, isEmpty } from '@greendrake/util'

humanSeconds(3661) // '1h 1m'
humanFileSize(1536) // '1.5 KiB'
humanFileSize(1536, true) // '1.5 kB'
isEmpty({}) // true
isEmpty([], true) // false

const ready = createDeferred<string>()
ready.resolve('ok')
await ready.promise
```
