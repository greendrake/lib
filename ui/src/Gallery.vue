<script lang="ts">
export interface GalleryItem {
    url: string
    alt?: string
    // Optional note rendered beneath the image (e.g. a shareable link under a
    // QR code). Travels with the item, so it follows navigation in a multi-image
    // gallery.
    caption?: string
}

const SWIPE_THRESHOLD = 50
</script>
<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
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
const loading = ref(true)

function prev() {
    if (currentIndex.value > 0) {
        loading.value = true
        currentIndex.value--
    }
}

function next() {
    if (currentIndex.value < props.items.length - 1) {
        loading.value = true
        currentIndex.value++
    }
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
        const target = touchStartTarget as HTMLElement | null
        if (target?.tagName !== 'IMG' && !target?.closest('.Gallery__nav')) {
            emit('cancel')
        }
    }
}

const modal = ref<InstanceType<typeof Modal> | null>(null)
onMounted(() => {
    document.addEventListener('keyup', onKeyUp)
    // Touch listeners go on the modal's root element (its own exposure point);
    // they are disposed of together with that element.
    const el = modal.value!.rootEl!
    el.addEventListener('touchstart', onTouchStart)
    el.addEventListener('touchend', onTouchEnd)
})
onUnmounted(() => document.removeEventListener('keyup', onKeyUp))
</script>
<template>
    <Modal ref="modal" class="Gallery Modal--expanded" closable :buttons="false" :main-class="{ spinner: loading }" @cancel="emit('cancel')">
        <img :src="items[currentIndex].url" :alt="items[currentIndex].alt" @load="loading = false" @click="emit('cancel')" />
        <p v-if="items[currentIndex].caption" class="Gallery__caption">{{ items[currentIndex].caption }}</p>
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
    .Modal__main {
        overflow: auto;
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        gap: var(--space-3);
        img {
            display: block;
            // min-height: 0 lets the image shrink within the column so an
            // optional caption below it always stays visible.
            min-height: 0;
            max-width: 100%;
            max-height: 100%;
            object-fit: contain;
        }
    }
    &__caption {
        flex: 0 0 auto;
        margin: 0;
        max-width: 100%;
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
