<script lang="ts">
import type { Ref } from 'vue'

export interface FormRefs {
    dirty?: Ref<boolean>
    valid?: Ref<boolean>
}
</script>
<script setup lang="ts">
import { computed, onUnmounted } from 'vue'
import Form from './Form'

// A <form> whose submit button doubles as the submission status display,
// walking the Form state machine: dirty → (confirm →) inprogress →
// success/error → back to rest. The *Text props label the button per state;
// missing ones fall back to the raw state name (a dev-time default — real
// consumers pass their own labels).
const props = defineProps<{
    savedText?: string
    successText?: string
    inprogressText?: string
    dirtyText?: string
    invalidText?: string
    confirmText?: string
    errorText?: string
    action?: () => Promise<unknown> | unknown
    askToConfirm?: boolean
    refs?: FormRefs
    // Render an empty button (status conveyed by CSS classes only).
    noContent?: boolean
    tryPersistently?: boolean
    // true/undefined → default success display duration; a number → that many
    // ms; false → skip the success state entirely.
    successTimeout?: boolean | number
}>()

const form = new Form({
    requireConfirmation: props.askToConfirm,
    handler: async () => {
        await props.action?.()
    },
    dirty: props.refs?.dirty,
    valid: props.refs?.valid,
    successTimeout: props.successTimeout === true ? undefined : props.successTimeout,
    tryPersistently: props.tryPersistently
})
const content = computed(() => {
    if (form.statusInfo.value) {
        return form.statusInfo.value
    }
    if (props.noContent) {
        return ''
    }
    return props[`${form.state.value}Text` as const] || form.state.value
})
const computeCls = (baseCls: string): string => {
    let str = `${baseCls} ${baseCls}-${form.state.value}`
    if (form.statusInfo.value) {
        str += ` ${baseCls}-error`
    }
    return str
}
const fCls = computed(() => computeCls('form'))
const bCls = computed(() => computeCls('button'))
onUnmounted(() => form.destroy())
const onSubmit = async () => await form.action()
</script>
<template>
    <form :class="fCls" @submit.prevent="onSubmit">
        <slot />
        <button type="submit" :class="bCls" :disabled="!form.isActionable.value">{{ content }}</button>
    </form>
</template>
<style scoped lang="scss">
button[type='submit'] {
    width: 100%;
}
</style>
