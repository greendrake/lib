<script setup lang="ts">
import { useAsyncAction } from '@greendrake/ui'
import { useCameraApi } from './api'
import Problem from './Problem.vue'
import { inWords } from './problems'

const api = useCameraApi()

// The camera goes offline at once; the banner follows it back.
const reboot = useAsyncAction(
    inWords(async () => {
        await api.call('camera.reboot')
    })
)
</script>
<template>
    <button type="button" :disabled="reboot.acting.value" @click="reboot.run()">Reboot</button>
    <Problem v-if="reboot.error.value" :text="reboot.error.value" />
</template>
