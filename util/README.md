# @greendrake/util

Framework-free, Node-safe TypeScript utilities: a payload-typed event emitter, timing primitives, retry orchestration, a staleness detector, comparators, `fetch` helpers, locale, markup and browser helpers, small misc functions, and global stubs for `bun test`. Ships as TypeScript source (`src/main.ts`) with no build step and no `.d.ts`; `sideEffects: false`.

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
- `coalesced(run)` — returns `pull()`: one run at a time, and at most one more queued behind it however many times it is pulled meanwhile, for work whose every request asks the same question (read this again, rebuild that). Each pull settles with the outcome of the run that covers it — the one it started, or the one queued behind a run already going, never the run in progress, which may have begun before whatever prompted the pull. A run that throws synchronously rejects like any other; the slot is freed and handed to the queued run before the finished run's pulls settle. The outcome is the puller's to handle.

```ts
import { Debouncer, Delayed, TimeOutPromise, coalesced } from '@greendrake/util'

const refresh = coalesced(() => loadView())
source.on('changed', () => void refresh().catch(report))
const current = await refresh() // never a view read before this call

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
- `fetchJson<T>(url, init?)` — `request` over `safeFetch`: `NetworkError` on transport failure, `ApiResponseError` on non-2xx, else the parsed JSON body as `T`.
- `request<T>(fetch, url, init?)` — one round trip to an API, through the `fetch` it is handed. `init` is a `RequestInit`, or one carrying `json` in place of `body`, which is sent serialized with `Content-Type: application/json`. `fetch` is called once and outside any `try`: its rejection reaches the caller untouched, so "no answer at all" stays distinguishable from any answer. A non-2xx status throws `ApiResponseError`; a 2xx answers the body parsed as JSON, `null` when empty.
- `bearerRequest(fetch, token)` — `request` with `Authorization: Bearer <token()>` on every call; `token` may answer synchronously (a JWT minted per request) or not (an access token held until it nearly expires).
- `Fetch` — `(url: string, init: RequestInit) => Promise<Response>`: what `request` calls. The global `fetch` is one; so is a test double, which need not fake whatever a runtime adds to `typeof fetch`.
- `ClientOptions` — `{ fetch: Fetch; now: () => number }`: what an API client built on `request` is constructed with, the clock in milliseconds as `Date.now` gives them.
- `NetworkError` — `name` `'NetworkError'`; message taken from the wrapped error, which is kept as `cause`. Transport-level failures elsewhere (an abnormal WebSocket close, say) can extend it so one `instanceof` check classifies all of them.
- `HttpError` — `status`, `statusText`; message `"<status> <statusText>"`.
- `ApiResponseError` — an `HttpError` that also carries the request's `method` and `path` (with its query) and the response's whole `body`: parsed when the response says it is JSON (`application/json` or `…+json`), its text otherwise, `null` when empty. Its message is `"<method> <path> → <status>"` and the body.
- `OriginPolicy` — which browser origins a server admits: `'*'`, a list of origins as browsers send them (`'https://app.example.com'`), or a predicate. `admitsOrigin(policy, origin)` applies one.

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

```ts
import { ApiResponseError, bearerRequest, type ClientOptions } from '@greendrake/util'

const createClient = (token: () => string, { fetch }: ClientOptions) => {
    const call = bearerRequest(fetch, token)
    return {
        rename: (id: string, name: string) => call(`https://api.example/v1/things/${id}`, { method: 'PATCH', json: { name } })
    }
}

try {
    await createClient(mint, { fetch, now: Date.now }).rename('42', 'Forty-two')
} catch (e) {
    if (e instanceof ApiResponseError) {
        // e.status, e.method, e.path, e.body — whatever the API answered
    }
    // anything else rejected before an answer arrived
}
```

## Locale

- `RTL_LANGUAGES: readonly string[]` — ISO 639-1 subtags of right-to-left scripts: `ar`, `fa`, `he`, `ur`, `ps`, `sd`, `ug`, `yi`.
- `isRTLLocale(tag)` — true when the tag's language subtag (the text before the first `-`) is in `RTL_LANGUAGES`.

```ts
import { isRTLLocale } from '@greendrake/util'

document.documentElement.dir = isRTLLocale(navigator.language) ? 'rtl' : 'ltr'
```

## Markup

For text written into HTML (or XML) as a string.

- `escapeHtml(text)` — `&`, `<`, `>`, `"` and `'` as `&amp;`, `&lt;`, `&gt;`, `&quot;` and `&#39;`: the text reads back verbatim as element content and as an attribute value in either quote.
- `scriptJson(value)` — `JSON.stringify(value)` with every `<` as its JSON unicode escape, so the JSON can sit inside a `<script>` element (`application/json`, `application/ld+json`): neither `</script>` nor `<!--` can occur in it, and `JSON.parse` reads the value back unchanged.

```ts
import { escapeHtml, scriptJson } from '@greendrake/util'

const head = `<title>${escapeHtml(title)}</title>
<meta name="description" content="${escapeHtml(description)}">
<script type="application/ld+json">${scriptJson(structuredData)}</script>`
```

## DOM and geolocation

The `./browser` entry, apart from the rest because their types are the DOM lib's: a program that type-checks without it — a bun service reaching this package through a dependency — never compiles them. Importable in Node all the same; they touch `document`, `window` and `navigator` only when called.

- `getReadyStatePromise()` — resolves when `document.readyState` reaches `'complete'` (immediately if it already has); one shared promise per module instance.
- `hasFinePointer()` — `matchMedia('(hover: hover) and (pointer: fine)')`: true where the primary input is a mouse or trackpad rather than a finger. Serves as a proxy for "focusing a field will not raise an on-screen keyboard", which no platform exposes directly.
- `toggleFullscreen(el)` — requests fullscreen for `el`, or exits fullscreen if `el` is already the fullscreen element. Returns false without acting where `document.fullscreenEnabled` is false (iPhone Safari), leaving the caller to pick a substitute.
- `getUserLocation(): Promise<UserLocation>` — `navigator.geolocation.getCurrentPosition` with `timeout: LOCATION_TIMEOUT_MS` (5000) and `maximumAge: LOCATION_MAX_AGE_MS` (60000); resolves `{ latitude, longitude }`, rejects with the geolocation error, or with `Error('Location unknown')` when the position carries no coords. The two constants are exported so an app that obtains the position from a platform API instead uses the same numbers.

```ts
import { getReadyStatePromise, getUserLocation, hasFinePointer, toggleFullscreen } from '@greendrake/util/browser'

await getReadyStatePromise()
if (hasFinePointer()) input.focus()
if (!toggleFullscreen(viewer)) openOverlayInstead()
const { latitude, longitude } = await getUserLocation()
```

## Testing

The `./testing` entry, for test files run by `bun test`: it imports `bun:test`, so it loads under bun's test runner only.

- `stubGlobals(values)` — the globals `values()` returns, for the calling test file's tests (or a `describe` block's, called inside one). `bun test` runs a run's files in one process, so they share the global object; `stubGlobals` puts each name in before the first test (`beforeAll`), recording the global object's own property for it, and after the last (`afterAll`) puts that property back — or removes the name, where there was none. Nothing happens at the call: a file a `-t` filter leaves no tests to run touches no global. Each stub is a non-writable, configurable own property of the global object.
- `memoryStorage()` — `{ storage, store }`: a `Storage` backed by a `Map` (bun has neither `localStorage` nor `sessionStorage`), for code taking one as an argument or for `stubGlobals` to install, and the `Map`, for the test to inspect and clear.
- `recordingFetch(answer)` — `{ fetch, requests }`: a `Fetch` standing in for a server, for a client built on `request` to be handed. Each request is recorded in `requests`, in order — `url`, `method`, `headers` (a `Headers`) and `body` as given — and answered with whatever `answer(request)` returns; an `answer` that throws makes the call reject, as a fetch that got no answer does.

```ts
import { test } from 'bun:test'
import { stubGlobals } from '@greendrake/util/testing'

let visibility: DocumentVisibilityState = 'visible'
stubGlobals(() => ({
    document: {
        get visibilityState() {
            return visibility
        }
    }
}))

test('a hidden page', () => {
    visibility = 'hidden'
    // ...
})
```

## Misc

- `randomString(prefix?)` — `prefix` followed by a base-36 fragment of `Math.random()`.
- `randomKey()` — 128 random bits from `crypto.getRandomValues`, as 32 hex digits: an idempotency key or a nonce. Unlike `crypto.randomUUID`, available outside secure contexts.
- `dClone(data)` — deep clone via a JSON round-trip (JSON-representable data only).
- `isEmpty(o, emptyArraysAreNotEmpty?)` — true for `null`, `undefined`, `''`, `NaN`, an empty array (unless the flag is set) and an empty plain object.
- `arrayRemove(a, el)` — removes the first occurrence of `el` in place.
- `arraysEqual(a, b, same = Object.is)` — same length and `same` holds element-wise, in order.
- `recordsEqual(a, b)` — the same keys, each holding the same value (`Object.is`).
- `capitalise(s)` — upper-cases the first character.
- `humanSeconds(seconds)` — compact duration, two most significant units: `3661` → `'1h 1m'`.
- `humanFileSize(bytes, si = false, dp = 1)` — binary units (`KiB`, `MiB`, …) by default, decimal units (`kB`, `MB`, …) with `si`; `dp` decimal places.
- `fileMime(file)` — `File.type`, falling back to an extension map (`heic`, `heif`, `avif`, `webp`) when the OS reports none, then `application/octet-stream`.
- `Declared<Map, Tag, Value>` (type) — the union a host's declaration map discriminates, one member per key: the key under `Tag`, its type under `Value` (absent for a key declared `never`); `{ [Tag]: string; [Value]?: unknown }` while the map is undeclared. For a package exporting an empty interface for its host to augment.
- `configuration<T>(owner)` — a package's boot-time configuration: `{ configure, get }`, where `get` throws under `owner`'s name until the host has called `configure`.
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
