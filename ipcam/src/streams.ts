import type { SegmentedOption } from '@greendrake/ui'
import type { CameraState, StreamKey } from './types'

/**
 * Every camera's streams, in the order its tile offers them and its banner names them,
 * with what each is to a viewer: the standard-definition extra one first, which the tile
 * opens on, then the high-definition main one.
 */
export const streams: { key: StreamKey; label: string }[] = [
    { key: 'extra', label: 'SD' },
    { key: 'main', label: 'HD' }
]

/**
 * The streams of the camera name names that can be watched, given its state, each by its
 * key on the stream hub, `<camera>/<stream>`: those the engine casts, as at the camera's
 * last login, or every one while the state tells of none, which the engine serves
 * meanwhile, waiting for the login.
 */
export const watchable = (name: string, state: CameraState | undefined): SegmentedOption[] => streams.filter(({ key }) => state?.Streams?.[key].Cast ?? true).map(({ key, label }) => ({ value: `${name}/${key}`, label }))
