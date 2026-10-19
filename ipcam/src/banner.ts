import { streams } from './streams'
import type { CameraState, StreamKey, StreamState } from './types'

/** What the banner says about the camera, and what it offers to do about it. */
export interface Banner {
    state: 'connecting' | 'online' | 'offline' | 'restart' | 'codec'
    message: string
}

/** The streams the engine does not cast, with how they are encoded, in the order the tile offers them: none before the first login. */
const uncast = ({ Streams: states }: CameraState): [StreamKey, StreamState][] => (states === undefined ? [] : streams.filter(({ key }) => !states[key].Cast).map(({ key }) => [key, states[key]]))

// The first that applies is what the banner says: a restart pending after a codec
// change outranks the codec the camera still reports until it restarts.
const rules: [state: Banner['state'], applies: (camera: CameraState) => boolean, message: (camera: CameraState) => string][] = [
    ['offline', camera => camera.State === 'offline', camera => `The camera at ${camera.Address} is offline${camera.Error ? `: ${camera.Error}` : ''}`],
    ['restart', camera => camera.RestartPending, () => 'The camera takes a setting written only once it restarts.'],
    [
        'codec',
        camera => uncast(camera).length > 0,
        camera => {
            const streams = uncast(camera)
            const which = streams.map(([key, stream]) => `the ${key} stream is on ${stream.Codec}`).join(' and ')
            return `Not played: ${which}. Encode settings switch ${streams.length > 1 ? 'them' : 'it'} to ${camera.Cast}.`
        }
    ],
    ['online', () => true, camera => `Online at ${camera.Address}`]
]

/** The banner for the camera's state; undefined until the backend has told it. */
export const bannerOf = (camera: CameraState | undefined): Banner => {
    if (camera === undefined) {
        return { state: 'connecting', message: 'Connecting…' }
    }
    const [state, , message] = rules.find(([, applies]) => applies(camera))!
    return { state, message: message(camera) }
}
