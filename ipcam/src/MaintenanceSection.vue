<script lang="ts">
const pad = (n: number): string => String(n).padStart(2, '0')

// The browser's local wall time, as the camera's clock takes it: it keeps no zone.
const wallTime = (d: Date): string => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
</script>
<script setup lang="ts">
// Maintenance: its settings, and the camera's clock and its reboot.
import { computed, ref } from 'vue'
import { useAsyncAction } from '@greendrake/ui'
import { useCameraApi } from './api'
import Problem from './Problem.vue'
import { inWords } from './problems'
import RebootButton from './RebootButton.vue'
import SettingsSection from './SettingsSection.vue'
import type { Group } from './types'

defineProps<{ group: Group }>()

const api = useCameraApi()

const clock = ref<string>()
const readClock = useAsyncAction(
    inWords(async () => {
        clock.value = (await api.call('camera.time')).Time
    })
)
const setClock = useAsyncAction(
    inWords(async () => {
        await api.call('camera.setTime', { Time: wallTime(new Date()) })
        await readClock.run()
    })
)
void readClock.run()
const clockProblem = computed(() => readClock.error.value || setClock.error.value)
</script>
<template>
    <SettingsSection :group="group">
        <div class="MaintenanceSection__clock">
            <span>
                Camera clock: <output class="MaintenanceSection__time">{{ clock }}</output>
            </span>
            <button type="button" :disabled="readClock.acting.value" @click="readClock.run()">Read</button>
            <button type="button" :disabled="setClock.acting.value" @click="setClock.run()">Set to this computer's</button>
            <Problem v-if="clockProblem" :text="clockProblem" />
            <RebootButton />
        </div>
    </SettingsSection>
</template>
<style lang="scss">
.MaintenanceSection {
    &__clock {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        margin-top: var(--space-3);
    }
}
</style>
