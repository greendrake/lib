import type { LiveVideo } from '../src/liveEdge'

// A video showing currentTime, with the given ranges buffered.
export const video = (currentTime: number, ...ranges: [start: number, end: number][]): LiveVideo => ({
    currentTime,
    playbackRate: 1,
    buffered: {
        length: ranges.length,
        start: index => ranges[index][0],
        end: index => ranges[index][1]
    }
})
