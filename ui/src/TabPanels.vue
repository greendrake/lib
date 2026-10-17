<script lang="ts">
import type { TabPane } from './TabBar.vue'

/** A tab: the tab strip's entry plus the panel it shows. */
export interface TabPanel extends TabPane {
    // The panel outlives being switched away from: kept as it was, not mounted again
    // on coming back. Unset, it is unmounted, and whatever it holds open closes.
    keepAlive?: boolean
}
</script>
<script setup lang="ts">
// A tab strip over one panel at a time, the tabs declared as data. The strip and the
// panel are the host's children, laid out as it lays out its own.
import { computed } from 'vue'
import TabBar from './TabBar.vue'

const props = defineProps<{ tabs: TabPanel[] }>()

// The open tab's id: bound by a host that opens tabs itself, kept here otherwise.
const active = defineModel<string>('active')

// Only ever a registry id, so the lookup always resolves.
const activeTab = computed({
    get: () => active.value ?? props.tabs[0]!.id,
    set: id => {
        active.value = id
    }
})
const panel = computed(() => props.tabs.find(t => t.id === activeTab.value)!)
</script>
<template>
    <TabBar v-model="activeTab" :tabs="tabs" />
    <!-- A kept panel is deactivated rather than unmounted while another shows. -->
    <KeepAlive>
        <component :is="panel.component" v-if="panel.keepAlive" :key="panel.id" v-bind="panel.props" />
    </KeepAlive>
    <component :is="panel.component" v-if="!panel.keepAlive" v-bind="panel.props" />
</template>
