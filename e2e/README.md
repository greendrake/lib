# @greendrake/e2e

Playwright tests for single-page apps, without writing the same parts again in each app: the Playwright config, a crawl that proves every linked page renders, a check for content hidden by a collapsed box, and a wait for a write that completes asynchronously.

```sh
bun add -d @greendrake/e2e
```

## Why

A Playwright suite for a single-page app starts with a config that is nearly the same in every app. The obvious way to get one is to copy it from the last app. The copies then drift, and a fix made in one does not reach the others.

Hand-written specs also let three failures through. A route that nobody wrote a spec for throws while it renders. A collapsed box clips its content away, and nothing throws. A test reads the result of an asynchronous write before the write completes. This package is the config, written once, and a helper for each of the three.

## Trade-offs

- **One browser, and defaults taken whole.** Both configs run desktop Chromium and nothing else. `overrides` replaces a top-level key whole and does not merge into it, so adding a browser means restating `projects`. An app that wants few of these defaults is better served by a config of its own.
- **Vite and bun are assumed.** By default the port comes from `VITE_PORT`, and the service-worker config serves the build with `vite preview` under bun unless `previewCommand` says otherwise. A config that imports the package also has to run Playwright under bun. See [Setup](#setup).
- **The crawl goes only where links lead, and has no limit.** It starts at `/` and follows `a[href]` links. It cannot find a page that is reached only through a button or a form. There is no cap on the number of pages, and every distinct href counts as one, query string included. So a site whose links never run out, such as a calendar with a "next month" link, is crawled until the test times out.
- **The clipped-content check cannot tell an accident from a design.** It reports every box that has child elements, whose `overflow-y` is `hidden` or `clip`, and whose content is more than 4px taller than the box. A panel that is closed with `height: 0; overflow: hidden` is such a box, and it is reported too. There is no option to exclude an element: filter the result by `selector`. The check is vertical only, so content clipped sideways is not reported.

## Overview

Two configs:

- **`spaPlaywright()`** is the config for a single-page app's suite. By default it reads the port from `VITE_PORT`, the variable that the app's own Vite config is expected to take its dev server's port from. So the tests target the server the app started.
- **`serviceWorkerPlaywright()`** is the config for a suite that tests an app's service worker. It serves the built app statically, with no backend, and runs one test at a time.

Three helpers, each for a failure that hand-written specs let through:

- **A page nobody wrote a spec for.** A route that throws while it renders stays broken until someone opens it. A spec that lists every route works until someone adds a route and forgets the list. `crawlLocalLinks()` takes the list from the app itself. It starts at `/`, follows the app's own links, and fails when a page throws an uncaught error or renders no content.
- **Content that a collapsed box hides.** A grid or flex item with `overflow: hidden` can collapse to nothing while its children keep their height. The children are clipped away, and nothing throws. The content is still in the DOM, so a test that looks for its text still finds it. `findClippedContent()` measures the boxes of a page and returns the ones in that state. The `check-layout` command runs the same measurement against any URL, in Chromium and in WebKit.
- **A write that completes later.** A page fires a tracking call or queues a task, and does not wait for the result. A test that reads the result once races the write. A test that sleeps first is slow when the sleep is long and flaky when it is short. `pollUntil()` reads until the result is there, within a time budget.

## Setup

`@playwright/test` (`>=1.51.0`) is a peer dependency. The app that installs the package chooses its own Playwright version, and this package resolves that copy.

The package ships TypeScript source, and Node cannot load TypeScript from `node_modules`. So a config that imports the package runs Playwright under bun: `bun --bun playwright test`. The `./poll` entry and the `check-layout` bin have no such requirement.

## `spaPlaywright()`

```js
// playwright.config.js
import { spaPlaywright } from '@greendrake/e2e'

export default spaPlaywright({ defaultPort: '5173' })
```

The frontend port is `VITE_PORT`, and falls back to `defaultPort`. `VITE_PORT` is the variable that the app's own Vite config is expected to take its dev server's port from, so the config targets the server the app actually started. Omit `defaultPort` in an app where whatever starts the tests always supplies `VITE_PORT`. A missing port then throws, and the run does not silently test whatever happens to be listening on a default.

Two more options:

- `baseURL` — for an app that its own backend serves. Pass that origin here, and `VITE_PORT` is not read.
- `locale` — for an app that starts in its visitor's language. Pass the language the specs' assertions are written in. It is Playwright's `use.locale`. When it is omitted, Playwright's own default applies.

What it returns (a `PlaywrightTestConfig` via `defineConfig`):

- `testDir: './tests'`, `fullyParallel: true`, `workers: 5`, `retries: 0`, `forbidOnly` when `CI` is set.
- `timeout: 120_000`. Crawl-style specs visit every page and click through every link, and Playwright's 30s default is sized for single-interaction tests.
- Reporter: `html`, `open: 'never'`.
- `use`: `baseURL` `http://localhost:<port>` (or the `baseURL` given), `locale` as given, `trace: 'on-first-retry'`, `video: 'retain-on-failure'`.
- One project, `chromium`, on `devices['Desktop Chrome']`.
- No `webServer`. The caller starts the app and whatever backs it.

`overrides` is a `PlaywrightTestConfig`. It is spread over the result last, key by key at the top level. So an `overrides.use` replaces the whole `use` object and does not merge into it.

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

The config for a service-worker suite. Its specs are in `./tests/sw`. They run against a built app that `vite preview` serves statically, with no backend at all.

The factory serves an existing `dist` and does not build one. That is deliberate: a build can carry deploy-time artefacts fetched from production, and a test run must not depend on those. Nothing in the suite needs an API either. The app starts, its API calls go unanswered, and what the suite asserts is that the app got far enough to say so. The worker under test is whichever one the app's own build emits.

Options:

- `dist` (required) — absolute path to the app's built output. The factory throws when it does not exist; build first.
- `defaultPort` — as for `spaPlaywright()`.
- `previewCommand(port)` — the command serving `dist` on `port`, for an app whose preview is not Vite's own. Default: `bun --bun vite preview --port <port> --strictPort`. That command serves on exactly that port or fails; it does not move to a free one.
- `previewEnv` — extra environment for the preview server, for a Vite config that resolves something before it will start.
- `notifications` — run on the full Chromium build (`channel: 'chromium'`), not on the headless shell that Playwright otherwise picks for a headless run. Any suite that asserts on notifications requires it. The shell ships without the notification platform and always denies the permission: `grantPermissions()` is accepted, and `Notification.permission` still reads `"denied"`. That makes a worker's push handling untestable. The option is off by default, because the shell starts faster and the two builds are not behaviourally identical.
- `overrides` — as for `spaPlaywright()`.

What it returns:

- `testDir: './tests/sw'`, `fullyParallel: false`, `workers: 1`. Every test drives the same origin's worker registration and cache storage, and those are shared browser-wide state.
- `forbidOnly` when `CI` is set; `timeout: 60_000`; reporter `html`, `open: 'never'`.
- `use`: `devices['Desktop Chrome']`, `baseURL` `http://localhost:<port>`, plus `channel: 'chromium'` under `notifications`.
- `webServer`: `previewCommand(port)` at `http://localhost:<port>`, with `reuseExistingServer: false`. Its environment carries `VITE_PORT` and `previewEnv`. `VITE_PORT` is there for an app config that requires it whenever a server starts.

## `crawlLocalLinks()`

```js
import { test } from '@playwright/test'
import { crawlLocalLinks } from '@greendrake/e2e'

test('every reachable page renders', async ({ page }) => {
    const visited = await crawlLocalLinks(page, 'http://localhost:5173')
    console.log(visited)
})
```

`crawlLocalLinks(page, origin)` crawls every same-origin page link reachable from `/`. It verifies that each page renders content, exercises SPA navigation by clicking through links, and fails on any uncaught page error. It returns the visited paths. It asserts with `expect` from `@playwright/test`, so it runs inside a test.

For each page, the crawler navigates (`domcontentloaded`) and waits up to 10s for the body to hold more than 100 characters of text. It does not wait for `networkidle`, which is unreliable with keepalive and analytics requests. On a timeout, the pending requests are logged before the error propagates.

The crawl is a smoke test. It cannot tell whether the text is the right text, and a page that legitimately shows less text fails. So does a click whose destination shows exactly the text of the page it was clicked on.

A route may deliberately leave the site: one that only redirects to another site, say. Once the page is off-origin, nothing on it belongs to the app. So the crawler asserts that no page error occurred and moves on without collecting links. Otherwise the foreign site's paths would be queued onto the app's origin. On-origin, the body must have text and no page error may have fired.

Link collection takes the page's `a[href]` elements. Each href is resolved against the page's URL, so a relative one such as `about` or `../about` counts too. The crawler keeps the links that lead to a page of the same origin. It drops a link to another origin, a `mailto:` or `tel:` link, and a link whose last path segment carries a file extension. Each new path joins the queue, with its query string and without its fragment.

Then the crawler clicks every link on the page that leads to another of the site's pages. A given href is clicked once across the whole crawl, since nav menus repeat on every page. Each click starts from the page freshly loaded, so each click costs a page load. The click must change the URL. Then, within 10s, the body must show more than 100 characters of text other than the text of the page the click left. No page error may have fired. A click that falls short of any of these fails the crawl.

Only a visible link that opens in the same tab is clicked. A page may show no such copy of a link, as when the link sits in a collapsed menu. Then the link is clicked on another page that shows it, if there is one.

Failed network requests, other than aborts, are warned about as they happen. Console errors are collected and reported as a warning at the end. Of these, only `pageerror` events fail the crawl. A page that does not show its content within 10 seconds fails it too.

## `findClippedContent()`

```js
import { expect, test } from '@playwright/test'
import { findClippedContent } from '@greendrake/e2e'

test('no collapsed boxes', async ({ page }) => {
    await page.goto('/')
    expect(await findClippedContent(page)).toEqual([])
})
```

`findClippedContent(page)` returns the elements whose own content is clipped away and cannot be scrolled to. That is the sign of a box that collapsed under its contents.

The usual cause is a grid or flex item that is also a scroll container. Such an item has no automatic minimum size (CSS Sizing 3 §3.4: the size of a scroll container's content does not constrain it). So its track or line collapses to nothing, while its children keep their full height and are clipped. It is easy to write by accident: `overflow: hidden` for rounded corners on a grid card is enough. The result is invisible content, not an error.

Engines disagree on which overflow values trigger it: `overflow: clip` is safe in Blink and not in older WebKit. So a page that looks right in one browser can collapse in another. Run the check in every engine the app is used in.

A box counts as clipped when all of these hold:

- Its `overflow-y` is `hidden` or `clip`. `auto` and `scroll` are deliberate scroll containers, and `visible` does not clip.
- Its `scrollHeight` exceeds its `clientHeight` by more than a 4px rounding tolerance.
- It has child elements. A clamped line of text has none, and is intended truncation.

Each `ClippedElement` carries:

- `selector` — a readable path such as `div.News > a.Card`.
- `height` — the rendered box height.
- `contentHeight` — the height its content needs.
- `overflowY`.
- `parentDisplay` — the parent's `display`. The problem is specific to grid and flex items.
- `text` — the first 60 characters of the element's text.

## `@greendrake/e2e/poll`

```js
import { pollUntil } from '@greendrake/e2e/poll'

const row = await pollUntil(
    () => listRows(),
    rows => rows.find(row => row.id === expectedId)
)
```

`pollUntil(produce, predicate, budgetMs = 5000, intervalMs = 200)` calls the async `produce` and applies `predicate` to its value. It repeats until the predicate returns a truthy match, and returns that match. Once the budget runs out it throws `pollUntil: predicate never satisfied within <budgetMs>ms`.

It exists for verifying fire-and-forget writes, such as tracking calls and queued tasks. The row is written asynchronously, so a single fetch races the write, and a wait-and-retry is required. The default budget covers a 1.5s client-side error debounce plus an allowance for the network.

It is a separate subpath because the main entry loads `@playwright/test` through the config factories. When a spec imports the main entry in an app that uses a different Playwright version, a second copy of `@playwright/test` is loaded, and test collection fails with an error. `./poll` imports nothing, so a spec can use it under any Playwright version.

## `check-layout`

The `check-layout` bin loads a URL in every engine, at phone and desktop widths, and reports any box whose content is clipped away unreachably. The detection is that of `findClippedContent()`. It is inlined, so the script runs as plain JavaScript against any URL, and needs no TypeScript toolchain.

```sh
bunx check-layout http://localhost:5173/
bunx check-layout http://localhost:5173/ --css '.Card { overflow: hidden }'
```

`--css <stylesheet>` injects the stylesheet after the page loads and before measuring. That is how a fix is tried against a deployed page without deploying it. It is also how the check is confirmed to still catch the thing it exists to catch.

The engines are `chromium` and `webkit`; an engine the resolved Playwright lacks is skipped. Each runs at two viewports: `phone` 390×844 and `desktop` 1280×800. Every page load waits for `networkidle`, with a 30s timeout.

The script sees one page in one state. It does not log in or click, and it does not run Firefox. For a page behind a login, or a state that takes interaction to reach, call `findClippedContent()` from a test.

For each engine and viewport the script prints one of:

- `ok`.
- `CLIP`, followed by the findings grouped by selector: the number of instances, the smallest box height, the largest content height, `overflow-y`, the parent's display and a sample of the hidden text.
- `ERROR`, with the first line of the failure, when the page does not load.

The exit code is `0` when nothing is clipped anywhere. It is `1` when anything is clipped or a page fails to load, so the script can block a deploy. It is `2` on a usage error (no URL) or when no Playwright can be resolved.

Playwright is resolved from the caller's side. The script tries `playwright`, then `playwright-core`, then a global bun install (`~/.bun/install/global/node_modules/playwright`). Install it in the workspace, or globally with `bun add -g playwright`.
