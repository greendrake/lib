export { createAppStoreConnect, toManyIds, type AppStoreConnect } from './client'
export type { App, AppInfo, AppInfoLocale, AppInfoLocalization, AppInfoRecord, AppInfoState, AppRecord, AppStoreReviewDetail, AppStoreVersion, AppStoreVersionLocalization, AppVersionState, BetaGroup, BetaTester, Build, BuildBetaDetail, BuildBundle, BuildBundleFileSize, BundleId, Capability, Certificate, Credentials, Device, Linkage, LocalizationLevel, Localizations, Page, Platform, PreReleaseVersion, Profile, Relationship, Resource, ReviewSubmission, ReviewSubmissionItem, StorePage, StorePages, VersionLocale } from './types'
// The failure every call throws on a status, and what a client is constructed
// with, from the copy of @greendrake/util this package resolves — so an
// `instanceof` against this export holds whatever copy the consumer has.
export { ApiResponseError, type ClientOptions, type Fetch } from '@greendrake/util'
