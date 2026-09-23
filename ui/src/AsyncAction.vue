<script setup lang="ts">
import { computed, ref } from 'vue'

// Interactive button/link for a one-shot async action: label switches between
// dirty/done/in-progress variants, the element disables itself while the
// promise is pending, with an optional spinner.
const props = withDefaults(
    defineProps<{
        promiseMaker: () => Promise<void>
        isDirty?: boolean
        isValid?: boolean
        showSpinner?: boolean
        labelDirty: string
        labelDone?: string
        labelInProgress?: string
        type?: string
        tag?: string
    }>(),
    {
        type: 'button',
        tag: 'button',
        showSpinner: true
    }
)
const spinner = ref(false)
const inProgress = ref(false)
// this is true when the action can be called e.g. when there is unsaved data (as opposed to when it has just been saved and there is nothing new to save yet)
const label = computed(() => {
    if (inProgress.value) {
        return props.labelInProgress || props.labelDirty
    }
    if (!props.isDirty) {
        return props.labelDone || props.labelDirty
    }
    return props.labelDirty
})
const disabled = computed(() => !props.isValid || !props.isDirty || inProgress.value)
const run = async () => {
    if (props.isValid) {
        inProgress.value = true
        if (props.showSpinner) {
            spinner.value = true
        }
        try {
            await props.promiseMaker()
        } finally {
            spinner.value = false
            inProgress.value = false
        }
    }
}
defineExpose({
    run
})
</script>
<template>
    <component :is="tag" class="AsyncAction" :class="{ disabled, spinner }" :type="tag === 'button' ? type : undefined" :href="tag === 'a' ? '#' : undefined" @click.prevent="run">{{ label }}</component>
</template>
<style lang="scss">
@use './shared' as *;
.AsyncAction {
    &.spinner::before {
        @include spinner-inline-right(8px);
    }
}
</style>
