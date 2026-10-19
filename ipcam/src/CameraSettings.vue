<script lang="ts">
import type { Component } from 'vue'
import MaintenanceSection from './MaintenanceSection.vue'
import SettingsSection from './SettingsSection.vue'
import { MAINTENANCE_GROUP } from './types'

// The sections of groups that show more than their settings.
const sections: Record<string, Component> = { [MAINTENANCE_GROUP]: MaintenanceSection }
</script>
<script setup lang="ts">
// A tab per group of the camera's settings. A group's settings are read from the camera
// once its tab first opens, and kept while another tab shows.
import { computed, ref, shallowRef, watch } from 'vue'
import { TabPanels, useAsyncAction, type TabPanel } from '@greendrake/ui'
import { useCameraApi } from './api'
import Problem from './Problem.vue'
import { inWords } from './problems'
import type { CameraState, Group } from './types'

const props = defineProps<{
    // Undefined until the engine has told it.
    state: CameraState | undefined
    // The group shown, by its ID.
    group: string | undefined
}>()

const emit = defineEmits<{
    // The group to show: one picked, or one standing in for a group the camera does not
    // have, which a host keeping the group in history replaces rather than adds to.
    'update:group': [group: string, picked: boolean]
}>()

const api = useCameraApi()

// The groups the camera has settings of, read once it is first online, the spinner
// showing on the settings meanwhile.
const groups = shallowRef<Group[]>([])
const load = useAsyncAction(
    inWords(async () => {
        groups.value = await api.call('settings.groups')
    })
)
watch(
    () => props.state?.State,
    state => {
        if (state === 'online' && groups.value.length === 0) {
            void load.run()
        }
    },
    { immediate: true }
)

// The group shown: the one named, if the camera has it. A group it does not have, or
// none, stands for the one shown last, or the first, which stays shown meanwhile.
const shown = ref<string>()
watch(
    [() => props.group, groups],
    ([group]) => {
        if (groups.value.length === 0) {
            return
        }
        if (groups.value.some(g => g.ID === group)) {
            shown.value = group
            return
        }
        emit('update:group', shown.value ?? groups.value[0]!.ID, false)
    },
    { immediate: true }
)

// A tab picked, which TabPanels' model always names.
const pick = (group: string | undefined): void => {
    emit('update:group', group!, true)
}

const tabs = computed((): TabPanel[] =>
    groups.value.map(group => ({
        id: group.ID,
        label: group.Title,
        component: sections[group.ID] ?? SettingsSection,
        props: { group },
        keepAlive: true
    }))
)
</script>
<template>
    <div class="CameraSettings" :class="{ spinner: load.acting.value }">
        <Problem v-if="load.error.value" :text="load.error.value" />
        <TabPanels v-if="shown" :active="shown" :tabs="tabs" @update:active="pick" />
    </div>
</template>
<style lang="scss">
@use '@greendrake/scss-kit' as kit;

// The tab strip, and under it the group's section, which fills the rest and scrolls.
.CameraSettings {
    @include kit.column-filled-by-last;
}
</style>
