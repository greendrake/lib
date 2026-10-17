<script setup lang="ts">
import { ref, computed } from 'vue'
import { SplitPane, TabBar, type TabDef } from '@greendrake/ui'

// The widths are the master pane's, handed to SplitPane (its defaults when unset).
defineProps<{
    initialWidth?: number
    minWidth?: number
    fullWidthUntilTab?: boolean
}>()

const emit = defineEmits<{
    'tab-open': [tab: TabDef]
    'tab-close': [tab: TabDef]
    'tab-activate': [tab: TabDef]
}>()

const tabs = ref<TabDef[]>([])
const activeTabId = ref<string | null>(null)

const activeTab = computed(() => tabs.value.find(t => t.id === activeTabId.value) ?? null)

const hasTabs = computed(() => tabs.value.length > 0)

function openTab(id: string, label?: string): void {
    let tab = tabs.value.find(t => t.id === id)
    if (!tab) {
        tab = { id, label: label ?? id }
        tabs.value.push(tab)
        emit('tab-open', tab)
    }
    activeTabId.value = id
    emit('tab-activate', tab)
}

function closeTab(id: string): void {
    const index = tabs.value.findIndex(t => t.id === id)
    if (index === -1) return

    const tab = tabs.value[index]
    tabs.value.splice(index, 1)
    emit('tab-close', tab)

    if (activeTabId.value === id) {
        activeTabId.value = tabs.value[Math.min(index, tabs.value.length - 1)]?.id ?? null
        if (activeTabId.value) {
            emit('tab-activate', tabs.value.find(t => t.id === activeTabId.value)!)
        }
    }
}

function activateTab(id: string): void {
    const tab = tabs.value.find(t => t.id === id)
    if (tab) {
        activeTabId.value = id
        emit('tab-activate', tab)
    }
}

function updateTab(id: string, newId?: string, newLabel?: string): void {
    const tab = tabs.value.find(t => t.id === id)
    if (!tab) return
    if (newLabel !== undefined) tab.label = newLabel
    if (newId !== undefined && newId !== id) {
        tab.id = newId
        if (activeTabId.value === id) activeTabId.value = newId
    }
}

defineExpose({
    openTab,
    closeTab,
    activateTab,
    updateTab,
    tabs,
    activeTab,
    activeTabId
})
</script>
<template>
    <SplitPane class="MasterDetail" :initial-width="initialWidth" :min-width="minWidth">
        <template #start>
            <slot name="master" :open-tab="openTab"></slot>
        </template>
        <template v-if="hasTabs || !fullWidthUntilTab" #end>
            <div class="MasterDetail__detail">
                <TabBar :tabs="tabs" :model-value="activeTabId" closable @update:model-value="activateTab" @close="closeTab" />
                <div class="MasterDetail__content">
                    <template v-for="tab in tabs" :key="tab.id">
                        <div v-show="tab.id === activeTabId" class="MasterDetail__pane">
                            <slot name="detail" :tab="tab" :is-active="tab.id === activeTabId"></slot>
                        </div>
                    </template>
                </div>
            </div>
        </template>
    </SplitPane>
</template>
<style lang="scss">
.MasterDetail {
    &__detail {
        overflow: hidden;
        display: flex;
        flex-direction: column;
    }

    &__content {
        flex-grow: 1;
        min-height: 0;
        min-width: 0;
        display: flex;
    }

    &__pane {
        flex-grow: 1;
        display: flex;
        min-height: 0;
        min-width: 0;

        > * {
            flex-grow: 1;
            min-width: 0;
        }
    }
}
</style>
