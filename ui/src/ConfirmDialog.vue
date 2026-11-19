<script setup lang="ts">
// A modal putting an action to the user: the action's name heads it and labels
// its OK, the message says what it does. OK runs the action, the button held
// while it runs; once it is done the dialog closes. An action that fails leaves
// the dialog open, its failure reported wherever the action reports it.
import { ref } from 'vue'
import Modal from './Modal.vue'

const props = defineProps<{
    title: string
    message: string
    run: () => Promise<unknown>
}>()

const emit = defineEmits<{
    /** Done, or cancelled: the host takes the dialog away. */
    close: []
}>()

const running = ref(false)

async function onOk() {
    running.value = true
    try {
        await props.run()
        emit('close')
    } finally {
        running.value = false
    }
}
</script>
<template>
    <Modal class="ConfirmDialog" :title="title" :ok-label="title" :ok-disabled="running" @ok="onOk" @cancel="emit('close')">
        <p>{{ message }}</p>
    </Modal>
</template>
<style lang="scss">
.ConfirmDialog {
    .Modal__window {
        max-width: 400px;
    }
    p {
        margin: 0;
    }
}
</style>
