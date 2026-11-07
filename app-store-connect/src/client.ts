// The App Store Connect REST client: one per API key, as many as you like.
//
// Auth is a short-lived JWT signed with the key's .p8 private key — there is no
// refresh and no session, so a token is minted per request and nothing has to
// be held. Apple caps its lifetime at 20 minutes; this asks for far less, since
// each one is used once.
import { createPrivateKey, sign } from 'node:crypto'
import { ApiResponseError, bearerRequest, type ApiRequestInit, type ClientOptions } from '@greendrake/util'
import { readings } from './readings'
import type { App, AppInfo, AppStoreReviewDetail, AppStoreVersion, BetaGroup, BetaTester, Build, BuildBetaDetail, BuildBundle, BuildBundleFileSize, BundleId, Capability, Certificate, Credentials, Device, LocalizationLevel, Localizations, Page, Platform, PreReleaseVersion, Profile, Relationship, Resource, ReviewSubmission, ReviewSubmissionItem, Single } from './types'

const API = 'https://api.appstoreconnect.apple.com'
const TOKEN_LIFETIME_SECONDS = 300

const segment = (value: unknown): string => Buffer.from(JSON.stringify(value)).toString('base64url')

// The id a to-one linkage points at, or null when the relationship is empty or
// was not asked for.
export const toOneId = (relationship: Relationship | undefined): string | null => {
    const data = relationship?.data
    return data && !Array.isArray(data) ? data.id : null
}

// The included companions of one type, looked up by a resource's to-one
// relationship to them — null when it points at none.
export const includedBy = <I>(page: Page<unknown, I>, type: string) => {
    const byId = new Map(page.included.filter(r => r.type === type).map(r => [r.id, r]))
    return (relationship: Relationship | undefined): Resource<I> | null => byId.get(toOneId(relationship) ?? '') ?? null
}

// The ids a to-many linkage points at — none when the relationship is empty or
// was not asked for.
export const toManyIds = (relationship: Relationship | undefined): string[] => {
    const data = relationship?.data
    return Array.isArray(data) ? data.map(linkage => linkage.id) : []
}

// One page of a collection as the API answers it: `included` only where
// `include` asked for something, and the cursor to the next page, if any.
type PageAnswer<A, I> = Omit<Page<A, I>, 'included'> & { included?: Resource<I>[]; links: { next?: string } }

// A relationship as the API wants it stated on a write.
const one = (type: string, id: string) => ({ data: { type, id } })
const many = (type: string, ids: string[]) => ({ data: ids.map(id => ({ type, id })) })

// Each localization level's parent: the resource type it is listed under and
// the relationship it names that parent by.
const PARENTS: Record<LocalizationLevel, { relationship: string; type: string }> = {
    appInfoLocalizations: { relationship: 'appInfo', type: 'appInfos' },
    appStoreVersionLocalizations: { relationship: 'appStoreVersion', type: 'appStoreVersions' }
}

// The authorised round trips every call is made of, closed over one key and
// one fetch — exposed, so a call the client lacks is made over the same JWT,
// host, paging and absence.
const transport = (credentials: Credentials, options: ClientOptions) => {
    const { issuerId, keyId } = credentials
    // Parsed once, here, so a key that is not one fails at construction rather
    // than at the first call.
    const privateKey = typeof credentials.privateKey === 'string' ? createPrivateKey(credentials.privateKey) : credentials.privateKey

    const mint = (): string => {
        const now = Math.floor(options.now() / 1000)
        const input = `${segment({
            alg: 'ES256',
            kid: keyId,
            typ: 'JWT'
        })}.${segment({
            iss: issuerId,
            iat: now,
            exp: now + TOKEN_LIFETIME_SECONDS,
            aud: 'appstoreconnect-v1'
        })}`
        // JWS wants the raw r‖s pair; the default DER encoding would be refused.
        return `${input}.${sign('sha256', Buffer.from(input), { key: privateKey, dsaEncoding: 'ieee-p1363' }).toString('base64url')}`
    }
    const call = bearerRequest(options.fetch, mint)

    // One round trip to a path under the API's host, answering JSON. A path
    // and never a URL: the host is the client's.
    const request = <T>(path: string, init?: ApiRequestInit): Promise<T> => call<T>(`${API}${path}`, init)

    return {
        request,

        // Every page of a collection. Apple pages them, and a team that had
        // registered a lot of devices would silently lose the tail without
        // following the cursor. 200 a page is the maximum every collection the
        // client's own calls read documents, top-level and related-resource
        // alike; `params` naming a `limit` replaces it. The `/relationships/…`
        // linkage endpoints are a different kind of resource and are never
        // listed from; companions are reached through `include` instead.
        list: async <A, I = Record<string, unknown>>(path: string, params: Record<string, string> = {}): Promise<Page<A, I>> => {
            // The query is `params`: one in the path would be run into the
            // `?` that `params` follows, and read by Apple as a value.
            if (path.includes('?')) throw new Error(`list takes its query as params, not in the path: ${path}`)
            const collected: Page<A, I> = { data: [], included: [] }
            let next: string | undefined = `${API}${path}?${new URLSearchParams({ limit: '200', ...params }).toString()}`
            while (next) {
                const page: PageAnswer<A, I> = await call<PageAnswer<A, I>>(next)
                collected.data.push(...page.data)
                collected.included.push(...(page.included ?? []))
                next = page.links.next
            }
            return collected
        },

        // A to-one related resource that may not exist yet — an App Store
        // version's review detail before anyone has filled it in. Apple
        // answers that with 404, or with a null `data`, and both mean there is
        // none. Decided on the status, and only for the one status that
        // carries that meaning; anything else is the failure it is.
        getOrAbsent: async <A>(path: string): Promise<Resource<A> | null> => {
            try {
                return (await request<{ data: Resource<A> | null }>(path)).data
            } catch (error: unknown) {
                if (error instanceof ApiResponseError && error.status === 404) return null
                throw error
            }
        }
    }
}

export type Transport = ReturnType<typeof transport>

// Every call of the API, over one transport.
const endpoints = ({ request, list, getOrAbsent }: Transport) => ({
    // The record of a bundle id's app, or null when there is none. Records
    // are readable but not creatable — /v1/apps has no POST, so one is made
    // in App Store Connect and everything here works against it afterwards.
    findApp: async (bundleId: string): Promise<Resource<App> | null> => (await list<App>('/v1/apps', { 'filter[bundleId]': bundleId })).data[0] ?? null,

    // Capabilities come back with the identifiers rather than one request
    // each: the per-bundle relationship endpoint cannot be paged.
    listBundleIds: () => list<BundleId, Capability>('/v1/bundleIds', { include: 'bundleIdCapabilities' }),

    // Newest first, so a just-finished upload heads the list. Each build
    // carries the marketing-version train it was uploaded under, read from
    // its to-one preReleaseVersion: a build number alone says nothing about
    // which version it belongs to.
    listBuilds: async (appKey: string) => {
        const page = await list<Build, PreReleaseVersion>('/v1/builds', {
            'filter[app]': appKey,
            sort: '-uploadedDate',
            include: 'preReleaseVersion'
        })
        const train = includedBy(page, 'preReleaseVersions')
        return page.data.map(build => ({ ...build, train: train(build.relationships?.['preReleaseVersion'])?.attributes.version ?? null }))
    },

    // Whether a build is actually offerable to testers. `processingState`
    // only says Apple finished ingesting it; this says whether anything
    // still stands between it and a tester's device.
    buildBetaState: async (buildKey: string) => (await list<BuildBetaDetail>('/v1/buildBetaDetails', { 'filter[build]': buildKey })).data,

    // The groups a tester has to be *in*. Being an App Store Connect user
    // with access to the app grants no TestFlight build.
    listBetaGroups: async (appKey: string) => (await list<BetaGroup>('/v1/betaGroups', { 'filter[app]': appKey })).data,

    listBetaTesters: async (groupKey: string) => (await list<BetaTester>(`/v1/betaGroups/${groupKey}/betaTesters`)).data,

    listCertificates: async () => (await list<Certificate>('/v1/certificates')).data,

    listDevices: async () => (await list<Device>('/v1/devices')).data,

    // profileContent is the profile itself, base64'd. Naming the fields is
    // what guarantees it arrives: Apple may omit heavy attributes from a
    // collection otherwise.
    listProfiles: async () => (await list<Profile>('/v1/profiles', { 'fields[profiles]': 'name,profileType,profileState,uuid,expirationDate,profileContent' })).data,

    createBundleId: async (identifier: string, name: string, platform: string) =>
        (
            await request<Single<BundleId>>('/v1/bundleIds', {
                method: 'POST',
                json: {
                    data: {
                        type: 'bundleIds',
                        attributes: {
                            identifier,
                            name,
                            platform
                        }
                    }
                }
            })
        ).data,

    enableCapability: async (bundleIdKey: string, capabilityType: string) =>
        (
            await request<Single<Capability>>('/v1/bundleIdCapabilities', {
                method: 'POST',
                json: {
                    data: {
                        type: 'bundleIdCapabilities',
                        attributes: { capabilityType },
                        relationships: { bundleId: one('bundleIds', bundleIdKey) }
                    }
                }
            })
        ).data,

    registerDevice: async (name: string, udid: string, platform: string) =>
        (
            await request<Single<Device>>('/v1/devices', {
                method: 'POST',
                json: {
                    data: {
                        type: 'devices',
                        attributes: {
                            name,
                            udid,
                            platform
                        }
                    }
                }
            })
        ).data,

    createProfile: async (name: string, profileType: string, bundleIdKey: string, certificateIds: string[], deviceIds: string[]) =>
        (
            await request<Single<Profile>>('/v1/profiles', {
                method: 'POST',
                json: {
                    data: {
                        type: 'profiles',
                        attributes: { name, profileType },
                        relationships: {
                            bundleId: one('bundleIds', bundleIdKey),
                            certificates: many('certificates', certificateIds),
                            // A distribution profile takes no devices, and the
                            // API refuses the relationship rather than
                            // ignoring it.
                            ...(deviceIds.length ? { devices: many('devices', deviceIds) } : {})
                        }
                    }
                }
            })
        ).data,

    deleteProfile: (profileKey: string) => request<null>(`/v1/profiles/${profileKey}`, { method: 'DELETE' }),

    deleteBundleId: (bundleIdKey: string) => request<null>(`/v1/bundleIds/${bundleIdKey}`, { method: 'DELETE' }),

    // Apple issues against a certificate request; the private key stays
    // wherever it was generated, which is why a lost one cannot be
    // recovered from the account.
    createCertificate: async (csrContent: string, certificateType: string) => (await request<Single<Certificate>>('/v1/certificates', { method: 'POST', json: { data: { type: 'certificates', attributes: { csrContent, certificateType } } } })).data,

    // Revocation is what Apple calls deleting a certificate: the resource
    // goes, and the identity it names stops being accepted.
    revokeCertificate: (certificateKey: string) => request<null>(`/v1/certificates/${certificateKey}`, { method: 'DELETE' }),

    // Every App Store version of the app on a platform, optionally of one
    // version string only, each with the build attached to it — null until
    // one is. The record carries one per marketing version ever created, in
    // whatever state each reached.
    listAppStoreVersions: async (appKey: string, platform: Platform, versionString?: string) => {
        const page = await list<AppStoreVersion, Build>(`/v1/apps/${appKey}/appStoreVersions`, {
            'filter[platform]': platform,
            include: 'build',
            ...(versionString === undefined ? {} : { 'filter[versionString]': versionString })
        })
        const build = includedBy(page, 'builds')
        return page.data.map(version => ({ ...version, build: build(version.relationships?.['build']) }))
    },

    // What a build costs a device to download and to install, per device
    // model and OS version — App Store Connect's App File Sizes. Those are
    // its app bundle's: an App Clip or a watch app the build carries is
    // sized apart.
    buildFileSizes: async (buildKey: string) => {
        const { included = [] } = await request<Single<Build> & { included?: Resource<BuildBundle>[] }>(`/v1/builds/${buildKey}?include=buildBundles`)
        const app = included.find(r => r.type === 'buildBundles' && r.attributes.bundleType === 'APP')
        if (!app) throw new Error(`build ${buildKey} carries no app bundle`)
        return (await list<BuildBundleFileSize>(`/v1/buildBundles/${app.id}/buildBundleFileSizes`)).data.map(r => r.attributes)
    },

    createAppStoreVersion: async (appKey: string, platform: Platform, versionString: string, releaseType: string) =>
        (
            await request<Single<AppStoreVersion>>('/v1/appStoreVersions', {
                method: 'POST',
                json: {
                    data: {
                        type: 'appStoreVersions',
                        attributes: {
                            platform,
                            versionString,
                            releaseType
                        },
                        relationships: { app: one('apps', appKey) }
                    }
                }
            })
        ).data,

    updateReleaseType: async (versionKey: string, releaseType: string) =>
        (
            await request<Single<AppStoreVersion>>(`/v1/appStoreVersions/${versionKey}`, {
                method: 'PATCH',
                json: {
                    data: {
                        type: 'appStoreVersions',
                        id: versionKey,
                        attributes: { releaseType }
                    }
                }
            })
        ).data,

    attachBuild: (versionKey: string, buildKey: string) => request<null>(`/v1/appStoreVersions/${versionKey}/relationships/build`, { method: 'PATCH', json: one('builds', buildKey) }),

    listAppInfos: async (appKey: string) => (await list<AppInfo>(`/v1/apps/${appKey}/appInfos`)).data,

    listLocalizations: async <L extends LocalizationLevel>(level: L, parentKey: string) => (await list<Localizations[L]>(`/v1/${PARENTS[level].type}/${parentKey}/${level}`)).data,

    createLocalization: async <L extends LocalizationLevel>(level: L, parentKey: string, attributes: Partial<Localizations[L]> & { locale: string }) =>
        (
            await request<Single<Localizations[L]>>(`/v1/${level}`, {
                method: 'POST',
                json: {
                    data: {
                        type: level,
                        attributes,
                        relationships: { [PARENTS[level].relationship]: one(PARENTS[level].type, parentKey) }
                    }
                }
            })
        ).data,

    updateLocalization: async <L extends LocalizationLevel>(level: L, localizationKey: string, attributes: Partial<Localizations[L]>) =>
        (
            await request<Single<Localizations[L]>>(`/v1/${level}/${localizationKey}`, {
                method: 'PATCH',
                json: {
                    data: {
                        type: level,
                        id: localizationKey,
                        attributes
                    }
                }
            })
        ).data,

    deleteLocalization: (level: LocalizationLevel, localizationKey: string) => request<null>(`/v1/${level}/${localizationKey}`, { method: 'DELETE' }),

    // The reviewer-facing details of a version, or null when it has none
    // yet.
    reviewDetail: async (versionKey: string): Promise<AppStoreReviewDetail | null> => (await getOrAbsent<AppStoreReviewDetail>(`/v1/appStoreVersions/${versionKey}/appStoreReviewDetail`))?.attributes ?? null,

    // Submissions are per app and platform, and only one may be open at a
    // time; the API refuses a second.
    listReviewSubmissions: async (appKey: string, platform: Platform, state: string) => (await list<ReviewSubmission>(`/v1/apps/${appKey}/reviewSubmissions`, { 'filter[platform]': platform, 'filter[state]': state })).data,

    createReviewSubmission: async (appKey: string, platform: Platform) =>
        (
            await request<Single<ReviewSubmission>>('/v1/reviewSubmissions', {
                method: 'POST',
                json: {
                    data: {
                        type: 'reviewSubmissions',
                        attributes: { platform },
                        relationships: { app: one('apps', appKey) }
                    }
                }
            })
        ).data,

    // Each item with the App Store version it carries, which is what says
    // whether a version is already in an open submission — an open one can
    // hold other kinds of item (an in-app purchase, an app event), and "it
    // has items" says nothing.
    listReviewSubmissionItems: async (submissionKey: string) =>
        (await list<ReviewSubmissionItem>(`/v1/reviewSubmissions/${submissionKey}/items`, { include: 'appStoreVersion' })).data.map(item => ({
            ...item,
            appStoreVersionId: toOneId(item.relationships?.['appStoreVersion'])
        })),

    addReviewSubmissionItem: (submissionKey: string, versionKey: string) =>
        request<unknown>('/v1/reviewSubmissionItems', {
            method: 'POST',
            json: {
                data: {
                    type: 'reviewSubmissionItems',
                    relationships: { reviewSubmission: one('reviewSubmissions', submissionKey), appStoreVersion: one('appStoreVersions', versionKey) }
                }
            }
        }),

    submitReview: async (submissionKey: string) =>
        (
            await request<Single<ReviewSubmission>>(`/v1/reviewSubmissions/${submissionKey}`, {
                method: 'PATCH',
                json: {
                    data: {
                        type: 'reviewSubmissions',
                        id: submissionKey,
                        attributes: { submitted: true }
                    }
                }
            })
        ).data
})

export type Endpoints = ReturnType<typeof endpoints>

// A client for one API key: its transport, every call of the API over it, and
// the readings composed from them.
export const createAppStoreConnect = (credentials: Credentials, options: ClientOptions) => {
    const api = transport(credentials, options)
    const calls = endpoints(api)
    return {
        ...api,
        ...calls,
        ...readings(calls)
    }
}

export type AppStoreConnect = ReturnType<typeof createAppStoreConnect>
