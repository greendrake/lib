<script lang="ts">
import { arrayRemove, fileMime, humanFileSize } from '@greendrake/util'
import { Uploader } from '../Uploader'

// What the caller's `book` hook resolves to: where the file goes and the
// server-side id the upload will be known by afterwards.
export interface UploadBooking {
    url: string
    verb?: 'PUT' | 'POST'
    fileId: string
}

// A successfully uploaded file, as announced via `uploaded` and recalled via
// `removed`. Both events carry the same object reference, so callers can
// track their collections by identity.
export interface UploadedFile {
    id: string
    name: string
    size: number
}

let nextItemKey = 0
</script>
<script setup lang="ts">
import { markRaw, ref, shallowReactive, watch, type Raw } from 'vue'

// Multi-file uploader: hidden full-area file input (which is natively both the
// click-to-pick and the drop target), per-file progress rows with abort, and
// uploads through the core Uploader. Owner-record concerns (e.g. lazily
// creating the record the files attach to) live entirely inside the caller's
// `book` hook.
//
// Component-scoped CSS custom properties (all with local defaults):
//   --file-upload-bg        drop-target background
//   --uploader-row-bg       file-row caption/delete-button background
//   --uploader-text-color   file-row text color
//   --uploader-bar-color    progress bar fill
//   --uploader-track-color  progress bar track
// Also reads --space-2, with no fallback.

interface FileItem {
    key: number
    name: string
    sizeLabel: string
    progress: number
    // markRaw'd: an XHR-holding state machine must never be proxied (and Raw
    // keeps the template-unwrapped item type assignable back to FileItem).
    up?: Raw<Uploader>
    uploaded?: UploadedFile
    cancelled?: boolean
}

const props = withDefaults(
    defineProps<{
        // Books an upload slot for the file. Rejections surface through the
        // caller's own error pipeline; the pending row is dropped.
        book: (file: File) => Promise<UploadBooking>
        // Content type for the upload — must match what the booked URL was
        // signed for. Default: fileMime (file.type, extension fallback).
        mime?: (file: File) => string
        prompt?: string
        uploadingText?: string
        attachedText?: string
    }>(),
    {
        mime: undefined,
        prompt: 'Click to attach files\nor drop here',
        uploadingText: 'Uploading:',
        attachedText: 'Attached'
    }
)

const emit = defineEmits<{
    uploaded: [file: UploadedFile]
    removed: [file: UploadedFile]
    uploading: [busy: boolean]
}>()

const list = ref<FileItem[]>([])
const fileInput = ref<HTMLInputElement | null>(null)
const inFlight = ref(0)

let wasBusy = false
watch(inFlight, n => {
    const busy = n > 0
    if (busy !== wasBusy) {
        wasBusy = busy
        emit('uploading', busy)
    }
})

const uploadOne = async (file: File, item: FileItem): Promise<void> => {
    try {
        const booking = await props.book(file)
        const up = new Uploader({
            url: booking.url,
            file,
            mime: (props.mime ?? fileMime)(file),
            verb: booking.verb
        })
        item.up = markRaw(up)
        up.on('progress', p => {
            item.progress = p
        })
        await up.send()
        if (item.cancelled) {
            return
        }
        item.progress = 100
        item.uploaded = {
            id: booking.fileId,
            name: file.name,
            size: file.size
        }
        emit('uploaded', item.uploaded)
    } finally {
        inFlight.value--
    }
}

const onFilesPicked = (e: Event): void => {
    const files = (e.target as HTMLInputElement).files
    if (!files) {
        return
    }
    for (const file of files) {
        const item: FileItem = shallowReactive({
            key: ++nextItemKey,
            name: file.name,
            sizeLabel: humanFileSize(file.size),
            progress: 0
        })
        list.value.push(item)
        inFlight.value++
        // Fire and forget — the UI tracks progress via the reactive `item`;
        // a failed booking/upload already surfaced through the caller's error
        // pipeline, so here the stuck row just goes away.
        void uploadOne(file, item).catch(() => arrayRemove(list.value, item))
    }
}

const remove = (item: FileItem): void => {
    if (item.up && !item.uploaded) {
        item.cancelled = true
        item.up.abort()
    }
    arrayRemove(list.value, item)
    if (item.uploaded) {
        emit('removed', item.uploaded)
    }
    // Reset so re-picking the very same file fires `change` again.
    if (fileInput.value) {
        fileInput.value.value = ''
    }
}
</script>
<template>
    <div class="FileUploader">
        <div v-if="list.length" class="FileUploader__List">
            <div v-for="f in list" :key="f.key" class="FileUploader__File">
                <div>
                    <div class="FileUploader__File__Name">
                        <span>{{ f.name }}</span>
                        <span>{{ f.sizeLabel }}</span>
                    </div>
                    <div>
                        <div class="FileUploader__PB" :style="{ width: f.progress + '%' }"></div>
                        <div class="FileUploader__PI" :class="{ finished: f.progress === 100 }">{{ f.progress === 100 ? attachedText : `${uploadingText} ${f.progress}%` }}</div>
                    </div>
                </div>
                <div @click="remove(f)" />
            </div>
        </div>
        <div class="FileUploader__uploadFile">
            <div class="FileUploader__prompt">{{ prompt }}</div>
            <input ref="fileInput" type="file" multiple @change="onFilesPicked" />
        </div>
    </div>
</template>
<style lang="scss">
@use '@greendrake/scss-kit' as kit;

$row-bg-default: linear-gradient(0deg, rgb(171 171 171) 0%, rgb(191 191 191) 50%, rgb(230 230 230) 100%);
$text-default: #444;

.FileUploader {
    &__PI {
        position: absolute;
        padding: 0 6px;
        top: 0;
        left: 0;
        bottom: 0;
        right: 0;
        z-index: kit.z('spinner');
        color: var(--uploader-text-color, #{$text-default});
        font-size: 12px;
        line-height: 14px;
    }
    &__PB {
        position: absolute;
        z-index: kit.z('raised');
        top: 0;
        left: 0;
        bottom: 0;
        background-color: var(--uploader-bar-color, #caeeca);
    }
    &__File {
        display: flex;
        justify-content: space-between;
        border-radius: 2px;
        box-shadow: 1px 1px 2px #aaa;
        &:not(:first-child) {
            margin-top: var(--space-2);
        }
        &__Name {
            background: var(--uploader-row-bg, #{$row-bg-default});
        }
        > div {
            &:first-child {
                flex-grow: 1;
                > div {
                    span {
                        color: var(--uploader-text-color, #{$text-default});
                    }
                    &:first-child {
                        padding: 0 6px;
                        display: flex;
                        justify-content: space-between;
                    }
                    &:last-child {
                        height: 16px;
                        position: relative;
                        background-color: var(--uploader-track-color, #fff4c8);
                    }
                }
            }
            &:last-child {
                --icon-size: 24px;
                background: var(--uploader-row-bg, #{$row-bg-default});
                width: 40px;
                text-align: center;
                margin-inline-start: 1px;
                position: relative;
                cursor: default;
                display: flex;
                justify-content: center;
                align-items: center;
                &::before {
                    content: '';
                    opacity: 0.5;
                    @include kit.icon;
                    @include kit.i('delete');
                }
                &:hover {
                    &::before {
                        opacity: 1;
                    }
                }
            }
        }
    }
    &__List {
        margin-bottom: var(--space-2);
    }
    &__prompt {
        color: #fff;
        text-align: center;
        white-space: pre-line;
    }
    &__uploadFile {
        border-radius: 2px;
        display: flex;
        align-items: center;
        justify-content: center;
        margin: 0 0 var(--space-2) 0;
        position: relative;
        width: 100%;
        height: 60px;
        font-size: 14px;
        line-height: 16px;
        background: var(--file-upload-bg, #999);
        input {
            position: absolute;
            z-index: kit.z('spinner');
            width: 100%;
            height: 100%;
            opacity: 0;
            cursor: default;
        }
    }
}
</style>
