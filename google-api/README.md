# @greendrake/google-api

Google APIs as a service account: the JWT-bearer exchange that buys an access token for any of them, and a client for the [Search Console API](https://developers.google.com/webmaster-tools/v1/api_reference_index) — sites, sitemaps, search performance over a window, a URL's index status. **Runs on [bun](https://bun.sh)**; its only runtime import besides [`@greendrake/util`](../util) is `node:crypto`.

## Install

```sh
bun add @greendrake/google-api
```

TypeScript source ships as-is (`src/main.ts` is the entry; no build step, no `.d.ts`). Extend `@greendrake/dev-config/tsconfig/node.json` with `"types": ["bun"]`, and `strict.json` over it if you like: this package checks itself under both.

## A bearer per service account

```ts
import { createSearchConsole, serviceAccountBearer } from '@greendrake/google-api'

const options = { fetch, now: Date.now }
const bearer = serviceAccountBearer(
    { clientEmail: key.client_email, privateKey: key.private_key }, // from the account's key JSON
    ['https://www.googleapis.com/auth/webmasters'],
    options
)
const searchConsole = createSearchConsole(bearer, options)
```

A key is created at **Google Cloud Console → IAM & Admin → Service Accounts → Keys**; its JSON carries `client_email` and `private_key`. The private key may be its PEM or a `KeyObject`; a PEM is parsed at construction, so a key that is not one fails there.

The bearer signs an RS256 assertion from `now()` and exchanges it at Google's token endpoint for an access token, which it holds until a minute before it expires. One exchange is in flight at a time: calls arriving during it get its token, and one that fails leaves the next call to start another. The scopes are yours — one token can carry several, and a scope the account was never granted fails at the call that needs it.

Credentials are values, never paths, and nothing is shared between two bearers or two clients: hold as many accounts in one process as you like. `fetch` is called as `(url, init)` and nothing else of it is used, so a test double is a plain function. `now` is milliseconds, as `Date.now` gives them.

Another Google API is a client away: `bearerRequest(fetch, bearer)` (re-exported from `@greendrake/util`) is the authorized round trip every call here makes.

```ts
import { bearerRequest } from '@greendrake/google-api'

const call = bearerRequest(fetch, bearer)
const project = await call<{ projectId: string }>(`https://firebase.googleapis.com/v1beta1/projects/${projectId}`)
```

## Failures

A `fetch` rejection — no answer at all — propagates out of every call, the exchange included, untouched: "could not be reached" stays distinguishable from any answer. A response whose status is not ok throws `ApiResponseError` (an `HttpError` of `@greendrake/util`, re-exported here so `instanceof` holds whichever copy of util you resolve), carrying `method`, `path` (with its query), `status` and the whole `body` Google answered: an API's `error` object, whose `message` is usually the actionable part, or the token endpoint's `error` and `error_description`.

## Search Console

| | |
|---|---|
| `listSites()`, `listSitemaps(site, sitemapIndex?)`, `submitSitemap(site, feedpath)`, `deleteSitemap(site, feedpath)`, `querySearchAnalytics(site, query)`, `inspectUrl(site, url)` | the API's calls, answering what it answers |
| `sites()` | each property the account is a user on, with its `permissionLevel`; none when it is on no property |
| `sitemaps(site, sitemapIndex?)` | each sitemap — or each child of one index — with `type`, `isIndex`, `isPending`, `lastSubmitted`, `lastDownloaded`, `errors`, `warnings` and `contents`, `null` where Google has said nothing |
| `performance(site, query)` | clicks, impressions, CTR and position over a window by the query's dimensions, with their totals |
| `inspection(site, url)` | one URL's index status, flattened: verdict, coverage, robots.txt, indexing, fetch, last crawl, canonicals, the sitemaps and pages it was found through, mobile usability, detected rich results, the console link |

A property is named as the console shows it: `sc-domain:example.com` for a domain property, `https://example.com/` for a URL-prefix one. `submitSitemap` on a sitemap already registered re-submits it, which is how Google is asked to read one it has gone stale on; `deleteSitemap` unsubmits it and leaves the file alone. With `sitemapIndex`, `listSitemaps` answers the children Google registered under that index — how a child's own fetch state is read, since a child is never submitted itself.

`performance` takes any part of a `SearchAnalyticsQuery`. The window defaults to the 28 days up to today by `now()`, counted back from `endDate` when only that is given; the dimensions default to `date` and the rows to 100. Everything else is sent as it is — `dimensionFilterGroups` to narrow the rows (one page, one query), `dataState: 'all'` to count the fresh days still being processed, `type` for image, video or news search. Search Console reports in whole days and runs a couple of days behind, so a window ending today reads zero for its last days.

## What the API does and does not do

**The grant is per property.** Neither a Cloud IAM role nor anything else on the project reaches Search Console. The service account is added as a user on each property (**Search Console → Settings → Users and permissions**), and the Search Console API is enabled on its Cloud project; missing either, every call 403s. `sites()` is the check: an account on no property lists none.

**A restricted user reads, and does no more.** Submitting a sitemap and inspecting a URL take an owner or a *full* user, and the `webmasters` scope rather than `webmasters.readonly`.

**Nothing here asks Google to crawl a page.** The Indexing API takes only `JobPosting` and `BroadcastEvent` pages; for anything else, sitemap submission and a crawlable link graph are the levers there are.
