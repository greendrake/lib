// The helpers that name the document, the window or the navigator. An entry of
// their own rather than part of the main one: a program that type-checks
// without the DOM lib — a bun service reaching this package through a
// dependency — would otherwise have to compile them, and fail to, for helpers
// it never calls.
export { getReadyStatePromise, hasFinePointer, toggleFullscreen } from './dom'
export { getUserLocation, LOCATION_TIMEOUT_MS, LOCATION_MAX_AGE_MS, type UserLocation } from './geo'
