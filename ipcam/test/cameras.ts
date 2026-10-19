// The camera states the tests read, made once.
import type { CameraState, StreamState } from '../src/types'

export const cast: StreamState = { Codec: 'H.265', Cast: true }
export const uncast: StreamState = { Codec: 'H.264', Cast: false }

/** A camera before its first login: offline, and how it encodes its streams unknown. */
export const undialled: CameraState = {
    State: 'offline',
    Address: '192.0.2.167:34567',
    Error: '',
    Settings: true,
    RestartPending: false,
    Cast: 'H.265'
}

/** The camera online, both its streams cast. */
export const online: CameraState = {
    ...undialled,
    State: 'online',
    Streams: { main: cast, extra: cast }
}
