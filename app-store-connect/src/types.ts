// The resource shapes the client reads and writes, as the API has them, and the
// records its readings answer.
import type { KeyObject } from 'node:crypto'

// An API key: the issuer and key id from App Store Connect > Users and Access >
// Integrations, and the .p8 private key — its PEM, or the key already parsed.
export interface Credentials {
    issuerId: string
    keyId: string
    privateKey: string | KeyObject
}

export type Platform = 'IOS' | 'MAC_OS' | 'TV_OS' | 'VISION_OS'

// An App Store version's state. The API also carries `appStoreState`, in an
// older vocabulary it marks deprecated; this is the one read.
export type AppVersionState = 'ACCEPTED' | 'DEVELOPER_REJECTED' | 'IN_REVIEW' | 'INVALID_BINARY' | 'METADATA_REJECTED' | 'PENDING_APPLE_RELEASE' | 'PENDING_DEVELOPER_RELEASE' | 'PREPARE_FOR_SUBMISSION' | 'PROCESSING_FOR_DISTRIBUTION' | 'READY_FOR_DISTRIBUTION' | 'READY_FOR_REVIEW' | 'REJECTED' | 'REPLACED_WITH_NEW_VERSION' | 'WAITING_FOR_EXPORT_COMPLIANCE' | 'WAITING_FOR_REVIEW'

// The state of an app's information record — the app-level half of its App
// Store page.
export type AppInfoState = 'ACCEPTED' | 'DEVELOPER_REJECTED' | 'IN_REVIEW' | 'PENDING_RELEASE' | 'PREPARE_FOR_SUBMISSION' | 'READY_FOR_DISTRIBUTION' | 'READY_FOR_REVIEW' | 'REJECTED' | 'REPLACED_WITH_NEW_INFO' | 'WAITING_FOR_REVIEW'

export interface Resource<A> {
    type: string
    id: string
    attributes: A
}

export interface Single<A> {
    data: Resource<A>
}

// What a relationship points at. Its linkage is one object for a to-one
// relationship (a build's preReleaseVersion) and an array for a to-many (a
// bundle id's capabilities); the API states which per relationship, so a
// reader picks the shape it knows rather than indexing blindly. Absent when
// the relationship was neither included nor asked for.
export interface Linkage {
    type: string
    id: string
}
export interface Relationship {
    data?: Linkage | Linkage[] | null
}

// A collection, every page of it, with whatever `include` asked for alongside.
// The relationships are the only way back from a resource to its included
// companions: `included` is flat, and typed by whatever `include` asked for.
export interface Page<A, I = Record<string, unknown>> {
    data: (Resource<A> & { relationships?: Record<string, Relationship> })[]
    included: Resource<I>[]
}

export interface BundleId {
    identifier: string
    name: string
    platform: string
    seedId: string
}
export interface Capability {
    capabilityType: string
}
export interface Certificate {
    name: string
    certificateType: string
    displayName: string
    expirationDate: string
    // The certificate itself, base64 DER — the half Apple keeps and will hand
    // back; the private key half exists only where the request was generated.
    certificateContent: string
}
export interface Device {
    name: string
    udid: string
    platform: string
    status: string
    deviceClass: string
}
export interface App {
    name: string
    bundleId: string
    sku: string
    primaryLocale: string
}
export interface Build {
    version: string
    uploadedDate: string
    processingState: string
    expired: boolean
    // Null until export compliance is answered — and a build with it unanswered
    // is invisible to every tester, however correctly they are set up.
    usesNonExemptEncryption: boolean | null
}
// The marketing-version train a build was uploaded under.
export interface PreReleaseVersion {
    version: string
    platform: string
}
export interface BuildBetaDetail {
    internalBuildState: string
    externalBuildState: string
}
export interface BetaGroup {
    name: string
    isInternalGroup: boolean
    hasAccessToAllBuilds: boolean | null
}
export interface BetaTester {
    email: string
    firstName: string
    lastName: string
    state: string
}
export interface Profile {
    name: string
    profileType: string
    profileState: string
    uuid: string
    expirationDate: string
    profileContent: string
}

export interface AppStoreVersion {
    versionString: string
    platform: Platform
    appVersionState: AppVersionState
    releaseType: string
    createdDate: string
}
// One app, App Clip or watch app inside a build.
export interface BuildBundle {
    bundleType: 'APP' | 'APP_CLIP' | 'WATCH_APP'
}
export interface BuildBundleFileSize {
    deviceModel: string
    osVersion: string
    downloadBytes: number
    installBytes: number
}
export interface AppStoreVersionLocalization {
    locale: string
    description: string | null
    keywords: string | null
    // The one field of a version's page that can change while the version is
    // on sale, without a new version or a review.
    promotionalText: string | null
    whatsNew: string | null
    supportUrl: string | null
    marketingUrl: string | null
}
// The app-level half of an App Store page's metadata, which every version
// shares. One record per state the information is in: a live app has the one
// on sale (READY_FOR_DISTRIBUTION), which refuses writes, and, while a version
// is in flight, the one going with it.
export interface AppInfo {
    state: AppInfoState
}
export interface AppInfoLocalization {
    locale: string
    name: string
    subtitle: string | null
    privacyPolicyUrl: string | null
    privacyChoicesUrl: string | null
}
// The reviewer-facing details: credentials and notes.
export interface AppStoreReviewDetail {
    contactEmail: string | null
    demoAccountName: string | null
    demoAccountRequired: boolean | null
    notes: string | null
}
export interface ReviewSubmission {
    platform: Platform
    state: string
    submittedDate: string | null
}
export interface ReviewSubmissionItem {
    state: string
}

// The two levels an App Store page is localized at — the app's information
// (name, subtitle, privacy policy) and a version's (description, keywords,
// What's New) — are one kind of resource twice over: listed under their parent,
// created related to it, patched and deleted by their own id. Each is named by
// its resource type, with the attributes it carries.
export interface Localizations {
    appInfoLocalizations: AppInfoLocalization
    appStoreVersionLocalizations: AppStoreVersionLocalization
}
export type LocalizationLevel = keyof Localizations

// --- What the readings answer --------------------------------------------------

// An app-information localization, as the readings report it.
export interface AppInfoLocale {
    id: string
    name: string
    subtitle: string | null
}
// A version localization, as the readings report it.
export interface VersionLocale {
    id: string
    description: string | null
    keywords: string | null
    promotionalText: string | null
    whatsNew: string | null
}
// An app-information record with its localizations by locale.
export interface AppInfoRecord {
    id: string
    state: AppInfoState
    locales: Record<string, AppInfoLocale>
}

export interface AppRecord {
    app: { id: string; name: string; sku: string; primaryLocale: string }
    appInfos: AppInfoRecord[]
    // Newest first.
    builds: { id: string; version: string; uploadedDate: string; processingState: string; expired: boolean; train: string | null }[]
}

// One App Store page: a version, the app information that goes with it, and
// both levels' localizations joined by locale — null at a level that lacks the
// locale.
export interface StorePage {
    version: { id: string; versionString: string; appVersionState: AppVersionState; releaseType: string; createdDate: string }
    appInfo: { id: string; state: AppInfoState }
    locales: Record<string, { appInfo: AppInfoLocale | null; version: VersionLocale | null }>
}

export interface StorePages {
    app: { id: string; primaryLocale: string }
    // The page on sale, if any, and the newest version's when that is another.
    pages: StorePage[]
}
