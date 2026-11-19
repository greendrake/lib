<script lang="ts">
export interface GalleryItem {
    url: string
    alt?: string
    // Optional note rendered beneath the image (e.g. a shareable link under a
    // QR code). Travels with the item, so it follows navigation in a multi-image
    // gallery.
    caption?: string
    /** A video, played with controls; an image otherwise. */
    video?: boolean
}

const SWIPE_THRESHOLD = 50
</script>
<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref } from 'vue'
import Modal from './Modal.vue'

const props = withDefaults(
    defineProps<{
        items: GalleryItem[]
        startIndex?: number
    }>(),
    { startIndex: 0 }
)

const emit = defineEmits<{
    cancel: []
}>()

const currentIndex = ref(props.startIndex)
const current = computed(() => props.items[currentIndex.value])
const loading = ref(true)

// The size of the window around the current item at the item's own size:
// measured once the item has loaded, the window laid out so for it
// (`measuring`); null until then.
const ownSize = ref<{ width: number; height: number } | null>(null)
const measuring = ref(false)
const viewport = reactive({ width: window.innerWidth, height: window.innerHeight })
const onResize = () => Object.assign(viewport, { width: window.innerWidth, height: window.innerHeight })

// The item at its own size, the window just big enough to house it, where the
// browser window houses that; else, as while it loads, the window filling the
// browser window and the item scaled down to fit in it.
const ownSized = computed(() => measuring.value || (ownSize.value !== null && ownSize.value.width <= viewport.width && ownSize.value.height <= viewport.height))

async function onLoaded() {
    loading.value = false
    measuring.value = true
    await nextTick()
    const { width, height } = modal.value!.windowEl!.getBoundingClientRect()
    ownSize.value = { width, height }
    measuring.value = false
}

function show(index: number) {
    loading.value = true
    ownSize.value = null
    currentIndex.value = index
}

function prev() {
    if (currentIndex.value > 0) show(currentIndex.value - 1)
}

function next() {
    if (currentIndex.value < props.items.length - 1) show(currentIndex.value + 1)
}

const onKeyUp = (e: KeyboardEvent) => {
    if (e.key === 'ArrowLeft') prev()
    else if (e.key === 'ArrowRight') next()
}

let touchStartX = 0
let touchStartY = 0
let touchStartTarget: EventTarget | null = null
let multiTouch = false

function onTouchStart(e: TouchEvent) {
    // A second finger turns the gesture into a pinch/zoom: hand it entirely to the
    // browser and stop tracking, so the following touchend neither navigates nor closes.
    if (e.touches.length > 1) {
        multiTouch = true
        return
    }
    touchStartX = e.touches[0].clientX
    touchStartY = e.touches[0].clientY
    touchStartTarget = e.target
}

function onTouchEnd(e: TouchEvent) {
    if (multiTouch) {
        // Ignore every touchend of a pinch/zoom until its last finger lifts.
        if (e.touches.length === 0) multiTouch = false
        return
    }
    const dx = e.changedTouches[0].clientX - touchStartX
    const dy = e.changedTouches[0].clientY - touchStartY
    const isSwipe = Math.abs(dx) >= SWIPE_THRESHOLD && Math.abs(dx) > Math.abs(dy)
    if (isSwipe) {
        if (dx < 0) {
            if (currentIndex.value < props.items.length - 1) next()
            else emit('cancel')
        } else {
            if (currentIndex.value > 0) prev()
            else emit('cancel')
        }
    } else if (Math.abs(dx) < SWIPE_THRESHOLD && Math.abs(dy) < SWIPE_THRESHOLD) {
        // A tap on the item itself (a video's controls among it) keeps the gallery open.
        const target = touchStartTarget as HTMLElement | null
        if (target?.tagName !== 'IMG' && target?.tagName !== 'VIDEO' && !target?.closest('.Gallery__nav')) {
            emit('cancel')
        }
    }
}

const modal = ref<InstanceType<typeof Modal> | null>(null)
onMounted(() => {
    document.addEventListener('keyup', onKeyUp)
    window.addEventListener('resize', onResize)
    // Touch listeners go on the modal's root element (its own exposure point);
    // they are disposed of together with that element.
    const el = modal.value!.rootEl!
    el.addEventListener('touchstart', onTouchStart)
    el.addEventListener('touchend', onTouchEnd)
})
onUnmounted(() => {
    document.removeEventListener('keyup', onKeyUp)
    window.removeEventListener('resize', onResize)
})
</script>
<template>
    <Modal ref="modal" class="Gallery" :class="ownSized ? 'Gallery--own-size' : 'Modal--expanded'" closable :buttons="false" :main-class="{ spinner: loading }" @cancel="emit('cancel')">
        <video v-if="current.video" :src="current.url" :aria-label="current.alt" controls autoplay @loadedmetadata="onLoaded" />
        <img v-else :src="current.url" :alt="current.alt" @load="onLoaded" @click="emit('cancel')" />
        <p v-if="current.caption" class="Gallery__caption">{{ current.caption }}</p>
        <button v-if="currentIndex > 0" class="Gallery__nav Gallery__nav--prev" @click="prev" />
        <button v-if="currentIndex < items.length - 1" class="Gallery__nav Gallery__nav--next" @click="next" />
    </Modal>
</template>
<style lang="scss">
@use '@greendrake/scss-kit' as *;
.Gallery {
    .Modal__toolbar-btn {
        width: 18px;
        height: 18px;
        opacity: 0.8;
        &:hover {
            opacity: 1;
        }
    }
    // The item edge to edge, the window its frame: specific enough to override
    // Modal's padding.
    &.Modal .Modal__main {
        padding: 0;
        overflow: auto;
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        img,
        video {
            display: block;
            // min-height: 0 lets the item shrink within the column so an
            // optional caption below it always stays visible.
            min-height: 0;
            max-width: 100%;
            max-height: 100%;
            object-fit: contain;
        }
    }
    // The window around the item at its own size, centred however narrow the
    // browser window, whose bottom sheet would otherwise stretch it: specific
    // enough to override Modal's own rules.
    &.Gallery--own-size.Modal {
        align-items: center;
        .Modal__window {
            width: auto;
            max-width: none;
            max-height: none;
            overflow: visible;
            border-radius: var(--modal-border-radius);
            padding-bottom: 0;
        }
        .Modal__main {
            overflow: visible;
            img,
            video {
                max-width: none;
                max-height: none;
            }
        }
    }
    &__caption {
        flex: 0 0 auto;
        // Takes the item's width, adding none to the window's: a long one wraps
        // rather than widens it.
        width: 0;
        min-width: 100%;
        margin: 0;
        padding: var(--space-2) var(--space-3);
        text-align: center;
        // URLs and other unbroken strings must wrap rather than overflow.
        word-break: break-word;
        opacity: 0.85;
    }
    &__nav {
        @include icon;
        position: absolute;
        top: 50%;
        transform: translateY(-50%);
        width: 36px;
        height: 36px;
        cursor: default;
        padding: 0;
        background-color: var(--font-color);
        border-radius: 50%;
        opacity: 0.8;
        &:hover {
            opacity: 1;
        }
        &--prev {
            left: 8px;
            @include i('chevron-left');
        }
        &--next {
            right: 8px;
            @include i('chevron-right');
        }
    }
}
</style>
