export { serviceAccountBearer, type Bearer, type ServiceAccountKey } from './auth'
export { createSearchConsole, type DimensionFilter, type RichResultsResult, type SearchAnalyticsQuery, type SearchAnalyticsRow, type SearchConsole, type SearchConsoleSite, type SitemapEntry, type UrlInspectionResult } from './search-console'
// The round trip every call makes and the failure it throws on a status, from
// the copy of @greendrake/util this package resolves — so an `instanceof`
// against this export holds whatever copy the consumer has, and a client of
// another Google API is built over a bearer with the same `bearerRequest`.
export { ApiResponseError, bearerRequest, type ApiRequestInit, type ClientOptions, type Fetch } from '@greendrake/util'
