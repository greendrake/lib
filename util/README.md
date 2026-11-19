# @greendrake/util

The small helpers that TypeScript code keeps needing, whether it runs in a browser or on a server: typed events, timers that cancel, a retry loop, `fetch` calls whose failures can be told apart, and a few comparators and formatters. No framework, no dependencies.

```sh
bun add @greendrake/util
```

`npm install @greendrake/util` works equally. The package ships `src/` as it is, with no compiled output and no `.d.ts`, so the consumer's bundler or runtime has to compile TypeScript, as Vite and bun do.

## Why

Every codebase grows these helpers: an event emitter, a debounce, a retry loop around `fetch`. The copies differ in the details that matter: whether a cancelled timer rejects, whether a retry can be stopped, whether a `404` and a dropped connection look the same to the caller.

The usual places to take them from each bring a constraint along. A framework's own helpers force the framework on the users of a library built on them. Node's `EventEmitter` does not exist in a browser. `fetch` resolves for a `404` or a `500` as it does for a `200`, so code has to check for an error status at every call. This package is those helpers, written once and tested, with no framework and no dependency behind them.

## Overview

- **It runs where TypeScript runs.** Nothing here imports a framework or another package. Importing the main entry touches no browser global, so the same code serves a web page, a server and a library used by both. The helpers that need the DOM are in `./browser`, and the ones that need bun's test runner are in `./testing`.
- **Events are typed.** `Emitter` takes a map from event name to argument list. `emit` and every listener of a declared event are checked against that event's arguments.
- **Failures can be told apart.** `fetchJson` settles three ways: the parsed body, an `HttpError` for an answer that is not 2xx, and a `NetworkError` when no answer came.
- **Waiting can be called off.** A `TimeOutPromise` cancels, a `Retrier` stops, and a `Debouncer` tells a caller whether a later call has superseded it.
- **Unused helpers cost nothing.** The package is marked `sideEffects: false`, so a bundler drops what an app does not import.

## Emitter

`Emitter<E extends EventMap>` is typed by an event map: `EventMap = Record<string, unknown[]>` maps each event name to its argument tuple. It is meant to be built on: `Retrier` and `StalenessDetector` below extend it. `on` and `once` return the unsubscribe function. `off` detaches a listener, whichever of the two added it. `emit` calls the `on` listeners first, then calls the `once` listeners and removes them.

Listeners run synchronously, inside `emit`. A listener that throws stops the emit: the listeners after it do not run, and the error reaches the caller of `emit`.

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

- `TimeOutPromise(ms)` — its `promise` resolves after `ms`, and `isResolved` says whether it has. `cancel()` clears the timer and rejects the promise with `undefined`. It does nothing once the promise has resolved. `TimeOutPromise.wait(ms)` is the one-shot form.
- `Debouncer(ms)` — `fire(ms?)` waits the delay, then resolves with a `proceed` predicate. `proceed()` is true only if no later `fire()` or `cancel()` has superseded this call. An argument to `fire` overrides the constructed delay for that call only. `cancel()` invalidates every `fire()` that is still waiting.
- `Delayed` — a registry of named timeouts. `go(name, fn, ms)` schedules `fn`, replacing a pending timeout of the same name. `cancel(name)` cancels one, and `cancelAll()` cancels them all. Names belong to the instance, so equal names in unrelated instances cannot collide.
- `coalesced(run)` — returns a function, `pull()`, for work where every request asks for the same thing (read this again, rebuild that). One run goes at a time. However many times `pull()` is called meanwhile, at most one more run is queued behind it. Each call settles with the outcome of a run that started no earlier than the call: the run it started, or the run queued behind one already going. It never settles with the run in progress, which may have begun before whatever prompted the call. A run that throws synchronously rejects like any other. When a run finishes, the queued run is started before the callers of the finished run get its outcome. Handling the outcome is the caller's job.

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

`Retrier<T>` extends `Emitter<RetrierEvents>`. `run()` resolves with the first accepted result. It rethrows an error for which `shouldRetry` returns false. It rejects with `RetryStopped` once `stop()` is called, unless an attempt that is already running then succeeds. The `stopped` getter says whether `stop()` has been called. The retrier reports its status through events, and showing that status is the consumer's concern.

A `Retrier` does not give up on its own: it has no attempt limit. It retries until a result is accepted, `shouldRetry` returns false, or `stop()` is called. Without `shouldRetry`, every error is retried. `stop()` ends the wait between attempts, but it cannot abort an attempt that is already running.

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

`StalenessDetector(ttlMs)` extends `Emitter<StalenessEvents>`. It tells whether data that a server pushes is still arriving. Call `renew()` each time data arrives, and `isFresh` stays true. When no `renew()` comes within `ttlMs`, `isFresh` turns false. `fresh` and `stale` fire only when `isFresh` changes. `dispose()` clears the pending timer.

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
- `byPreset(prop, preset: string[])` — orders by position in a preset list, case-insensitive. A value missing from the preset ties with everything, so the next comparator decides.
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

- `safeFetch(...args)` — the same signature as `fetch`. A rejection (DNS failure, connection refused, TLS/CORS rejection, device offline) is rethrown as `NetworkError`. Every rejection is wrapped, so an aborted request also arrives as a `NetworkError`, with the abort's own error as its `cause`. A non-2xx status resolves normally, as with `fetch`.
- `fetchJson<T>(url, init?)` — `request` over `safeFetch`. It rejects with `NetworkError` on a transport failure, an abort included, and with `ApiResponseError` on a non-2xx status. Otherwise it resolves with the parsed JSON body as `T`.
- `request<T>(fetch, url, init?)` — one request to a JSON API and its response, made with the `fetch` passed in. `init` is a `RequestInit`. It may carry `json` in place of `body`: the value is sent serialized, with `Content-Type: application/json`. `fetch` is called once and outside any `try`, so its rejection reaches the caller unchanged: "no answer at all" stays distinguishable from any answer. A non-2xx status throws `ApiResponseError`. A 2xx resolves with the body parsed as JSON, or with `null` when the body is empty. A 2xx body that is not JSON throws the parser's `SyntaxError`. The whole response is read into memory. No timeout is set: pass a `signal` in `init` for one.
- `bearerRequest(fetch, token)` — `request` with `Authorization: Bearer <token()>` on every call. `token` may answer synchronously (a JWT created for each request) or with a promise (an access token held until it nearly expires).
- `Fetch` — `(url: string, init: RequestInit) => Promise<Response>`: what `request` calls. The global `fetch` is one. So is a test double, which does not have to fake whatever a runtime adds to `typeof fetch`.
- `ClientOptions` — `{ fetch: Fetch; now: () => number }`: what an API client built on `request` is constructed with. `now` is the clock, in milliseconds as `Date.now` gives them.
- `NetworkError` — its `name` is `'NetworkError'`. Its message is taken from the wrapped error, which is kept as `cause`. Transport-level failures elsewhere (an abnormal WebSocket close, say) can extend it, so that one `instanceof` check classifies all of them.
- `HttpError` — `status`, `statusText`; message `"<status> <statusText>"`.
- `ApiResponseError` — an `HttpError` that also carries the request's `method` and `path` (with its query), and the response's whole `body`. The body is parsed when the response says it is JSON (`application/json` or `…+json`). Otherwise it is the response's text, or `null` when empty. The message is `"<method> <path> → <status>"` and the body.
- `OriginPolicy` — which browser origins a server accepts: `'*'`, a list of origins as browsers send them (`'https://app.example.com'`), or a predicate. `admitsOrigin(policy, origin)` tells whether a policy accepts an origin.

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

- `escapeHtml(text)` — writes `&`, `<`, `>`, `"` and `'` as `&amp;`, `&lt;`, `&gt;`, `&quot;` and `&#39;`. The text then reads back verbatim as element content, and as an attribute value in either kind of quote.
- `scriptJson(value)` — `JSON.stringify(value)` with every `<` written as its JSON unicode escape, so that the JSON can sit inside a `<script>` element (`application/json`, `application/ld+json`). Neither `</script>` nor `<!--` can occur in it, and `JSON.parse` reads the value back unchanged.

```ts
import { escapeHtml, scriptJson } from '@greendrake/util'

const head = `<title>${escapeHtml(title)}</title>
<meta name="description" content="${escapeHtml(description)}">
<script type="application/ld+json">${scriptJson(structuredData)}</script>`
```

## DOM and geolocation

These helpers are the `./browser` entry. They are kept apart from the rest because their types come from TypeScript's DOM lib. A program that type-checks without that lib — a bun service that reaches this package through a dependency, say — then never compiles them. The entry can be imported in Node all the same: the helpers touch `document`, `window` and `navigator` only when called.

- `getReadyStatePromise()` — resolves when `document.readyState` reaches `'complete'`, or immediately if it already has. The module keeps one promise, and every call returns it.
- `hasFinePointer()` — `matchMedia('(hover: hover) and (pointer: fine)')`: true where the primary input is a mouse or trackpad rather than a finger. No platform reports whether focusing a field will raise an on-screen keyboard, so this check is used to guess that it will not.
- `toggleFullscreen(el)` — requests fullscreen for `el`, or exits fullscreen if `el` is already the fullscreen element. Where `document.fullscreenEnabled` is false (iPhone Safari), it returns false without acting, and the caller picks a substitute.
- `getUserLocation(): Promise<UserLocation>` — `navigator.geolocation.getCurrentPosition` with `timeout: LOCATION_TIMEOUT_MS` (5000) and `maximumAge: LOCATION_MAX_AGE_MS` (60000). It resolves with `{ latitude, longitude }`. It rejects with the geolocation error, or with `Error('Location unknown')` when the position carries no coords. The two constants are exported so that an app taking the position from a platform API instead can use the same numbers.

```ts
import { getReadyStatePromise, getUserLocation, hasFinePointer, toggleFullscreen } from '@greendrake/util/browser'

await getReadyStatePromise()
if (hasFinePointer()) input.focus()
if (!toggleFullscreen(viewer)) openOverlayInstead()
const { latitude, longitude } = await getUserLocation()
```

## Testing

The `./testing` entry is for test files run by `bun test`. It imports `bun:test`, so it loads under bun's test runner only.

- `stubGlobals(values)` — installs the globals that `values()` returns for the tests of the calling test file, or of a `describe` block when called inside one. `bun test` runs all the files of a run in one process, so they share the global object. Before the first test (`beforeAll`), `stubGlobals` records the global object's own property for each name and puts the stub in. After the last test (`afterAll`), it puts that property back, or removes the name where there was none. Nothing happens at the call itself: when a `-t` filter leaves a file no tests to run, that file touches no global. Each stub is a non-writable, configurable own property of the global object.
- `memoryStorage()` — returns `{ storage, store }`. `storage` is a `Storage` backed by a `Map` (bun has neither `localStorage` nor `sessionStorage`). Pass it to code that takes a `Storage` as an argument, or install it with `stubGlobals`. `store` is the `Map`, for the test to inspect and clear.
- `recordingFetch(answer)` — returns `{ fetch, requests }`. `fetch` is a `Fetch` that stands in for a server: hand it to a client built on `request`. Each request is recorded in `requests`, in order, as its `url`, `method`, `headers` (a `Headers`) and `body` as given. It is answered with whatever `answer(request)` returns. An `answer` that throws makes the call reject, as a fetch that got no answer does.

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
- `randomKey()` — 128 random bits from `crypto.getRandomValues`, as 32 hex digits: an idempotency key or a nonce. Unlike `crypto.randomUUID`, it is available outside secure contexts.
- `dClone(data)` — deep clone via a JSON round-trip. It keeps only what JSON can hold: a `Date` comes back as a string, a `Map` or a `Set` as an empty object, and members that are `undefined` or functions are dropped. For a faithful deep copy use `structuredClone`.
- `isEmpty(o, emptyArraysAreNotEmpty?)` — true for `null`, `undefined`, `''`, `NaN`, an empty array (unless the flag is set) and an empty plain object.
- `arrayRemove(a, el)` — removes the first occurrence of `el` in place.
- `arraysEqual(a, b, same = Object.is)` — same length and `same` holds element-wise, in order. The comparison is one level deep.
- `recordsEqual(a, b)` — the same keys, each holding the same value (`Object.is`). The comparison is one level deep. The package has no deep equality.
- `capitalise(s)` — upper-cases the first character.
- `humanSeconds(seconds)` — compact duration, two most significant units: `3661` → `'1h 1m'`. It writes `y`, `d`, `h`, `m` and `s`, and counts a year as 365 days.
- `humanCount(n, noun)` — a count in English: `humanCount(3, 'file')` → `'3 files'`, `humanCount(1, 'file')` → `'1 file'`. The plural is the noun with an `-s`.
- `humanFileSize(bytes, si = false, dp = 1)` — binary units (`KiB`, `MiB`, …) by default, decimal units (`kB`, `MB`, …) with `si`; `dp` decimal places, with a `.` for the decimal point. None of these three takes a locale. For localised output use the platform's `Intl` formatters.
- `fileMime(file)` — `File.type`. When the OS reports none, it falls back to an extension map (`heic`, `heif`, `avif`, `webp`), and then to `application/octet-stream`.
- `Declared<Map, Tag, Value>` (type) — for a package that exports an empty interface for its host to augment. The host is the program that uses the package, and `Map` is that interface. The type is a union with one member per key the host declares: the key under `Tag`, and the key's type under `Value` (absent for a key declared `never`). While the map is undeclared, the type is `{ [Tag]: string; [Value]?: unknown }`.
- `configuration<T>(owner)` — the configuration a package is given at start-up, as `{ configure, get }`. The host calls `configure`. Until it has, `get` throws an error that names `owner`.
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
