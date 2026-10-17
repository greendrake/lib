<script lang="ts">
import type { TabPanel } from '@greendrake/ui'

/** A dashboard tab: the tab strip's entry plus the panel it renders. */
export type DashTab = TabPanel
</script>
<script setup lang="ts">
// The dashboard shell: a tab strip over one panel at a time, filling the page. The tab
// registry is the single declarative source for both — adding a tab is one row in the
// array the host passes.
import { TabPanels } from '@greendrake/ui'

defineProps<{ tabs: DashTab[] }>()

// The open tab's id: bound by a host that opens tabs itself, kept here otherwise.
const active = defineModel<string>('active')
</script>
<template>
    <div class="Dashboard">
        <TabPanels v-model:active="active" :tabs="tabs" />
    </div>
</template>
<style lang="scss">
.Dashboard {
    display: flex;
    flex-direction: column;
    height: 100vh;
    width: 100%;
}
</style>
