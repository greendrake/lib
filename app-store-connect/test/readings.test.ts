import { describe, expect, test } from 'bun:test'
import type { AppInfoState, AppVersionState } from '../src/main'
import { clientOver, collection, resource, routed } from './fake'

const APP = resource('apps', 'app1', {
    name: 'List One',
    bundleId: 'one.list.app',
    sku: 'L1',
    primaryLocale: 'en-US'
})

const version = (id: string, versionString: string, appVersionState: AppVersionState, createdDate: string) =>
    resource('appStoreVersions', id, {
        versionString,
        platform: 'IOS',
        appVersionState,
        releaseType: 'MANUAL',
        createdDate
    })

const info = (id: string, state: AppInfoState) => resource('appInfos', id, { state })

const infoLocalization = (id: string, locale: string, name: string) =>
    resource('appInfoLocalizations', id, {
        locale,
        name,
        subtitle: `${name}, subtitled`,
        privacyPolicyUrl: null,
        privacyChoicesUrl: null
    })

const versionLocalization = (id: string, locale: string, description: string) =>
    resource('appStoreVersionLocalizations', id, {
        locale,
        description,
        keywords: 'lists',
        promotionalText: null,
        whatsNew: 'Fixes',
        supportUrl: null,
        marketingUrl: null
    })

// App Store Connect holding the app with these versions and app information,
// and each one's localizations by its id.
const holding = (versions: unknown[], infos: unknown[], localizations: Record<string, unknown[]>) =>
    routed({
        'GET /v1/apps': collection([APP]),
        'GET /v1/apps/app1/appStoreVersions': collection(versions),
        'GET /v1/apps/app1/appInfos': collection(infos),
        ...Object.fromEntries(Object.entries(localizations).map(([parent, held]) => [parent.startsWith('i') ? `GET /v1/appInfos/${parent}/appInfoLocalizations` : `GET /v1/appStoreVersions/${parent}/appStoreVersionLocalizations`, collection(held)]))
    })

const NOTHING = routed({ 'GET /v1/apps': collection([]) })

describe('appRecord', () => {
    test('is null when App Store Connect holds no record', async () => {
        expect(await clientOver(NOTHING.fetch).appRecord('one.list.app', 10)).toBeNull()
    })

    test('reports the record, its app information by locale, and the newest builds', async () => {
        const { fetch } = routed({
            'GET /v1/apps': collection([APP]),
            'GET /v1/apps/app1/appInfos': collection([info('i1', 'READY_FOR_DISTRIBUTION'), info('i2', 'PREPARE_FOR_SUBMISSION')]),
            'GET /v1/appInfos/i1/appInfoLocalizations': collection([infoLocalization('l1', 'en-US', 'List One')]),
            'GET /v1/appInfos/i2/appInfoLocalizations': collection([infoLocalization('l2', 'en-US', 'List One'), infoLocalization('l3', 'de-DE', 'Liste Eins')]),
            'GET /v1/builds': collection(
                [
                    resource(
                        'builds',
                        'b3',
                        {
                            version: '9',
                            uploadedDate: '2026-03-01',
                            processingState: 'VALID',
                            expired: false,
                            usesNonExemptEncryption: false
                        },
                        { preReleaseVersion: { data: { type: 'preReleaseVersions', id: 'p2' } } }
                    ),
                    resource(
                        'builds',
                        'b2',
                        {
                            version: '8',
                            uploadedDate: '2026-02-01',
                            processingState: 'VALID',
                            expired: false,
                            usesNonExemptEncryption: false
                        },
                        { preReleaseVersion: { data: { type: 'preReleaseVersions', id: 'p1' } } }
                    ),
                    resource('builds', 'b1', {
                        version: '7',
                        uploadedDate: '2026-01-01',
                        processingState: 'VALID',
                        expired: true,
                        usesNonExemptEncryption: false
                    })
                ],
                [resource('preReleaseVersions', 'p1', { version: '1.0', platform: 'IOS' }), resource('preReleaseVersions', 'p2', { version: '1.1', platform: 'IOS' })]
            )
        })
        expect(await clientOver(fetch).appRecord('one.list.app', 2)).toEqual({
            app: {
                id: 'app1',
                name: 'List One',
                sku: 'L1',
                primaryLocale: 'en-US'
            },
            appInfos: [
                {
                    id: 'i1',
                    state: 'READY_FOR_DISTRIBUTION',
                    locales: {
                        'en-US': {
                            id: 'l1',
                            name: 'List One',
                            subtitle: 'List One, subtitled'
                        }
                    }
                },
                {
                    id: 'i2',
                    state: 'PREPARE_FOR_SUBMISSION',
                    locales: {
                        'en-US': {
                            id: 'l2',
                            name: 'List One',
                            subtitle: 'List One, subtitled'
                        },
                        'de-DE': {
                            id: 'l3',
                            name: 'Liste Eins',
                            subtitle: 'Liste Eins, subtitled'
                        }
                    }
                }
            ],
            builds: [
                {
                    id: 'b3',
                    version: '9',
                    uploadedDate: '2026-03-01',
                    processingState: 'VALID',
                    expired: false,
                    train: '1.1'
                },
                {
                    id: 'b2',
                    version: '8',
                    uploadedDate: '2026-02-01',
                    processingState: 'VALID',
                    expired: false,
                    train: '1.0'
                }
            ]
        })
    })
})

describe('storePages', () => {
    test('is null when App Store Connect holds no record', async () => {
        expect(await clientOver(NOTHING.fetch).storePages('one.list.app', 'IOS')).toBeNull()
    })

    test('asks for the platform it is given', async () => {
        const { fetch, requests } = holding([], [], {})
        await clientOver(fetch).storePages('one.list.app', 'VISION_OS')
        const versions = requests.map(r => new URL(r.url)).find(url => url.pathname.endsWith('/appStoreVersions'))
        expect(versions?.searchParams.get('filter[platform]')).toBe('VISION_OS')
    })

    test('joins both levels by locale, null at the level that lacks one, with the ids a write takes', async () => {
        const { fetch } = holding([version('v1', '1.0', 'READY_FOR_DISTRIBUTION', '2026-01-01T00:00:00Z')], [info('i1', 'READY_FOR_DISTRIBUTION')], {
            i1: [infoLocalization('a-en', 'en-US', 'List One'), infoLocalization('a-de', 'de-DE', 'Liste Eins')],
            v1: [versionLocalization('v-en', 'en-US', 'Lists of anything'), versionLocalization('v-fr', 'fr-FR', 'Des listes')]
        })
        expect(await clientOver(fetch).storePages('one.list.app', 'IOS')).toEqual({
            app: { id: 'app1', primaryLocale: 'en-US' },
            pages: [
                {
                    version: {
                        id: 'v1',
                        versionString: '1.0',
                        appVersionState: 'READY_FOR_DISTRIBUTION',
                        releaseType: 'MANUAL',
                        createdDate: '2026-01-01T00:00:00Z'
                    },
                    appInfo: { id: 'i1', state: 'READY_FOR_DISTRIBUTION' },
                    locales: {
                        'en-US': {
                            appInfo: {
                                id: 'a-en',
                                name: 'List One',
                                subtitle: 'List One, subtitled'
                            },
                            version: {
                                id: 'v-en',
                                description: 'Lists of anything',
                                keywords: 'lists',
                                promotionalText: null,
                                whatsNew: 'Fixes'
                            }
                        },
                        'de-DE': {
                            appInfo: {
                                id: 'a-de',
                                name: 'Liste Eins',
                                subtitle: 'Liste Eins, subtitled'
                            },
                            version: null
                        },
                        'fr-FR': {
                            appInfo: null,
                            version: {
                                id: 'v-fr',
                                description: 'Des listes',
                                keywords: 'lists',
                                promotionalText: null,
                                whatsNew: 'Fixes'
                            }
                        }
                    }
                }
            ]
        })
    })

    // Which pages are shown, each as [version, the app information it pairs with].
    test.each([
        [
            'on sale, and a newer version being prepared',
            [version('v0', '0.9', 'REPLACED_WITH_NEW_VERSION', '2025-12-01T00:00:00Z'), version('v2', '1.1', 'PREPARE_FOR_SUBMISSION', '2026-03-01T00:00:00Z'), version('v1', '1.0', 'READY_FOR_DISTRIBUTION', '2026-01-01T00:00:00Z')],
            [info('i1', 'READY_FOR_DISTRIBUTION'), info('i2', 'PREPARE_FOR_SUBMISSION')],
            [
                ['1.0', 'i1'],
                ['1.1', 'i2']
            ]
        ],
        [
            'on sale, and a newer version approved and held for a manual release',
            [version('v1', '1.0', 'READY_FOR_DISTRIBUTION', '2026-01-01T00:00:00Z'), version('v2', '1.1', 'PENDING_DEVELOPER_RELEASE', '2026-03-01T00:00:00Z')],
            [info('i2', 'PENDING_RELEASE'), info('i1', 'READY_FOR_DISTRIBUTION')],
            [
                ['1.0', 'i1'],
                ['1.1', 'i2']
            ]
        ],
        ['on sale alone', [version('v0', '0.9', 'REPLACED_WITH_NEW_VERSION', '2025-12-01T00:00:00Z'), version('v1', '1.0', 'READY_FOR_DISTRIBUTION', '2026-01-01T00:00:00Z')], [info('i1', 'READY_FOR_DISTRIBUTION')], [['1.0', 'i1']]],
        ['never released', [version('v1', '1.0', 'WAITING_FOR_REVIEW', '2026-01-01T00:00:00Z')], [info('i1', 'WAITING_FOR_REVIEW')], [['1.0', 'i1']]],
        ['no version at all', [], [info('i1', 'PREPARE_FOR_SUBMISSION')], []]
    ] as const)('%s', async (_name, versions, infos, shown) => {
        const { fetch } = holding([...versions], [...infos], {
            i1: [],
            i2: [],
            v0: [],
            v1: [],
            v2: []
        })
        const pages = (await clientOver(fetch).storePages('one.list.app', 'IOS'))?.pages ?? []
        expect(pages.map(page => [page.version.versionString, page.appInfo.id])).toEqual(shown.map(pair => [...pair]))
    })

    test('errs naming the states found when a page has no single app information to pair with', async () => {
        const { fetch } = holding([version('v1', '1.0', 'READY_FOR_DISTRIBUTION', '2026-01-01T00:00:00Z'), version('v2', '1.1', 'PREPARE_FOR_SUBMISSION', '2026-03-01T00:00:00Z')], [info('i1', 'READY_FOR_DISTRIBUTION')], {
            i1: [],
            v1: [],
            v2: []
        })
        await expect(clientOver(fetch).storePages('one.list.app', 'IOS')).rejects.toThrow("one.list.app 1.1 (PREPARE_FOR_SUBMISSION) pairs with no single app information in flight; the app's information is in READY_FOR_DISTRIBUTION")
    })
})
