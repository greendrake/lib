<script lang="ts">
import type { Component } from 'vue'
import type { TabDef } from '@greendrake/ui'

/** A dashboard tab: the tab strip's entry plus the panel it renders. */
export interface DashTab extends TabDef {
    component: Component
    props?: Record<string, unknown>
}
</script>
<script setup lang="ts">
// The dashboard shell: a tab strip over one panel at a time. The tab registry
// is the single declarative source for both — adding a tab is one row in the
// array the host passes.
import { computed, ref } from 'vue'
import { TabBar } from '@greendrake/ui'

const props = defineProps<{ tabs: DashTab[] }>()

// activeTab only ever holds a registry id, so the lookup always resolves.
const activeTab = ref(props.tabs[0]!.id)
const active = computed(() => props.tabs.find(t => t.id === activeTab.value)!)
</script>
<template>
    <div class="Dashboard">
        <TabBar v-model="activeTab" :tabs="tabs" />
        <component :is="active.component" v-bind="active.props" />
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
