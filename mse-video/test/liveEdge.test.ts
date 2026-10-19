import { describe, expect, test } from 'bun:test'
import { followLiveEdge, type LiveVideo } from '../src/liveEdge'

// A video showing currentTime with video buffered up to end.
const video = (currentTime: number, end?: number): LiveVideo => ({
    currentTime,
    playbackRate: 1,
    buffered: {
        length: end === undefined ? 0 : 1,
        start: () => 0,
        end: () => end!
    }
})

describe('followLiveEdge', () => {
    test('leaves a video with nothing buffered alone', () => {
        const v = video(5)
        followLiveEdge(v)
        expect(v).toMatchObject({ currentTime: 5, playbackRate: 1 })
    })

    test('speeds up when behind, and slows down once caught up', () => {
        const v = video(5, 6)
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
        const v = video(5, 8)
        followLiveEdge(v)
        expect(v.currentTime).toBeCloseTo(7.7)
    })
})
