<script setup lang="ts">
import { computed } from 'vue'
import { bannerOf } from './banner'
import RebootButton from './RebootButton.vue'
import { ENCODE_GROUP, type CameraState } from './types'

const props = defineProps<{
    // Undefined until the engine has told it.
    state: CameraState | undefined
}>()

defineEmits<{
    // The banner offers a group of the camera's settings: the encoding ones.
    openGroup: [group: string]
}>()

const banner = computed(() => bannerOf(props.state))
</script>
<template>
    <div class="StateBanner" :data-state="banner.state" role="status">
        <span>{{ banner.message }}</span>
        <RebootButton v-if="banner.state === 'restart'" />
        <button v-if="banner.state === 'codec'" type="button" @click="$emit('openGroup', ENCODE_GROUP)">Encode settings</button>
    </div>
</template>
<style lang="scss">
.StateBanner {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-4);
    border-bottom: 1px solid var(--border-color-light);
    border-left: 4px solid transparent;
    &[data-state='offline'],
    &[data-state='codec'] {
        border-left-color: var(--error-color);
    }
    &[data-state='restart'] {
        border-left-color: var(--warning-color);
    }
}
</style>
