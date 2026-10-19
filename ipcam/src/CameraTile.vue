<script setup lang="ts">
// A camera's tile: its picture, which a click has fill the screen, and a menu of its
// sound, of its stream where it has more than one to watch, and, once its state says it
// has settings, of the item that opens them.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { unlockAudio } from '@greendrake/mse-video'
import { Video } from '@greendrake/mse-video/vue'
import { MenuButton, ModalOnClick, SegmentedControl, SwitchRow } from '@greendrake/ui'
import { toggleFullscreen } from '@greendrake/util/browser'
import CameraSettings from './CameraSettings.vue'
import StateBanner from './StateBanner.vue'
import { watchable } from './streams'
import type { CameraState } from './types'

const props = defineProps<{
    // WebSocket URL of the stream hub.
    hubUrl: string
    // The camera's name, which keys its streams on the hub.
    name: string
    // Undefined until the engine has told it.
    state: CameraState | undefined
    // Whether the picture keeps its aspect ratio, letterboxed, rather than stretching to
    // the tile: for a tile the host does not size to the camera's 16:9.
    contain?: boolean
}>()

const offered = computed(() => watchable(props.name, props.state))
// The stream picked, by its key, kept while it cannot be watched: the first that can
// shows meanwhile. None shows with none to watch.
const picked = ref<string>()
const shown = computed(() => (offered.value.find(o => o.value === picked.value) ?? offered.value[0])?.value)

const audio = ref(false)
const onSound = (on: boolean): void => {
    // Browsers let audio start only from within a user gesture.
    if (on) {
        unlockAudio()
    }
    audio.value = on
}

const wrapperEl = ref<HTMLElement | null>(null)
// Stands in for fullscreen only where the browser has none to give (iPhone Safari,
// whose native video player would drop both the menu and the app around it). Where the
// real thing is available it is used untouched: the browser sizes the tile to the screen
// and the video fills it, as it fills its grid cell the rest of the time.
const substituteFullscreen = ref(false)
const onPictureClick = () => {
    if (!toggleFullscreen(wrapperEl.value!)) {
        substituteFullscreen.value = !substituteFullscreen.value
    }
}
const nativeFullscreen = ref(false)
const onFullscreenChange = () => {
    nativeFullscreen.value = document.fullscreenElement === wrapperEl.value
}
onMounted(() => {
    document.addEventListener('fullscreenchange', onFullscreenChange)
})
onBeforeUnmount(() => {
    document.removeEventListener('fullscreenchange', onFullscreenChange)
})
// The settings go while the tile fills the screen: what they open, a modal on the body,
// would not show over native fullscreen.
const filling = computed(() => nativeFullscreen.value || substituteFullscreen.value)

// The group of settings shown, kept while the settings are closed.
const group = ref<string>()
</script>
<template>
    <div ref="wrapperEl" class="CameraTile" :class="{ 'CameraTile--fullscreen': substituteFullscreen, 'CameraTile--contain': contain || substituteFullscreen }">
        <div class="CameraTile__frame">
            <MenuButton class="CameraTile__menu" menu-class="CameraTile__items">
                <template #main="{ open }"><button type="button" class="CameraTile__trigger" aria-haspopup="menu" :aria-expanded="open" aria-label="Menu"></button></template>
                <template #menuitems>
                    <SwitchRow :model-value="audio" label="Sound" @update:model-value="onSound" />
                    <div v-if="shown && offered.length > 1">
                        <span>Stream</span>
                        <SegmentedControl :model-value="shown" :options="offered" ariaLabel="Stream" @update:model-value="key => (picked = key)" />
                    </div>
                    <ModalOnClick v-if="!filling && state?.Settings" tag="button" type="button" :title="name" :buttons="false" modal-class="CameraTile__settings">
                        Settings
                        <template #content>
                            <StateBanner :state="state" @open-group="g => (group = g)" />
                            <CameraSettings :key="name" v-model:group="group" :state="state" />
                        </template>
                    </ModalOnClick>
                </template>
            </MenuButton>
            <div v-if="shown" class="CameraTile__picture" @click="onPictureClick">
                <Video :hub-url="hubUrl" :stream-key="shown" :audio="audio" />
            </div>
        </div>
    </div>
</template>
<style lang="scss">
@use 'sass:meta';
@use '@greendrake/scss-kit' as kit;

// The host sizes the tile; the frame inside fills it, or the viewport while it stands in
// for fullscreen, which leaves the tile's own box to the host whatever sizes it.
.CameraTile {
    position: relative;
    // Keeps the menu button's z-index inside the tile: in the root stacking context it
    // would paint over whichever sibling tile is standing in for fullscreen.
    isolation: isolate;
    // Over the tiles after it while it stands in for fullscreen, and below a modal.
    &--fullscreen {
        z-index: kit.z('sticky');
    }
    &__frame {
        position: absolute;
        inset: 0;
    }
    &--fullscreen &__frame {
        position: fixed;
    }
    // The viewport, or a box the host does not size to 16:9, rarely has the camera's
    // aspect ratio, unlike the 16:9 grid cells the stretched default is meant for.
    &--contain &__frame {
        background-color: black;
    }
    &--contain &__picture video {
        object-fit: contain;
    }
    &__picture {
        width: 100%;
        height: 100%;
    }
    // Over the picture, within the tile's own stacking context.
    &__menu {
        position: absolute;
        z-index: kit.z('raised');
        top: 10px;
        right: 10px;
    }
    // Faint over the picture until pointed at, or while its menu shows.
    &__trigger {
        display: flex;
        align-items: center;
        justify-content: center;
        width: 30px;
        height: 30px;
        padding: 0;
        border: 0;
        background-color: white;
        opacity: 0.2;
        &:hover,
        &[aria-expanded='true'] {
            opacity: 1;
        }
        &::before {
            content: '';
            @include kit.icon-base;
            @include kit.i('menu');
            mask-position: center;
            background-color: black;
            width: 18px;
            height: 18px;
        }
    }
    // The menu, which is not in the tile but on the body, or on the tile while it fills
    // the screen. Each item is a row, Settings too: what it is at the start, any control
    // of it at the end.
    &__items {
        display: flex;
        flex-direction: column;
        padding: var(--space-1) 0;
        background-color: var(--form-background);
        border: 1px solid var(--border-color-light);
        border-radius: var(--border-radius);
        > * {
            display: flex;
            align-items: center;
            justify-content: space-between;
            gap: var(--space-3);
            padding: var(--space-2) var(--space-3);
            border: 0;
            background: none;
            color: var(--font-color);
            font: inherit;
            text-align: start;
        }
        > button:hover {
            background-color: var(--form-background-hover);
        }
    }
    // The settings' modal, which is on the body. Modal shrinks to its content, in which
    // the settings' grid would be a single column: the window takes the viewport's width
    // instead, up to what three of SettingsSection's 240 px columns take, with the gaps
    // between them and the padding round them. Its height is not the content's either,
    // which changes with the group shown: 60% of the viewport's, but no less than 400 px
    // unless the viewport is less.
    &__settings .Modal__window {
        width: min(100vw, calc(3 * 240px + 2 * var(--space-3) + 4 * var(--space-4)));
        height: min(100dvh, max(400px, 60dvh));
    }
    // The banner on top, and under it the settings, which fill the rest, their group's
    // section scrolling under its tab strip.
    &__settings .Modal__main {
        @include kit.column-filled-by-last;
    }
}

// The settings' native controls take the dashboard's skin, whatever skin the host gives
// its own. Scoped with no weight of its own, so that what a control styles itself with
// outranks it, as it outranks the bare skin.
:where(.CameraTile__settings) {
    @include meta.load-css('@greendrake/theme/form-controls');
}
</style>
