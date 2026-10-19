import { describe, expect, test } from 'bun:test'
import { bannerOf } from '../src/banner'
import type { CameraState } from '../src/types'
import { cast, online, uncast, undialled } from './cameras'

describe('the banner', () => {
    test('says it is connecting until the engine has said anything', () => {
        expect(bannerOf(undefined)).toEqual({ state: 'connecting', message: 'Connecting…' })
    })
    test('says the camera is offline before it has been dialled', () => {
        expect(bannerOf(undialled)).toEqual({ state: 'offline', message: 'The camera at 192.0.2.167:34567 is offline' })
    })
    test('says where the camera is while all is well', () => {
        expect(bannerOf(online)).toEqual({ state: 'online', message: 'Online at 192.0.2.167:34567' })
    })
    test('says why the camera is offline', () => {
        const offline: CameraState = {
            ...online,
            State: 'offline',
            Error: 'connection refused',
            RestartPending: true
        }
        expect(bannerOf(offline)).toEqual({ state: 'offline', message: 'The camera at 192.0.2.167:34567 is offline: connection refused' })
    })
    test('asks for a restart a write left pending, ahead of the codec it changed', () => {
        const pending: CameraState = {
            ...online,
            RestartPending: true,
            Streams: { main: uncast, extra: cast }
        }
        expect(bannerOf(pending)).toEqual({ state: 'restart', message: 'The camera takes a setting written only once it restarts.' })
    })
    test('names a stream the engine does not cast, and the codec it casts', () => {
        expect(bannerOf({ ...online, Streams: { main: cast, extra: uncast } })).toEqual({
            state: 'codec',
            message: 'Not played: the extra stream is on H.264. Encode settings switch it to H.265.'
        })
    })
    test('and both, when both are', () => {
        expect(bannerOf({ ...online, Streams: { main: uncast, extra: uncast } }).message).toBe('Not played: the extra stream is on H.264 and the main stream is on H.264. Encode settings switch them to H.265.')
    })
})
