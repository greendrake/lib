// Search Console: what Google has done with a site — which sitemaps it fetched
// and how much of each it indexed, what one URL's index status is, and what
// the property earned in search results.
//
// The grant is its own: a Cloud IAM role reaches nothing here. The service
// account is added as a user on the property itself (Search Console >
// Settings > Users and permissions), and the Search Console API is enabled on
// its Cloud project; missing either, every call 403s.
import { bearerRequest, type ClientOptions } from '@greendrake/util'
import type { Bearer } from './auth'

const API = 'https://searchconsole.googleapis.com'
// Search Console reports in whole days and runs a couple of days behind, so a
// default window ending today is right even though its last days read as zero.
const DEFAULT_PERFORMANCE_DAYS = 28
const DAY_MS = 24 * 60 * 60 * 1000
const DEFAULT_ROW_LIMIT = 100

export interface SearchConsoleSite {
    siteUrl: string
    permissionLevel: string
}

// Counts arrive as string-encoded int64s throughout this API.
export interface SitemapEntry {
    path: string
    lastSubmitted?: string
    lastDownloaded?: string
    isPending?: boolean
    isSitemapsIndex?: boolean
    type?: string
    warnings?: string
    errors?: string
    contents?: { type: string; submitted: string; indexed?: string }[]
}

export interface SearchAnalyticsRow {
    keys?: string[]
    clicks: number
    impressions: number
    ctr: number
    position: number
}

export interface DimensionFilter {
    dimension: string
    operator?: 'equals' | 'notEquals' | 'contains' | 'notContains' | 'includingRegex' | 'excludingRegex'
    expression: string
}

export interface SearchAnalyticsQuery {
    startDate: string
    endDate: string
    dimensions?: string[]
    // Narrows the rows to those the filters admit, e.g. one page or one query.
    dimensionFilterGroups?: { groupType?: 'and'; filters: DimensionFilter[] }[]
    // `final` (the default) counts only settled data; `all` adds the fresh
    // days still being processed.
    dataState?: 'final' | 'all'
    rowLimit?: number
    type?: string
}

export interface UrlInspectionResult {
    inspectionResult: {
        inspectionResultLink?: string
        indexStatusResult?: {
            verdict?: string
            coverageState?: string
            robotsTxtState?: string
            indexingState?: string
            lastCrawlTime?: string
            pageFetchState?: string
            googleCanonical?: string
            userCanonical?: string
            crawledAs?: string
            sitemap?: string[]
            referringUrls?: string[]
        }
        mobileUsabilityResult?: { verdict?: string }
        richResultsResult?: RichResultsResult
    }
}

// What Google detected of the page's structured data: each rich result type it
// found, the items of that type, and what is wrong with each item.
export interface RichResultsResult {
    verdict: string
    detectedItems?: {
        richResultType: string
        items: { name: string; issues?: { issueMessage: string; severity: string }[] }[]
    }[]
}

const day = (ms: number) => new Date(ms).toISOString().slice(0, 10)

// A property is addressed by the string the console shows: `sc-domain:example.com`
// for a domain property, `https://example.com/` for a URL-prefix one. It is a
// path segment, so it is escaped rather than interpolated raw — and so is a
// sitemap's own URL, its feed path.
const sitePath = (siteUrl: string) => `${API}/webmasters/v3/sites/${encodeURIComponent(siteUrl)}`
const sitemapPath = (siteUrl: string, feedpath: string) => `${sitePath(siteUrl)}/sitemaps/${encodeURIComponent(feedpath)}`

// A Search Console client over a bearer for the `webmasters` scope (or
// `webmasters.readonly`, for the reads alone).
export const createSearchConsole = (bearer: Bearer, options: ClientOptions) => {
    const call = bearerRequest(options.fetch, bearer)

    const listSites = () => call<{ siteEntry?: SearchConsoleSite[] }>(`${API}/webmasters/v3/sites`)

    // Without sitemapIndex, the sitemaps submitted directly; with it, the
    // children Google registered under that index — which is how a child's own
    // fetch state is read, since a child is never submitted itself.
    const listSitemaps = (siteUrl: string, sitemapIndex?: string) => call<{ sitemap?: SitemapEntry[] }>(`${sitePath(siteUrl)}/sitemaps${sitemapIndex ? `?${new URLSearchParams({ sitemapIndex }).toString()}` : ''}`)

    const querySearchAnalytics = (siteUrl: string, query: SearchAnalyticsQuery) => call<{ rows?: SearchAnalyticsRow[] }>(`${sitePath(siteUrl)}/searchAnalytics/query`, { method: 'POST', json: query })

    // The URL Inspection API is v1 and addresses the property in the body, not
    // the path.
    const inspectUrl = (siteUrl: string, inspectionUrl: string) =>
        call<UrlInspectionResult>(`${API}/v1/urlInspection/index:inspect`, {
            method: 'POST',
            json: {
                siteUrl,
                inspectionUrl,
                languageCode: 'en-US'
            }
        })

    return {
        listSites,
        listSitemaps,
        // Submitting a sitemap already registered re-submits it: the way to ask
        // Google to read one it has gone stale on. Both writes answer an empty
        // body.
        submitSitemap: (siteUrl: string, feedpath: string) => call<null>(sitemapPath(siteUrl, feedpath), { method: 'PUT' }),
        // Unsubmits it; the file itself is untouched, and Google may still reach
        // it from robots.txt.
        deleteSitemap: (siteUrl: string, feedpath: string) => call<null>(sitemapPath(siteUrl, feedpath), { method: 'DELETE' }),
        querySearchAnalytics,
        inspectUrl,

        // --- Readings ----------------------------------------------------------

        // The properties the account is a user on, with its permission on each —
        // the one the writes answer to. None when it is on no property.
        sites: async () => ((await listSites()).siteEntry ?? []).map(({ siteUrl, permissionLevel }) => ({ siteUrl, permissionLevel })),

        // The sitemaps of a property, or the children of one index — none when
        // nothing was ever submitted, or Google registered no child.
        sitemaps: async (siteUrl: string, sitemapIndex?: string) =>
            ((await listSitemaps(siteUrl, sitemapIndex)).sitemap ?? []).map(entry => ({
                path: entry.path,
                type: entry.type ?? null,
                isIndex: entry.isSitemapsIndex ?? false,
                isPending: entry.isPending ?? false,
                lastSubmitted: entry.lastSubmitted ?? null,
                lastDownloaded: entry.lastDownloaded ?? null,
                errors: entry.errors ?? null,
                warnings: entry.warnings ?? null,
                contents: entry.contents ?? []
            })),

        // Clicks, impressions, CTR and position over a window, by the query's
        // dimensions. The window defaults to the 28 days up to today, the
        // dimensions to `date` and the rows to 100; everything else the query
        // says is sent as it is.
        performance: async (siteUrl: string, query: Partial<SearchAnalyticsQuery>) => {
            const endDate = query.endDate ?? day(options.now())
            const startDate = query.startDate ?? day(Date.parse(endDate) - DEFAULT_PERFORMANCE_DAYS * DAY_MS)
            const sent = {
                rowLimit: DEFAULT_ROW_LIMIT,
                dimensions: ['date'],
                ...query,
                startDate,
                endDate
            }
            const { rows = [] } = await querySearchAnalytics(siteUrl, sent)
            const totals = rows.reduce((sum, row) => ({ clicks: sum.clicks + row.clicks, impressions: sum.impressions + row.impressions }), { clicks: 0, impressions: 0 })
            return {
                startDate,
                endDate,
                dimensions: sent.dimensions,
                totals,
                rows
            }
        },

        // One URL's index status as Google holds it, flattened.
        inspection: async (siteUrl: string, url: string) => {
            const { inspectionResult } = await inspectUrl(siteUrl, url)
            const status = inspectionResult.indexStatusResult
            return {
                verdict: status?.verdict ?? null,
                coverageState: status?.coverageState ?? null,
                robotsTxtState: status?.robotsTxtState ?? null,
                indexingState: status?.indexingState ?? null,
                pageFetchState: status?.pageFetchState ?? null,
                lastCrawlTime: status?.lastCrawlTime ?? null,
                crawledAs: status?.crawledAs ?? null,
                googleCanonical: status?.googleCanonical ?? null,
                userCanonical: status?.userCanonical ?? null,
                discoveredInSitemaps: status?.sitemap ?? [],
                referringUrls: status?.referringUrls ?? [],
                mobileUsability: inspectionResult.mobileUsabilityResult?.verdict ?? null,
                richResults: inspectionResult.richResultsResult ?? null,
                consoleLink: inspectionResult.inspectionResultLink ?? null
            }
        }
    }
}

export type SearchConsole = ReturnType<typeof createSearchConsole>
