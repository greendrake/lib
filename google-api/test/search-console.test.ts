import { describe, expect, test } from 'bun:test'
import { ApiResponseError, createSearchConsole, type SearchConsole } from '../src/main'
import { recordingFetch } from '@greendrake/util/testing'
import { NOW } from './clock'

const DOMAIN = 'sc-domain:example.com'
const PREFIX = 'https://example.com/'
const SITEMAP = 'https://example.com/sitemap.xml'

const over = (answer: () => Response) => {
    const { fetch, requests: sent } = recordingFetch(answer)
    return { client: createSearchConsole(() => Promise.resolve('ya29.token'), { fetch, now: () => NOW }), sent }
}

// Each call's request: method, path with its query, and JSON body.
const REQUESTS: [string, (client: SearchConsole) => Promise<unknown>, string, string, unknown][] = [
    ['listSites', c => c.listSites(), 'GET', '/webmasters/v3/sites', null],
    ['listSitemaps', c => c.listSitemaps(DOMAIN), 'GET', '/webmasters/v3/sites/sc-domain%3Aexample.com/sitemaps', null],
    ['listSitemaps of an index', c => c.listSitemaps(DOMAIN, SITEMAP), 'GET', '/webmasters/v3/sites/sc-domain%3Aexample.com/sitemaps?sitemapIndex=https%3A%2F%2Fexample.com%2Fsitemap.xml', null],
    ['submitSitemap', c => c.submitSitemap(PREFIX, SITEMAP), 'PUT', '/webmasters/v3/sites/https%3A%2F%2Fexample.com%2F/sitemaps/https%3A%2F%2Fexample.com%2Fsitemap.xml', null],
    ['deleteSitemap', c => c.deleteSitemap(PREFIX, SITEMAP), 'DELETE', '/webmasters/v3/sites/https%3A%2F%2Fexample.com%2F/sitemaps/https%3A%2F%2Fexample.com%2Fsitemap.xml', null],
    [
        'querySearchAnalytics',
        c =>
            c.querySearchAnalytics(DOMAIN, {
                startDate: '2026-01-01',
                endDate: '2026-01-31',
                dimensions: ['page']
            }),
        'POST',
        '/webmasters/v3/sites/sc-domain%3Aexample.com/searchAnalytics/query',
        {
            startDate: '2026-01-01',
            endDate: '2026-01-31',
            dimensions: ['page']
        }
    ],
    [
        'inspectUrl',
        c => c.inspectUrl(DOMAIN, 'https://example.com/a'),
        'POST',
        '/v1/urlInspection/index:inspect',
        {
            siteUrl: DOMAIN,
            inspectionUrl: 'https://example.com/a',
            languageCode: 'en-US'
        }
    ]
]

describe('requests', () => {
    test.each(REQUESTS)('%s', async (_name, call, method, path, body) => {
        const { client, sent } = over(() => Response.json({}))
        await call(client)
        expect(sent).toHaveLength(1)
        const [request] = sent
        const url = new URL(request?.url ?? '')
        expect(request?.method).toBe(method)
        expect(url.origin).toBe('https://searchconsole.googleapis.com')
        expect(`${url.pathname}${url.search}`).toBe(path)
        expect(typeof request?.body === 'string' ? JSON.parse(request.body) : null).toEqual(body)
        expect(request?.headers.get('Authorization')).toBe('Bearer ya29.token')
    })

    test("a status throws ApiResponseError carrying Google's whole answer", async () => {
        const answer = {
            error: {
                code: 403,
                message: 'User does not have sufficient permission for site',
                status: 'PERMISSION_DENIED'
            }
        }
        const { client } = over(() => Response.json(answer, { status: 403 }))
        const error = await client.listSitemaps(DOMAIN).catch((e: unknown) => e)
        expect(error).toBeInstanceOf(ApiResponseError)
        expect(error).toMatchObject({
            method: 'GET',
            path: '/webmasters/v3/sites/sc-domain%3Aexample.com/sitemaps',
            status: 403,
            body: answer
        })
    })
})

describe('sites', () => {
    test('each property with the permission the account holds on it', async () => {
        const { client } = over(() => Response.json({ siteEntry: [{ siteUrl: DOMAIN, permissionLevel: 'siteFullUser' }] }))
        expect(await client.sites()).toEqual([{ siteUrl: DOMAIN, permissionLevel: 'siteFullUser' }])
    })

    test('none for an account on no property, which Google answers with no list at all', async () => {
        expect(await over(() => Response.json({})).client.sites()).toEqual([])
    })
})

describe('sitemaps', () => {
    test('each sitemap, what Google left unsaid made explicit', async () => {
        const fetched = {
            path: SITEMAP,
            type: 'sitemap',
            isSitemapsIndex: true,
            isPending: false,
            lastSubmitted: '2026-11-01T00:00:00Z',
            lastDownloaded: '2026-11-02T00:00:00Z',
            errors: '0',
            warnings: '1',
            contents: [
                {
                    type: 'web',
                    submitted: '120',
                    indexed: '0'
                }
            ]
        }
        const { client } = over(() => Response.json({ sitemap: [fetched, { path: 'https://example.com/never.xml' }] }))
        expect(await client.sitemaps(DOMAIN)).toEqual([
            {
                path: SITEMAP,
                type: 'sitemap',
                isIndex: true,
                isPending: false,
                lastSubmitted: '2026-11-01T00:00:00Z',
                lastDownloaded: '2026-11-02T00:00:00Z',
                errors: '0',
                warnings: '1',
                contents: [
                    {
                        type: 'web',
                        submitted: '120',
                        indexed: '0'
                    }
                ]
            },
            {
                path: 'https://example.com/never.xml',
                type: null,
                isIndex: false,
                isPending: false,
                lastSubmitted: null,
                lastDownloaded: null,
                errors: null,
                warnings: null,
                contents: []
            }
        ])
    })

    test('none when nothing was submitted', async () => {
        expect(await over(() => Response.json({})).client.sitemaps(DOMAIN, SITEMAP)).toEqual([])
    })
})

describe('performance', () => {
    const ROWS = [
        {
            keys: ['2026-11-01'],
            clicks: 3,
            impressions: 40,
            ctr: 0.075,
            position: 8.2
        },
        {
            keys: ['2026-11-02'],
            clicks: 1,
            impressions: 25,
            ctr: 0.04,
            position: 9.1
        }
    ]

    test('defaults the window to the 28 days up to today by the clock, the dimension to date and the rows to 100', async () => {
        const { client, sent } = over(() => Response.json({ rows: ROWS }))
        expect(await client.performance(DOMAIN, {})).toEqual({
            startDate: '2026-10-09',
            endDate: '2026-11-06',
            dimensions: ['date'],
            totals: { clicks: 4, impressions: 65 },
            rows: ROWS
        })
        expect(JSON.parse(String(sent[0]?.body))).toEqual({
            rowLimit: 100,
            dimensions: ['date'],
            startDate: '2026-10-09',
            endDate: '2026-11-06'
        })
    })

    test('counts the default window back from an end date given', async () => {
        const { client, sent } = over(() => Response.json({}))
        expect(await client.performance(DOMAIN, { endDate: '2026-03-01' })).toMatchObject({
            startDate: '2026-02-01',
            endDate: '2026-03-01',
            totals: { clicks: 0, impressions: 0 },
            rows: []
        })
        expect(JSON.parse(String(sent[0]?.body))).toMatchObject({ startDate: '2026-02-01', endDate: '2026-03-01' })
    })

    test('sends everything else the query says as it is', async () => {
        const query = {
            startDate: '2026-01-01',
            endDate: '2026-01-31',
            dimensions: ['query', 'page'],
            dimensionFilterGroups: [
                {
                    groupType: 'and' as const,
                    filters: [
                        {
                            dimension: 'page',
                            operator: 'equals' as const,
                            expression: 'https://example.com/a'
                        }
                    ]
                }
            ],
            dataState: 'all' as const,
            rowLimit: 10,
            type: 'image'
        }
        const { client, sent } = over(() => Response.json({}))
        expect(await client.performance(DOMAIN, query)).toMatchObject({
            startDate: '2026-01-01',
            endDate: '2026-01-31',
            dimensions: ['query', 'page']
        })
        expect(JSON.parse(String(sent[0]?.body))).toEqual(query)
    })
})

describe('inspection', () => {
    test("flattens one URL's index status", async () => {
        const richResults = { verdict: 'PASS', detectedItems: [{ richResultType: 'Breadcrumbs', items: [{ name: 'Unnamed item' }] }] }
        const { client } = over(() =>
            Response.json({
                inspectionResult: {
                    inspectionResultLink: 'https://search.google.com/search-console/inspect?resource_id=x',
                    indexStatusResult: {
                        verdict: 'PASS',
                        coverageState: 'Submitted and indexed',
                        robotsTxtState: 'ALLOWED',
                        indexingState: 'INDEXING_ALLOWED',
                        lastCrawlTime: '2026-11-01T00:00:00Z',
                        pageFetchState: 'SUCCESSFUL',
                        googleCanonical: 'https://example.com/a',
                        userCanonical: 'https://example.com/a',
                        crawledAs: 'MOBILE',
                        sitemap: [SITEMAP],
                        referringUrls: ['https://example.com/']
                    },
                    mobileUsabilityResult: { verdict: 'PASS' },
                    richResultsResult: richResults
                }
            })
        )
        expect(await client.inspection(DOMAIN, 'https://example.com/a')).toEqual({
            verdict: 'PASS',
            coverageState: 'Submitted and indexed',
            robotsTxtState: 'ALLOWED',
            indexingState: 'INDEXING_ALLOWED',
            pageFetchState: 'SUCCESSFUL',
            lastCrawlTime: '2026-11-01T00:00:00Z',
            crawledAs: 'MOBILE',
            googleCanonical: 'https://example.com/a',
            userCanonical: 'https://example.com/a',
            discoveredInSitemaps: [SITEMAP],
            referringUrls: ['https://example.com/'],
            mobileUsability: 'PASS',
            richResults,
            consoleLink: 'https://search.google.com/search-console/inspect?resource_id=x'
        })
    })

    test('says null of whatever Google has not established', async () => {
        const { client } = over(() => Response.json({ inspectionResult: {} }))
        expect(await client.inspection(DOMAIN, 'https://example.com/new')).toEqual({
            verdict: null,
            coverageState: null,
            robotsTxtState: null,
            indexingState: null,
            pageFetchState: null,
            lastCrawlTime: null,
            crawledAs: null,
            googleCanonical: null,
            userCanonical: null,
            discoveredInSitemaps: [],
            referringUrls: [],
            mobileUsability: null,
            richResults: null,
            consoleLink: null
        })
    })
})
