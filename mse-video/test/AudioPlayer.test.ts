import { describe, expect, test } from 'bun:test'
import { AudioPlayer, type AudioClock, type AudioFrame, type AudioOutput } from '../src/AudioPlayer'

class FakeVideo extends EventTarget implements AudioClock {
    currentTime = 10
    paused = false
    seeking = false
    readyState = 4
}

interface Scheduled {
    at: number
    samples: Float32Array
    sampleRate: number
    stopped: boolean
}

class FakeOutput implements AudioOutput {
    currentTime = 100
    readonly scheduled: Scheduled[] = []

    schedule(samples: Float32Array<ArrayBuffer>, sampleRate: number, at: number, ended: () => void): () => void {
        const entry: Scheduled = {
            at,
            samples,
            sampleRate,
            stopped: false
        }
        this.scheduled.push(entry)
        return () => {
            entry.stopped = true
            ended()
        }
    }

    starts(): number[] {
        return this.scheduled.map(entry => Math.round(entry.at * 1000) / 1000)
    }
}

// 20 ms of 8 kHz A-law silence, due at ptsMs on the video's timeline.
const frame = (ptsMs: number): AudioFrame => ({
    ptsMs,
    sampleRate: 8000,
    codec: 'alaw',
    samples: new Uint8Array(160).fill(0xd5)
})

const setUp = (): { video: FakeVideo; output: FakeOutput; player: AudioPlayer } => {
    const video = new FakeVideo()
    const output = new FakeOutput()
    return {
        video,
        output,
        player: new AudioPlayer(video, output)
    }
}

describe('AudioPlayer', () => {
    test('schedules a frame for when the video reaches it, and the next back to back', () => {
        const { output, player } = setUp()
        // The video shows 10 s while the output is at 100 s: 10.1 s is due at 100.1 s.
        player.push(frame(10100))
        player.push(frame(10120))
        // Off by 10 ms, which is within reach: still back to back.
        player.push(frame(10150))
        expect(output.starts()).toEqual([100.1, 100.12, 100.14])
        expect(output.scheduled[0].sampleRate).toBe(8000)
        expect(output.scheduled[0].samples[0] * 32768).toBe(8)
    })

    test('drops a frame the video has already passed', () => {
        const { output, player } = setUp()
        player.push(frame(9900))
        expect(output.scheduled).toEqual([])
    })

    test('leaves a gap when the video is due further on', () => {
        const { output, player } = setUp()
        player.push(frame(10100))
        player.push(frame(10300))
        expect(output.starts()).toEqual([100.1, 100.3])
        expect(output.scheduled[0].stopped).toBe(false)
    })

    test('drops what is queued when the video has moved past it', () => {
        const { video, output, player } = setUp()
        player.push(frame(10100))
        player.push(frame(10120))
        // The queue ends at 100.14 s; this one is due at 100.05 s.
        video.currentTime = 10.55
        player.push(frame(10600))
        expect(output.starts()).toEqual([100.1, 100.12, 100.05])
        expect(output.scheduled.map(entry => entry.stopped)).toEqual([true, true, false])
    })

    test('plays only while the video does', () => {
        const video = new FakeVideo()
        video.paused = true
        const output = new FakeOutput()
        const player = new AudioPlayer(video, output)
        player.push(frame(10100))
        expect(output.scheduled).toEqual([])
        video.dispatchEvent(new Event('playing'))
        player.push(frame(10100))
        expect(output.scheduled.length).toBe(1)
        for (const halt of ['waiting', 'pause', 'seeking', 'emptied']) {
            video.dispatchEvent(new Event('playing'))
            player.push(frame(10200))
            video.dispatchEvent(new Event(halt))
            expect(output.scheduled.every(entry => entry.stopped)).toBe(true)
            player.push(frame(10300))
            expect(output.scheduled.at(-1)!.at).toBe(100.2)
        }
    })

    test('a video still buffering at the start waits for playing', () => {
        const video = new FakeVideo()
        video.readyState = 2
        const output = new FakeOutput()
        new AudioPlayer(video, output).push(frame(10100))
        expect(output.scheduled).toEqual([])
    })

    test('close silences the queue and stops listening', () => {
        const { video, output, player } = setUp()
        player.push(frame(10100))
        player.close()
        expect(output.scheduled[0].stopped).toBe(true)
        video.dispatchEvent(new Event('waiting'))
        player.push(frame(10200))
        // Still running, as no halt reached a closed player.
        expect(output.scheduled.length).toBe(2)
    })
})
