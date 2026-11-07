# @greendrake/app-store-connect

A client for the [App Store Connect API](https://developer.apple.com/documentation/appstoreconnectapi): the ES256 JWT, every page of a collection, App IDs and capabilities, devices, certificates and provisioning profiles, builds and TestFlight, App Store versions and both levels of their localizations, review submissions; two readings composed from them: an app's record, and its App Store pages; and the authorised round trips they are written over, for any endpoint answering JSON the client does not cover. **Runs on [bun](https://bun.sh)**; its only runtime import besides [`@greendrake/util`](../util) is `node:crypto`.

## Install

```sh
bun add @greendrake/app-store-connect
```

TypeScript source ships as-is (`src/main.ts` is the entry; no build step, no `.d.ts`). Extend `@greendrake/dev-config/tsconfig/node.json` with `"types": ["bun"]`, and `strict.json` over it if you like: this package checks itself under both.

## A client per key

```ts
import { createAppStoreConnect } from '@greendrake/app-store-connect'

const appStore = createAppStoreConnect(
    { issuerId, keyId, privateKey: p8 }, // the .p8 as PEM text, or a KeyObject
    { fetch, now: Date.now }
)

const app = await appStore.findApp('com.example.app')
```

The key is created at **App Store Connect → Users and Access → Integrations → App Store Connect API**; the issuer id is on the same page. Profile creation needs the **Admin** (or Account Holder) role. A PEM is parsed at construction, so a key that is not one fails there.

Credentials are values, never paths, and nothing is shared between two clients: hold as many keys in one process as you like. Each request carries a JWT minted from `now()` and good for five minutes — there is no session and nothing to refresh.

`fetch` is called as `(url, init)` and nothing else of it is used, so a test double is a plain function. `now` is milliseconds, as `Date.now` gives them.

## Failures

A `fetch` rejection — no answer at all — propagates out of every call untouched: "could not be reached" stays distinguishable from any answer. A response whose status is not ok throws `ApiResponseError` (an `HttpError` of `@greendrake/util`, re-exported here so `instanceof` holds whichever copy of util you resolve), carrying `method`, `path` (with its query), `status` and the whole `body` Apple answered — its `errors` array, whose `detail` is usually the actionable part.

```ts
import { ApiResponseError } from '@greendrake/app-store-connect'

try {
    await appStore.createBundleId('com.example.app', 'Example', 'UNIVERSAL')
} catch (e) {
    if (e instanceof ApiResponseError && e.status === 409) {
        // e.body.errors
    }
}
```

`reviewDetail` answers `null` for a version with none yet — Apple says so with a 404 or a null `data`, and both mean the same — and `findApp` `null` for a bundle id with no app record.

## Calls

Every collection read follows Apple's cursor to the last page, 200 at a time. Collections answer their resources (`{ type, id, attributes }`); `listBundleIds` answers the whole page with its `included` capabilities (`toManyIds` reads a bundle id's capability ids off its relationship).

| | |
|---|---|
| `findApp(bundleId)` | the app record, or `null` |
| `listBundleIds()`, `createBundleId(identifier, name, platform)`, `deleteBundleId(key)`, `enableCapability(bundleIdKey, type)` | App IDs and capabilities |
| `listDevices()`, `registerDevice(name, udid, platform)` | devices |
| `listCertificates()`, `createCertificate(csr, type)`, `revokeCertificate(key)` | signing certificates; Apple issues against a CSR, so the private key stays where it was generated |
| `listProfiles()`, `createProfile(name, type, bundleIdKey, certificateIds, deviceIds)`, `deleteProfile(key)` | provisioning profiles, with their content |
| `listBuilds(appKey)` | newest first, each with the marketing version (`train`) it was uploaded under |
| `buildBetaState(buildKey)`, `listBetaGroups(appKey)`, `listBetaTesters(groupKey)` | TestFlight |
| `buildFileSizes(buildKey)` | download and install size of the build's app bundle per device model and OS version |
| `listAppStoreVersions(appKey, platform, versionString?)` | every App Store version on a platform, each with its attached `build` or `null` |
| `createAppStoreVersion(appKey, platform, versionString, releaseType)`, `updateReleaseType(versionKey, releaseType)`, `attachBuild(versionKey, buildKey)` | a version and its build |
| `listAppInfos(appKey)` | the app information records |
| `listLocalizations(level, parentKey)`, `createLocalization(level, parentKey, attributes)`, `updateLocalization(level, key, attributes)`, `deleteLocalization(level, key)` | both localization levels |
| `reviewDetail(versionKey)` | App Review contact, demo account and notes, or `null` |
| `listReviewSubmissions(appKey, platform, state)`, `createReviewSubmission(appKey, platform)`, `listReviewSubmissionItems(submissionKey)`, `addReviewSubmissionItem(submissionKey, versionKey)`, `submitReview(submissionKey)` | review submissions; each item names the version it carries (`appStoreVersionId`) |

`platform` is a `Platform`: `IOS`, `MAC_OS`, `TV_OS` or `VISION_OS`. States are read in the API's current vocabulary — a version's `appVersionState` (`AppVersionState`), an app information record's `state` (`AppInfoState`) — not the deprecated `appStoreState`.

## Readings

| | |
|---|---|
| `appRecord(bundleId, recentBuilds)` | `null` for no record; the record (`id`, `name`, `sku`, `primaryLocale`); each app information record's `id`, `state` and, per locale, its localization's `id`, `name` and `subtitle`; the newest `recentBuilds` builds with `version`, `uploadedDate`, `processingState`, `expired` and `train` |
| `storePages(bundleId, platform)` | `null` for no record; the record's `id` and `primaryLocale`, and its App Store pages |

A page is a version, the app information that goes with it, and both levels' localizations joined by locale — `null` at the level that lacks one:

```ts
{
    version: { id, versionString, appVersionState, releaseType, createdDate },
    appInfo: { id, state },
    locales: { 'en-US': { appInfo: { id, name, subtitle } | null, version: { id, description, keywords, promotionalText, whatsNew } | null }, … }
}
```

The pages are the version on sale (`READY_FOR_DISTRIBUTION`), if any, and the newest version when that is another one, in whatever state: being prepared, rejected, in review, or approved and held for a manual release. Each pairs with the app information in its own case — the one on sale, or the one in flight; anything else is an error naming the states found. The ids are what the writes take. Promotional text, the one field of a page that changes while it is on sale:

```ts
const english = page.locales['en-US']?.version
if (english) await appStore.updateLocalization('appStoreVersionLocalizations', english.id, { promotionalText: 'Now with more' })
```

## Calling what the client does not cover

The calls above are written over three that take a path under `https://api.appstoreconnect.apple.com` — never a URL: the host is the client's. They are on the client too, so an endpoint answering JSON that it lacks is one call away, over the same key, with the same failures:

| | |
|---|---|
| `request<T>(path, init?)` | one round trip answering JSON: a `json` in `init` is sent as JSON, an ok answer parsed (`null` when empty) |
| `list<A, I>(path, params?)` | every page of a collection, 200 at a time unless `params` names a `limit`, with the `included` of every page; the query is `params`, and a path carrying one is refused |
| `getOrAbsent<A>(path)` | a to-one resource that may not exist: `null` on a 404 and on a null `data` |

The relationship readers are the ones the client's own calls join with: `toOneId` and `toManyIds` read the ids a resource's relationship points at, and `includedBy(page, type)` looks up, by a resource's to-one relationship, the companion of that type a `list` with `include` gathered.

An app's analytics reports, which the client has no call for. A report request goes on generating reports until it is left unread for long enough to stop (`stoppedDueToInactivity`), and only a new one resumes them — so one still running is read rather than another made:

```ts
import type { Single } from '@greendrake/app-store-connect'

interface ReportRequest {
    accessType: 'ONGOING' | 'ONE_TIME_SNAPSHOT'
    stoppedDueToInactivity: boolean
}

const { data: ongoing } = await appStore.list<ReportRequest>(`/v1/apps/${appKey}/analyticsReportRequests`, { 'filter[accessType]': 'ONGOING' })
const requested =
    ongoing.find(request => !request.attributes.stoppedDueToInactivity) ??
    (
        await appStore.request<Single<ReportRequest>>('/v1/analyticsReportRequests', {
            method: 'POST',
            json: { data: { type: 'analyticsReportRequests', attributes: { accessType: 'ONGOING' }, relationships: { app: { data: { type: 'apps', id: appKey } } } } }
        })
    ).data
const { data: reports } = await appStore.list<{ name: string; category: string }>(`/v1/analyticsReportRequests/${requested.id}/reports`, { 'filter[category]': 'APP_USAGE' })
for (const report of reports) {
    const { data: instances } = await appStore.list<{ granularity: string; processingDate: string }>(`/v1/analyticsReports/${report.id}/instances`, { 'filter[granularity]': 'DAILY' })
    for (const instance of instances) {
        const { data: segments } = await appStore.list<{ url: string; checksum: string; sizeInBytes: number }>(`/v1/analyticsReportInstances/${instance.id}/segments`)
        // Each segment's `url` is outside the API's host: fetch it yourself.
    }
}
```

## What the API does and does not do

**App records are read-only.** `/v1/apps` has a GET and no POST: a record is created in App Store Connect (Apps → +), and uploads are refused until it exists.

**A page is localized at two levels.** The app information carries the name, subtitle and privacy URLs; a version carries the description, keywords, promotional text, What's New and support and marketing URLs. Both are one kind of resource twice over — listed under their parent, created related to it, patched and deleted by their own id — and `level` (`appInfoLocalizations` or `appStoreVersionLocalizations`) names which.

**The app information on sale refuses writes.** A live app has one record per state: the one on sale (`READY_FOR_DISTRIBUTION`), and, while a version is in flight, the one going with it. Only that one takes writes, while it is `PREPARE_FOR_SUBMISSION`, `DEVELOPER_REJECTED` or `REJECTED`.

**One open review submission per app and platform.** A second is refused; an open one is reused, and may already hold items of other kinds (an in-app purchase, an app event).

**Profiles are fixed at creation.** There is no PATCH: a device or certificate is added by deleting the profile and creating it again. A distribution profile takes no devices, and a `devices` relationship on one is refused rather than ignored.
