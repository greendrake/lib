<script setup lang="ts">
import { ref } from 'vue'
import Modal from './Modal.vue'

// Trigger-plus-modal composite: the default slot is the trigger element;
// clicking it opens the built-in Modal around the `content` slot. The modal
// chrome props mirror Modal's and are forwarded as-is — undefined falls
// through to Modal's own defaults; `modalClass` is a distinct prop because
// class/$attrs on this component land on the trigger element.
//
// `ok` is re-emitted WITHOUT auto-closing: async submitters keep the modal
// open on failure and close programmatically (exposed onCancel) on success.
// `cancel` auto-closes first, then re-emits for callers that reset form state
// on dismissal.
withDefaults(
    defineProps<{
        // Forwarded to Modal: titled header with explicit close.
        title?: string
        tag?: string
        // Trigger-element attributes, declared (not $attrs) so strictTemplates
        // accepts them at call sites.
        id?: string
        type?: string
        modalClass?: string
        okLabel?: string
        cancelLabel?: string
        okDisabled?: boolean
        hideOk?: boolean
        closable?: boolean
        expandable?: boolean
        buttons?: boolean
    }>(),
    {
        tag: 'a',
        // Vue casts absent boolean props to false unless a default is
        // declared; explicit `undefined` defaults keep absent props undefined
        // so Modal's own defaults (notably `buttons: true`) stay in effect.
        okDisabled: undefined,
        hideOk: undefined,
        closable: undefined,
        expandable: undefined,
        buttons: undefined
    }
)
const emit = defineEmits<{ open: []; ok: []; cancel: []; click: [event: MouseEvent] }>()
const modalOpen = ref(false)
const onClick = (event: MouseEvent) => {
    // Re-emitted so callers can react to the trigger interaction itself
    // (e.g. stop propagation inside a clickable row).
    emit('click', event)
    modalOpen.value = true
    emit('open')
}
const onCancel = () => {
    modalOpen.value = false
}
const onModalCancel = () => {
    onCancel()
    emit('cancel')
}
defineExpose({
    open: onClick,
    onCancel
})
</script>
<template>
    <Modal v-if="modalOpen" :class="modalClass" :title="title" :ok-label="okLabel" :cancel-label="cancelLabel" :ok-disabled="okDisabled" :hide-ok="hideOk" :closable="closable" :expandable="expandable" :buttons="buttons" @ok="emit('ok')" @cancel="onModalCancel">
        <slot name="content" />
    </Modal>
    <component :is="tag" :id="id" :type="type" href="#" v-bind="$attrs" @click.prevent="onClick">
        <slot />
    </component>
</template>
