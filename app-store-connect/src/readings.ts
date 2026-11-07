// What several calls answer together, as plain records: an app's record, and
// its App Store pages.
import type { Endpoints } from './client'
import type { AppInfoRecord, AppRecord, Platform, StorePage, StorePages } from './types'

// The state both an App Store version and the app information are in while on
// sale.
const ON_SALE = 'READY_FOR_DISTRIBUTION'

export const readings = (api: Endpoints) => {
    // Each app-information record with its localizations by locale.
    const appInfoRecords = async (appKey: string): Promise<AppInfoRecord[]> =>
        Promise.all(
            (await api.listAppInfos(appKey)).map(async info => ({
                id: info.id,
                state: info.attributes.state,
                locales: Object.fromEntries(
                    (await api.listLocalizations('appInfoLocalizations', info.id)).map(localization => [
                        localization.attributes.locale,
                        {
                            id: localization.id,
                            name: localization.attributes.name,
                            subtitle: localization.attributes.subtitle
                        }
                    ])
                )
            }))
        )

    return {
        // Whether App Store Connect knows the app at all — null when it holds no
        // record — and what it has: the record, its app information in each
        // state with the app's name and subtitle per locale, and the newest
        // `recentBuilds` builds it has received.
        appRecord: async (bundleId: string, recentBuilds: number): Promise<AppRecord | null> => {
            const app = await api.findApp(bundleId)
            if (!app) return null
            const [appInfos, builds] = await Promise.all([appInfoRecords(app.id), api.listBuilds(app.id)])
            const { name, sku, primaryLocale } = app.attributes
            return {
                app: {
                    id: app.id,
                    name,
                    sku,
                    primaryLocale
                },
                appInfos,
                builds: builds.slice(0, recentBuilds).map(({ id, attributes: { version, uploadedDate, processingState, expired }, train }) => ({
                    id,
                    version,
                    uploadedDate,
                    processingState,
                    expired,
                    train
                }))
            }
        },

        // An app's App Store pages on a platform, both levels joined by locale —
        // null when App Store Connect holds no record. The page on sale, if
        // any, and the newest version's when that is another one, whatever its
        // state: being prepared, rejected, in review, or approved and held for
        // a manual release.
        //
        // An app holds at most two app-information records — the one on sale
        // and, while a version is in flight, the one going with it — so the
        // page on sale takes the first and the other page the second. Anything
        // else is not a shape the API is known to take, and is an error naming
        // what was found.
        storePages: async (bundleId: string, platform: Platform): Promise<StorePages | null> => {
            const app = await api.findApp(bundleId)
            if (!app) return null
            const [appInfos, versions] = await Promise.all([appInfoRecords(app.id), api.listAppStoreVersions(app.id, platform)])
            const onSale = versions.find(version => version.attributes.appVersionState === ON_SALE)
            const newest = versions.reduce<(typeof versions)[number] | undefined>((found, version) => (found && Date.parse(found.attributes.createdDate) >= Date.parse(version.attributes.createdDate) ? found : version), undefined)
            const shown = [...new Set([onSale, newest])].filter(version => version !== undefined)

            const pages = await Promise.all(
                shown.map(async (version): Promise<StorePage> => {
                    const live = version === onSale
                    const { versionString, appVersionState, releaseType, createdDate } = version.attributes
                    const [info, ...more] = appInfos.filter(candidate => (candidate.state === ON_SALE) === live)
                    if (!info || more.length) {
                        throw new Error(`${bundleId} ${versionString} (${appVersionState}) pairs with no single app information ${live ? 'on sale' : 'in flight'}; ` + `the app's information is in ${appInfos.map(candidate => candidate.state).join(', ') || 'no state'}`)
                    }
                    const localizations = await api.listLocalizations('appStoreVersionLocalizations', version.id)
                    const byLocale = new Map(
                        localizations.map(({ id, attributes: { locale, description, keywords, promotionalText, whatsNew } }) => [
                            locale,
                            {
                                id,
                                description,
                                keywords,
                                promotionalText,
                                whatsNew
                            }
                        ])
                    )
                    const locales = [...new Set([...Object.keys(info.locales), ...byLocale.keys()])]
                    return {
                        version: {
                            id: version.id,
                            versionString,
                            appVersionState,
                            releaseType,
                            createdDate
                        },
                        appInfo: { id: info.id, state: info.state },
                        locales: Object.fromEntries(locales.map(locale => [locale, { appInfo: info.locales[locale] ?? null, version: byLocale.get(locale) ?? null }]))
                    }
                })
            )
            return { app: { id: app.id, primaryLocale: app.attributes.primaryLocale }, pages }
        }
    }
}
