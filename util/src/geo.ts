export interface UserLocation {
    latitude: number
    longitude: number
}

// How long to wait for a fix, and how stale a cached one may be. One decision,
// not two that agree by coincidence, and exported because it is not this
// function's alone: an app that asks a platform for the position instead of the
// browser is producing the same user-visible behaviour and takes the same
// numbers. Without a timeout the W3C default is Infinity — devices that never
// complete a fix (OS location services off, no positioning source) would leave
// the promise hanging forever. A cached fix both speeds up the common case and
// avoids spurious timeouts where a fresh one takes longer than the wait.
export const LOCATION_TIMEOUT_MS = 5000
export const LOCATION_MAX_AGE_MS = 60000

export const getUserLocation = async (): Promise<UserLocation> => {
    const result = await new Promise<GeolocationPosition>((resolve, reject) => navigator.geolocation.getCurrentPosition(resolve, reject, { timeout: LOCATION_TIMEOUT_MS, maximumAge: LOCATION_MAX_AGE_MS }))
    if (!result.coords) {
        throw new Error('Location unknown')
    }
    return {
        latitude: result.coords.latitude,
        longitude: result.coords.longitude
    }
}
