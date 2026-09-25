# @greendrake/e2e

A Playwright kit for single-page apps: the config factories an app would otherwise hand-copy, a same-origin SPA crawler, a clipped-content detector, a polling helper for asynchronous writes, and a standalone layout checker.

## Install

```sh
bun add -d @greendrake/e2e
```

`@playwright/test` (`>=1.49.0`) is a peer dependency: the consumer pins its own Playwright, and this package resolves that copy. The package ships TypeScript source, which Node cannot load from `node_modules`, so a config importing it runs Playwright under bun: `bun --bun playwright test`. The `./poll` entry and the `check-layout` bin have no such requirement.

## `spaPlaywright()`

```js
// playwright.config.js
import { spaPlaywright } from '@greendrake/e2e'

export default spaPlaywright({ defaultPort: '5173' })
```

The frontend port is `VITE_PORT`, falling back to `defaultPort` — the variable the app's own Vite config is expected to pin its dev server to, so the config targets the server the app actually started. Omit `defaultPort` in an app whose harness always supplies `VITE_PORT`: a missing port then throws rather than silently testing whatever happens to be listening on a default.

What it returns (a `PlaywrightTestConfig` via `defineConfig`):

- `testDir: './tests'`, `fullyParallel: true`, `workers: 5`, `retries: 0`, `forbidOnly` when `CI` is set.
- `timeout: 120_000` — crawl-style specs visit every page and click through every link; Playwright's 30s default is sized for single-interaction tests.
- Reporter: `html`, `open: 'never'`.
- `use`: `baseURL` `http://localhost:<port>`, `trace: 'on-first-retry'`, `video: 'retain-on-failure'`.
- One project, `chromium`, on `devices['Desktop Chrome']`.
- No `webServer`: the app, and whatever backs it, are started by the caller.

`overrides` is a `PlaywrightTestConfig` spread over the result last, key by key at the top level — an `overrides.use` replaces the whole `use` object rather than merging into it.

## `serviceWorkerPlaywright()`

```js
// playwright.sw.config.js
import { serviceWorkerPlaywright } from '@greendrake/e2e'
import { fileURLToPath } from 'node:url'

export default serviceWorkerPlaywright({
    dist: fileURLToPath(new URL('./dist', import.meta.url)),
    defaultPort: '4173'
})
```

The config for a service-worker suite: specs in `./tests/sw`, run against a built app that `vite preview` serves statically, with no backend at all. Serving an existing `dist` rather than building one here is deliberate — a build can carry deploy-time artefacts fetched from production, which a test run must not depend on. Nothing in the suite needs an API either: the app boots, its calls park unanswered, and what is asserted is that it got far enough to say so. The worker under test is whichever one the app's own build emits.

Options:

- `dist` (required) — absolute path to the app's built output. The factory throws when it does not exist; build first.
- `defaultPort` — as for `spaPlaywright()`.
- `previewEnv` — extra environment for the preview server, for a Vite config that resolves something before it will start.
- `notifications` — run on the full Chromium build (`channel: 'chromium'`) rather than the headless shell Playwright otherwise picks for a headless run. Required by any suite that asserts on notifications: the shell ships without the notification platform and hard-denies the permission — `grantPermissions()` is accepted and `Notification.permission` still reads `"denied"`, making a worker's push handling untestable. Off by default: the shell starts faster, and the two builds are not behaviourally identical.
- `overrides` — as for `spaPlaywright()`.

What it returns:

- `testDir: './tests/sw'`, `fullyParallel: false`, `workers: 1` — every test drives the same origin's worker registration and cache storage, which are shared browser-wide state.
- `forbidOnly` when `FORBID_ONLY` is set; `timeout: 60_000`; reporter `html`, `open: 'never'`.
- `use`: `devices['Desktop Chrome']`, `baseURL` `http://localhost:<port>`, plus `channel: 'chromium'` under `notifications`.
- `webServer`: `bun --bun vite preview` at `http://localhost:<port>`, `reuseExistingServer: false`, with `VITE_PORT` and `previewEnv` in its environment.

## `crawlLocalLinks()`

```js
import { test } from '@playwright/test'
import { crawlLocalLinks } from '@greendrake/e2e'

test('every reachable page renders', async ({ page }) => {
    const visited = await crawlLocalLinks(page, 'http://localhost:5173')
    console.log(visited)
})
```

`crawlLocalLinks(page, origin)` crawls every same-origin page link reachable from `/`, verifies each page renders content, exercises SPA navigation by clicking through links, fails on any uncaught page error, and returns the visited paths. It asserts with `expect` from `@playwright/test`, so it runs inside a test.

Per page: navigate (`domcontentloaded`) and wait up to 10s for the body to hold more than 100 characters of text — `networkidle` is unreliable with keepalive and analytics requests; on timeout the pending requests are logged before the error propagates. A route may deliberately leave the site (an external-redirect stub); once off-origin, nothing on the page belongs to the app, so the crawler asserts that no page error occurred and moves on without harvesting — otherwise the foreign site's paths would be queued onto the app's origin. On-origin, the body must have text and no page error may have fired.

Link harvesting takes `a[href]` elements, keeping root-relative hrefs and absolute same-origin URLs (reduced to their pathname) and dropping `#`, `mailto:`, `tel:` and any href whose last path segment carries a file extension. Each new path joins the queue. Then every visible, enabled page link on the page is clicked — a given href is click-tested once across the whole crawl, since nav menus repeat on every page — the body after the click must have text, and the page goes back. A click-through that fails is logged as a warning, not a failure. Failed network requests (other than aborts) are warned about as they happen; console errors are collected and reported as a warning at the end. Only `pageerror` events fail the crawl.

## `findClippedContent()`

```js
import { expect, test } from '@playwright/test'
import { findClippedContent } from '@greendrake/e2e'

test('no collapsed boxes', async ({ page }) => {
    await page.goto('/')
    expect(await findClippedContent(page)).toEqual([])
})
```

`findClippedContent(page)` returns the elements whose own content is clipped away and cannot be scrolled to — the signature of a box that collapsed under its contents.

The usual cause is a grid or flex item that is also a scroll container. Such an item has no automatic minimum size (CSS Sizing 3 §3.4: the size of a scroll container's content does not constrain it), so its track or line collapses to nothing while its children keep their full height and are clipped. It is easy to write by accident — `overflow: hidden` for rounded corners on a grid card is enough — and the result is invisible content rather than an error. Engines disagree on which overflow values trigger it (`overflow: clip` is safe in Blink and not in older WebKit), so a page that looks right in one browser can collapse in another; run the check in every engine the app is used in.

A box counts as clipped when its `overflow-y` is `hidden` or `clip` (`auto` and `scroll` are deliberate scroll containers, `visible` does not clip), its `scrollHeight` exceeds its `clientHeight` by more than a 4px rounding tolerance, and it has child elements — a clamped line of text has none and is intended truncation.

Each `ClippedElement` carries `selector` (a readable path such as `div.News > a.Card`), `height` (rendered box height) and `contentHeight` (the height its content needs), `overflowY`, `parentDisplay` (the hazard is specific to grid and flex items) and `text` (the first 60 characters of the element's text).

## `@greendrake/e2e/poll`

```js
import { pollUntil } from '@greendrake/e2e/poll'

const row = await pollUntil(
    () => listRows(),
    rows => rows.find(row => row.id === expectedId)
)
```

`pollUntil(produce, predicate, budgetMs = 5000, intervalMs = 200)` calls the async `produce` and applies `predicate` to its value until the predicate returns a truthy match, and returns that match; once the budget is spent it throws `pollUntil: predicate never satisfied within <budgetMs>ms`. It exists for verifying fire-and-forget writes (tracking calls, queued tasks): the row lands asynchronously, so a single fetch races the write and a wait-and-retry is required. The default budget covers a 1.5s client-side error debounce plus network slack.

It is a separate subpath because the main entry loads `@playwright/test` through the config factories; importing that from a spec in an app that pins a different Playwright version loads a second `@playwright/test` copy — a hard collection error. `./poll` imports nothing, so a spec can use it under any pinned version.

## `check-layout`

The `check-layout` bin loads a URL in every engine, at phone and desktop widths, and reports any box whose content is clipped away unreachably — the `findClippedContent()` detection, inlined so the script runs as plain JavaScript against any URL with no TypeScript pipeline in the way.

```sh
bunx check-layout http://localhost:5173/
bunx check-layout http://localhost:5173/ --css '.Card { overflow: hidden }'
```

`--css <stylesheet>` injects the stylesheet after the page loads and before measuring — how a fix is tried against a deployed page without deploying it, and how the check is confirmed to still catch the thing it exists to catch.

Engines and viewports: `chromium` and `webkit` (an engine the resolved Playwright lacks is skipped), each at `phone` 390×844 and `desktop` 1280×800; every page load waits for `networkidle` with a 30s timeout. Per engine/viewport the script prints `ok`, or `CLIP` followed by the findings grouped by selector — the number of instances, the smallest box height, the largest content height, `overflow-y`, the parent's display and a sample of the hidden text — or `ERROR` with the first line of the failure when the page does not load.

Exit code: `0` when nothing is clipped anywhere; `1` when anything is clipped or a page fails to load, so it can gate a deploy; `2` on a usage error (no URL) or when no Playwright can be resolved.

Playwright is resolved from the caller's side, trying in order `playwright`, `playwright-core`, then a global bun install (`~/.bun/install/global/node_modules/playwright`). Install it in the workspace, or globally with `bun add -g playwright`.
