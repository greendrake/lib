<template>
    <div class="Video" :data-state="state">
        <video v-if="supported" ref="videoEl" muted playsinline></video>
        <p v-else class="unsupported">This browser cannot play the camera's H.265 video.</p>
    </div>
</template>
<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { canPlay } from './codec'
import { MSEVideo, type MSEVideoState } from './MSEVideo'

const props = defineProps<{
    // WebSocket URL of the stream hub. Every player pointed at the same one shares a
    // single socket.
    hubUrl: string
    // Which stream on that hub: a key its server knows.
    streamKey: string
    // Whether to play the camera's audio. The element itself stays muted: the audio
    // plays through the page's audio context, which unlockAudio has to be called for,
    // from the click that turns this on.
    audio: boolean
}>()

// A browser that cannot play the stream gets no player at all, and says so.
const supported = canPlay()
const state = ref<MSEVideoState | 'unsupported'>(supported ? 'closed' : 'unsupported')
const videoEl = ref<HTMLVideoElement | null>(null)
let player: MSEVideo | undefined

onMounted(() => {
    if (!supported) {
        return
    }
    player = new MSEVideo(videoEl.value!, props.hubUrl, props.streamKey)
    player.on('state', s => (state.value = s))
    state.value = player.state
    player.setAudio(props.audio)
})
onUnmounted(() => player?.close())
watch(
    () => props.streamKey,
    key => player?.setKey(key)
)
watch(
    () => props.audio,
    on => player?.setAudio(on)
)
</script>
<style scoped>
.Video,
video {
    width: 100%;
    height: 100%;
}
video {
    object-fit: fill;
}
.unsupported {
    margin: 0;
    padding: 1em;
    text-align: center;
}
</style>
