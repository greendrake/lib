import { describe, expect, test } from 'bun:test'
import { followLiveEdge } from '../src/liveEdge'
import { video } from './video'

describe('followLiveEdge', () => {
    test('leaves a video with nothing buffered alone', () => {
        const v = video(5)
        followLiveEdge(v)
        expect(v).toMatchObject({ currentTime: 5, playbackRate: 1 })
    })

    test('speeds up when behind, and slows down once caught up', () => {
        const v = video(5, [0, 6])
        followLiveEdge(v)
        expect(v.playbackRate).toBe(1.1)
        // Between the two thresholds it keeps whatever speed it has.
        v.currentTime = 5.65
        followLiveEdge(v)
        expect(v.playbackRate).toBe(1.1)
        v.currentTime = 5.9
        followLiveEdge(v)
        expect(v.playbackRate).toBe(1)
        v.currentTime = 5.65
        followLiveEdge(v)
        expect(v.playbackRate).toBe(1)
    })

    test('jumps when far behind', () => {
        const v = video(5, [0, 8])
        followLiveEdge(v)
        expect(v.currentTime).toBeCloseTo(7.7)
    })
})
