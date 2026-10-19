// Keeping a live stream's playback close to the newest frame it has. The server times
// frames on the camera's own clock, so playback keeps pace with the camera, and the lag
// a viewer starts with or picks up from a stall would otherwise stay.

// In seconds of video buffered ahead of what is on screen: past CATCH_UP_LEAD playback
// speeds up to CATCH_UP_RATE until the lead is back under CAUGHT_UP_LEAD; past
// JUMP_LEAD it jumps to JUMP_TO_LEAD behind the newest frame.
const CATCH_UP_LEAD = 0.5
const CAUGHT_UP_LEAD = 0.2
const CATCH_UP_RATE = 1.1
const JUMP_LEAD = 2.5
const JUMP_TO_LEAD = 0.3

export type LiveVideo = Pick<HTMLVideoElement, 'buffered' | 'currentTime' | 'playbackRate'>

// Steers playback towards the live edge; meant to run on every timeupdate and
// progress event.
export const followLiveEdge = (video: LiveVideo): void => {
    const buffered = video.buffered
    if (buffered.length === 0) {
        return
    }
    const end = buffered.end(buffered.length - 1)
    const lead = end - video.currentTime
    if (lead > JUMP_LEAD) {
        video.currentTime = end - JUMP_TO_LEAD
    } else if (lead > CATCH_UP_LEAD) {
        video.playbackRate = CATCH_UP_RATE
    } else if (lead < CAUGHT_UP_LEAD) {
        video.playbackRate = 1
    }
}
