import { describe, expect, test } from 'bun:test'
import { verify } from 'node:crypto'
import { ApiResponseError, createAppStoreConnect, toManyIds, type AppStoreConnect } from '../src/main'
import { recordingFetch } from '@greendrake/util/testing'
import { NOW, clientOver, collection, jsonOf, key, resource, routed } from './fake'

const decode = (segment: string): unknown => JSON.parse(Buffer.from(segment, 'base64url').toString())

const EMPTY = () => Response.json(collection([]))

describe('authentication', () => {
    test('signs each request with an ES256 JWT for the key, minted from the clock', async () => {
        const { fetch, requests: sent } = recordingFetch(EMPTY)
        await clientOver(fetch).listDevices()
        const token = sent[0]?.headers.get('Authorization')?.replace(/^Bearer /, '') ?? ''
        const [header = '', claims = '', signature = ''] = token.split('.')
        expect(decode(header)).toEqual({
            alg: 'ES256',
            kid: 'KEY123',
            typ: 'JWT'
        })
        expect(decode(claims)).toEqual({
            iss: 'issuer-1',
            iat: NOW / 1000,
            exp: NOW / 1000 + 300,
            aud: 'appstoreconnect-v1'
        })
        // IEEE P1363 — the raw r‖s pair JWS wants — and not DER.
        expect(verify('sha256', Buffer.from(`${header}.${claims}`), { key: key.publicKey, dsaEncoding: 'ieee-p1363' }, Buffer.from(signature, 'base64url'))).toBe(true)
    })

    test('mints a fresh token per request', async () => {
        let now = NOW
        const { fetch, requests: sent } = recordingFetch(EMPTY)
        const client = createAppStoreConnect(
            {
                issuerId: 'issuer-1',
                keyId: 'KEY123',
                privateKey: key.privateKey
            },
            { fetch, now: () => now }
        )
        await client.listDevices()
        now += 60_000
        await client.listDevices()
        const issued = sent.map(s => (decode(s.headers.get('Authorization')?.split('.')[1] ?? '') as { iat: number }).iat)
        expect(issued).toEqual([NOW / 1000, NOW / 1000 + 60])
    })

    test('a key that is not one fails at construction', () => {
        const { fetch } = recordingFetch(EMPTY)
        expect(() =>
            createAppStoreConnect(
                {
                    issuerId: 'i',
                    keyId: 'k',
                    privateKey: 'not a key'
                },
                { fetch, now: () => NOW }
            )
        ).toThrow()
    })
})

const LIMIT = { limit: '200' }

// Each call's request: method, path, query and JSON body.
const REQUESTS: [string, (client: AppStoreConnect) => Promise<unknown>, string, string, Record<string, string>, unknown][] = [
    ['findApp', c => c.findApp('one.list.app'), 'GET', '/v1/apps', { ...LIMIT, 'filter[bundleId]': 'one.list.app' }, null],
    ['listBundleIds', c => c.listBundleIds(), 'GET', '/v1/bundleIds', { ...LIMIT, include: 'bundleIdCapabilities' }, null],
    [
        'listBuilds',
        c => c.listBuilds('app1'),
        'GET',
        '/v1/builds',
        {
            ...LIMIT,
            'filter[app]': 'app1',
            sort: '-uploadedDate',
            include: 'preReleaseVersion'
        },
        null
    ],
    ['buildBetaState', c => c.buildBetaState('b1'), 'GET', '/v1/buildBetaDetails', { ...LIMIT, 'filter[build]': 'b1' }, null],
    ['listBetaGroups', c => c.listBetaGroups('app1'), 'GET', '/v1/betaGroups', { ...LIMIT, 'filter[app]': 'app1' }, null],
    ['listBetaTesters', c => c.listBetaTesters('g1'), 'GET', '/v1/betaGroups/g1/betaTesters', LIMIT, null],
    ['listCertificates', c => c.listCertificates(), 'GET', '/v1/certificates', LIMIT, null],
    ['listDevices', c => c.listDevices(), 'GET', '/v1/devices', LIMIT, null],
    ['listProfiles', c => c.listProfiles(), 'GET', '/v1/profiles', { ...LIMIT, 'fields[profiles]': 'name,profileType,profileState,uuid,expirationDate,profileContent' }, null],
    [
        'createBundleId',
        c => c.createBundleId('one.list.app', 'List One', 'UNIVERSAL'),
        'POST',
        '/v1/bundleIds',
        {},
        {
            data: {
                type: 'bundleIds',
                attributes: {
                    identifier: 'one.list.app',
                    name: 'List One',
                    platform: 'UNIVERSAL'
                }
            }
        }
    ],
    [
        'enableCapability',
        c => c.enableCapability('bid1', 'PUSH_NOTIFICATIONS'),
        'POST',
        '/v1/bundleIdCapabilities',
        {},
        {
            data: {
                type: 'bundleIdCapabilities',
                attributes: { capabilityType: 'PUSH_NOTIFICATIONS' },
                relationships: { bundleId: { data: { type: 'bundleIds', id: 'bid1' } } }
            }
        }
    ],
    [
        'registerDevice',
        c => c.registerDevice('Phone', 'udid1', 'IOS'),
        'POST',
        '/v1/devices',
        {},
        {
            data: {
                type: 'devices',
                attributes: {
                    name: 'Phone',
                    udid: 'udid1',
                    platform: 'IOS'
                }
            }
        }
    ],
    [
        'createProfile with devices',
        c => c.createProfile('Dev', 'IOS_APP_DEVELOPMENT', 'bid1', ['c1'], ['d1', 'd2']),
        'POST',
        '/v1/profiles',
        {},
        {
            data: {
                type: 'profiles',
                attributes: { name: 'Dev', profileType: 'IOS_APP_DEVELOPMENT' },
                relationships: {
                    bundleId: { data: { type: 'bundleIds', id: 'bid1' } },
                    certificates: { data: [{ type: 'certificates', id: 'c1' }] },
                    devices: {
                        data: [
                            { type: 'devices', id: 'd1' },
                            { type: 'devices', id: 'd2' }
                        ]
                    }
                }
            }
        }
    ],
    [
        'createProfile without devices, which a distribution profile takes none of',
        c => c.createProfile('Store', 'IOS_APP_STORE', 'bid1', ['c1'], []),
        'POST',
        '/v1/profiles',
        {},
        {
            data: {
                type: 'profiles',
                attributes: { name: 'Store', profileType: 'IOS_APP_STORE' },
                relationships: { bundleId: { data: { type: 'bundleIds', id: 'bid1' } }, certificates: { data: [{ type: 'certificates', id: 'c1' }] } }
            }
        }
    ],
    ['deleteProfile', c => c.deleteProfile('p1'), 'DELETE', '/v1/profiles/p1', {}, null],
    ['deleteBundleId', c => c.deleteBundleId('bid1'), 'DELETE', '/v1/bundleIds/bid1', {}, null],
    ['createCertificate', c => c.createCertificate('CSR', 'DISTRIBUTION'), 'POST', '/v1/certificates', {}, { data: { type: 'certificates', attributes: { csrContent: 'CSR', certificateType: 'DISTRIBUTION' } } }],
    ['revokeCertificate', c => c.revokeCertificate('c1'), 'DELETE', '/v1/certificates/c1', {}, null],
    [
        'listAppStoreVersions',
        c => c.listAppStoreVersions('app1', 'IOS'),
        'GET',
        '/v1/apps/app1/appStoreVersions',
        {
            ...LIMIT,
            'filter[platform]': 'IOS',
            include: 'build'
        },
        null
    ],
    [
        'listAppStoreVersions of one version string',
        c => c.listAppStoreVersions('app1', 'MAC_OS', '2.0'),
        'GET',
        '/v1/apps/app1/appStoreVersions',
        {
            ...LIMIT,
            'filter[platform]': 'MAC_OS',
            include: 'build',
            'filter[versionString]': '2.0'
        },
        null
    ],
    [
        'createAppStoreVersion',
        c => c.createAppStoreVersion('app1', 'TV_OS', '2.0', 'MANUAL'),
        'POST',
        '/v1/appStoreVersions',
        {},
        {
            data: {
                type: 'appStoreVersions',
                attributes: {
                    platform: 'TV_OS',
                    versionString: '2.0',
                    releaseType: 'MANUAL'
                },
                relationships: { app: { data: { type: 'apps', id: 'app1' } } }
            }
        }
    ],
    [
        'updateReleaseType',
        c => c.updateReleaseType('v1', 'AFTER_APPROVAL'),
        'PATCH',
        '/v1/appStoreVersions/v1',
        {},
        {
            data: {
                type: 'appStoreVersions',
                id: 'v1',
                attributes: { releaseType: 'AFTER_APPROVAL' }
            }
        }
    ],
    ['attachBuild', c => c.attachBuild('v1', 'b1'), 'PATCH', '/v1/appStoreVersions/v1/relationships/build', {}, { data: { type: 'builds', id: 'b1' } }],
    ['listAppInfos', c => c.listAppInfos('app1'), 'GET', '/v1/apps/app1/appInfos', LIMIT, null],
    ['listLocalizations of the app information', c => c.listLocalizations('appInfoLocalizations', 'i1'), 'GET', '/v1/appInfos/i1/appInfoLocalizations', LIMIT, null],
    ['listLocalizations of a version', c => c.listLocalizations('appStoreVersionLocalizations', 'v1'), 'GET', '/v1/appStoreVersions/v1/appStoreVersionLocalizations', LIMIT, null],
    [
        'createLocalization of the app information',
        c => c.createLocalization('appInfoLocalizations', 'i1', { locale: 'de-DE', name: 'Liste' }),
        'POST',
        '/v1/appInfoLocalizations',
        {},
        {
            data: {
                type: 'appInfoLocalizations',
                attributes: { locale: 'de-DE', name: 'Liste' },
                relationships: { appInfo: { data: { type: 'appInfos', id: 'i1' } } }
            }
        }
    ],
    [
        'createLocalization of a version',
        c => c.createLocalization('appStoreVersionLocalizations', 'v1', { locale: 'de-DE', description: 'Listen' }),
        'POST',
        '/v1/appStoreVersionLocalizations',
        {},
        {
            data: {
                type: 'appStoreVersionLocalizations',
                attributes: { locale: 'de-DE', description: 'Listen' },
                relationships: { appStoreVersion: { data: { type: 'appStoreVersions', id: 'v1' } } }
            }
        }
    ],
    [
        'updateLocalization, writing promotional text',
        c => c.updateLocalization('appStoreVersionLocalizations', 'l1', { promotionalText: 'Now with more' }),
        'PATCH',
        '/v1/appStoreVersionLocalizations/l1',
        {},
        {
            data: {
                type: 'appStoreVersionLocalizations',
                id: 'l1',
                attributes: { promotionalText: 'Now with more' }
            }
        }
    ],
    ['deleteLocalization', c => c.deleteLocalization('appInfoLocalizations', 'l2'), 'DELETE', '/v1/appInfoLocalizations/l2', {}, null],
    ['reviewDetail', c => c.reviewDetail('v1'), 'GET', '/v1/appStoreVersions/v1/appStoreReviewDetail', {}, null],
    [
        'listReviewSubmissions',
        c => c.listReviewSubmissions('app1', 'IOS', 'READY_FOR_REVIEW'),
        'GET',
        '/v1/apps/app1/reviewSubmissions',
        {
            ...LIMIT,
            'filter[platform]': 'IOS',
            'filter[state]': 'READY_FOR_REVIEW'
        },
        null
    ],
    [
        'createReviewSubmission',
        c => c.createReviewSubmission('app1', 'VISION_OS'),
        'POST',
        '/v1/reviewSubmissions',
        {},
        {
            data: {
                type: 'reviewSubmissions',
                attributes: { platform: 'VISION_OS' },
                relationships: { app: { data: { type: 'apps', id: 'app1' } } }
            }
        }
    ],
    ['listReviewSubmissionItems', c => c.listReviewSubmissionItems('s1'), 'GET', '/v1/reviewSubmissions/s1/items', { ...LIMIT, include: 'appStoreVersion' }, null],
    [
        'addReviewSubmissionItem',
        c => c.addReviewSubmissionItem('s1', 'v1'),
        'POST',
        '/v1/reviewSubmissionItems',
        {},
        {
            data: {
                type: 'reviewSubmissionItems',
                relationships: { reviewSubmission: { data: { type: 'reviewSubmissions', id: 's1' } }, appStoreVersion: { data: { type: 'appStoreVersions', id: 'v1' } } }
            }
        }
    ],
    [
        'submitReview',
        c => c.submitReview('s1'),
        'PATCH',
        '/v1/reviewSubmissions/s1',
        {},
        {
            data: {
                type: 'reviewSubmissions',
                id: 's1',
                attributes: { submitted: true }
            }
        }
    ]
]

describe('requests', () => {
    test.each(REQUESTS)('%s', async (_name, call, method, pathname, query, body) => {
        const { fetch, requests: sent } = recordingFetch(EMPTY)
        await call(clientOver(fetch))
        expect(sent).toHaveLength(1)
        const [request] = sent
        const url = new URL(request?.url ?? '')
        expect(request?.method).toBe(method)
        expect(url.origin).toBe('https://api.appstoreconnect.apple.com')
        expect(url.pathname).toBe(pathname)
        expect(Object.fromEntries(url.searchParams)).toEqual(query)
        expect(jsonOf(request)).toEqual(body)
        expect(request?.headers.get('Content-Type')).toBe(body === null ? null : 'application/json')
    })
})

describe('answers', () => {
    test('a collection is followed page by page until there is no next', async () => {
        const next = 'https://api.appstoreconnect.apple.com/v1/bundleIds?limit=200&include=bundleIdCapabilities&cursor=Mg'
        const pages = [
            {
                data: [resource('bundleIds', 'b1', {})],
                included: [resource('bundleIdCapabilities', 'c1', { capabilityType: 'PUSH_NOTIFICATIONS' })],
                links: { self: 'x', next }
            },
            { data: [resource('bundleIds', 'b2', {})], links: { self: next } }
        ]
        const { fetch, requests: sent } = recordingFetch(() => Response.json(pages.shift()))
        const { data, included } = await clientOver(fetch).listBundleIds()
        expect(data.map(r => r.id)).toEqual(['b1', 'b2'])
        expect(included.map(r => r.attributes.capabilityType)).toEqual(['PUSH_NOTIFICATIONS'])
        expect(sent.map(s => s.url)).toEqual(['https://api.appstoreconnect.apple.com/v1/bundleIds?limit=200&include=bundleIdCapabilities', next])
    })

    test('a build carries the train it was uploaded under, or null for none', async () => {
        const { fetch } = routed({
            'GET /v1/builds': collection([resource('builds', 'b2', { version: '7' }, { preReleaseVersion: { data: { type: 'preReleaseVersions', id: 'p1' } } }), resource('builds', 'b1', { version: '6' })], [resource('preReleaseVersions', 'p1', { version: '1.2', platform: 'IOS' })])
        })
        expect((await clientOver(fetch).listBuilds('app1')).map(b => [b.id, b.train])).toEqual([
            ['b2', '1.2'],
            ['b1', null]
        ])
    })

    test('a version carries the build attached to it, or null for none', async () => {
        const { fetch } = routed({
            'GET /v1/apps/app1/appStoreVersions': collection([resource('appStoreVersions', 'v2', { versionString: '1.1' }, { build: { data: { type: 'builds', id: 'b7' } } }), resource('appStoreVersions', 'v1', { versionString: '1.0' }, { build: { data: null } })], [resource('builds', 'b7', { version: '7' })])
        })
        expect((await clientOver(fetch).listAppStoreVersions('app1', 'IOS')).map(v => [v.id, v.build?.attributes.version ?? null])).toEqual([
            ['v2', '7'],
            ['v1', null]
        ])
    })

    test('a submission item carries the version it holds, or null for another kind of item', async () => {
        const { fetch } = routed({
            'GET /v1/reviewSubmissions/s1/items': collection([resource('reviewSubmissionItems', 'i1', { state: 'READY_FOR_REVIEW' }, { appStoreVersion: { data: { type: 'appStoreVersions', id: 'v1' } } }), resource('reviewSubmissionItems', 'i2', { state: 'READY_FOR_REVIEW' })])
        })
        expect((await clientOver(fetch).listReviewSubmissionItems('s1')).map(i => i.appStoreVersionId)).toEqual(['v1', null])
    })

    test('a bundle id names its capabilities by a to-many relationship', () => {
        expect(
            toManyIds({
                data: [
                    { type: 'bundleIdCapabilities', id: 'c1' },
                    { type: 'bundleIdCapabilities', id: 'c2' }
                ]
            })
        ).toEqual(['c1', 'c2'])
        expect(toManyIds({ data: { type: 'x', id: 'one' } })).toEqual([])
        expect(toManyIds(undefined)).toEqual([])
    })

    test('a write answers the resource it made', async () => {
        const device = resource('devices', 'd1', {
            name: 'Phone',
            udid: 'udid1',
            platform: 'IOS',
            status: 'ENABLED',
            deviceClass: 'IPHONE'
        })
        const { fetch } = recordingFetch(() => Response.json({ data: device }, { status: 201 }))
        expect(await clientOver(fetch).registerDevice('Phone', 'udid1', 'IOS')).toEqual(device)
    })

    test('the app of a bundle id, or null when App Store Connect holds no record', async () => {
        const app = resource('apps', 'app1', {
            name: 'List One',
            bundleId: 'one.list.app',
            sku: 'L1',
            primaryLocale: 'en-US'
        })
        expect(await clientOver(routed({ 'GET /v1/apps': collection([app]) }).fetch).findApp('one.list.app')).toEqual(app)
        expect(await clientOver(routed({ 'GET /v1/apps': collection([]) }).fetch).findApp('one.list.app')).toBeNull()
    })
})

describe('buildFileSizes', () => {
    test("sizes the build's app bundle, not an App Clip it carries", async () => {
        const sizes = [
            {
                deviceModel: 'iPhone17,1',
                osVersion: '26.0',
                downloadBytes: 1,
                installBytes: 2
            }
        ]
        const { fetch, requests: sent } = routed({
            'GET /v1/builds/b1': { data: resource('builds', 'b1', {}), included: [resource('buildBundles', 'clip', { bundleType: 'APP_CLIP' }), resource('buildBundles', 'main', { bundleType: 'APP' })] },
            'GET /v1/buildBundles/main/buildBundleFileSizes': collection([resource('buildBundleFileSizes', 's1', sizes[0])])
        })
        expect(await clientOver(fetch).buildFileSizes('b1')).toEqual(sizes)
        expect(new URL(sent[0]?.url ?? '').searchParams.get('include')).toBe('buildBundles')
    })

    test('errs on a build carrying no app bundle', async () => {
        const { fetch } = routed({ 'GET /v1/builds/b1': { data: resource('builds', 'b1', {}) } })
        await expect(clientOver(fetch).buildFileSizes('b1')).rejects.toThrow('build b1 carries no app bundle')
    })
})

describe('reviewDetail', () => {
    const PATH = 'GET /v1/appStoreVersions/v1/appStoreReviewDetail'
    const detail = {
        contactEmail: 'a@example.com',
        demoAccountName: 'demo',
        demoAccountRequired: true,
        notes: null
    }

    test('answers the detail a version has', async () => {
        expect(await clientOver(routed({ [PATH]: { data: resource('appStoreReviewDetails', 'r1', detail) } }).fetch).reviewDetail('v1')).toEqual(detail)
    })

    test.each([
        ['a 404', () => Response.json({ errors: [{ status: '404' }] }, { status: 404 })],
        ['a null data', () => Response.json({ data: null })]
    ])('answers null for %s', async (_name, answer) => {
        expect(await clientOver(recordingFetch(answer).fetch).reviewDetail('v1')).toBeNull()
    })

    test('any other status is the failure it is', async () => {
        const { fetch } = recordingFetch(() => Response.json({ errors: [{ status: '500' }] }, { status: 500 }))
        await expect(clientOver(fetch).reviewDetail('v1')).rejects.toBeInstanceOf(ApiResponseError)
    })
})

describe('failures', () => {
    test("a status throws ApiResponseError carrying Apple's whole answer", async () => {
        const answer = {
            errors: [
                {
                    status: '409',
                    code: 'ENTITY_ERROR',
                    detail: 'An attribute value is not acceptable.'
                }
            ]
        }
        const { fetch } = recordingFetch(() => Response.json(answer, { status: 409 }))
        const error = await clientOver(fetch)
            .createBundleId('one.list.app', 'List One', 'IOS')
            .catch((e: unknown) => e)
        expect(error).toBeInstanceOf(ApiResponseError)
        expect(error).toMatchObject({
            method: 'POST',
            path: '/v1/bundleIds',
            status: 409,
            body: answer
        })
    })

    test('a fetch that gets no answer rejects the call with its own error, untouched', async () => {
        const unreachable = new TypeError('fetch failed')
        const client = createAppStoreConnect(
            {
                issuerId: 'i',
                keyId: 'k',
                privateKey: key.privateKey
            },
            { fetch: () => Promise.reject(unreachable), now: () => NOW }
        )
        await expect(client.listDevices()).rejects.toBe(unreachable)
    })
})
