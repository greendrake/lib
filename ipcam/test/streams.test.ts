import { describe, expect, test } from 'bun:test'
import { watchable } from '../src/streams'
import type { CameraState } from '../src/types'
import { cast, online, uncast, undialled } from './cameras'

// The keys of the streams of the camera named gate that can be watched.
const watched = (state: CameraState | undefined): string[] => watchable('gate', state).map(s => s.value)

describe('the streams watched', () => {
    test('are both while the engine casts both, keyed by the camera’s name, standard definition first', () => {
        expect(watchable('gate', online)).toEqual([
            { value: 'gate/extra', label: 'SD' },
            { value: 'gate/main', label: 'HD' }
        ])
    })
    test('are those the engine casts', () => {
        expect(watched({ ...online, Streams: { main: cast, extra: uncast } })).toEqual(['gate/main'])
        expect(watched({ ...online, Streams: { main: uncast, extra: cast } })).toEqual(['gate/extra'])
        expect(watched({ ...online, Streams: { main: uncast, extra: uncast } })).toEqual([])
    })
    test('are both until the engine has told of the camera, and of its streams at a login', () => {
        expect(watched(undefined)).toEqual(['gate/extra', 'gate/main'])
        expect(watched(undialled)).toEqual(['gate/extra', 'gate/main'])
    })
})
