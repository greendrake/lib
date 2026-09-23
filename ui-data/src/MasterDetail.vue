<script setup lang="ts">
import { ref, computed } from 'vue'
import { TabBar, type TabDef } from '@greendrake/ui'

const props = withDefaults(
    defineProps<{
        initialWidth?: number
        minWidth?: number
        fullWidthUntilTab?: boolean
    }>(),
    {
        initialWidth: 150,
        minWidth: 100,
        fullWidthUntilTab: false
    }
)

const emit = defineEmits<{
    'tab-open': [tab: TabDef]
    'tab-close': [tab: TabDef]
    'tab-activate': [tab: TabDef]
}>()

const tabs = ref<TabDef[]>([])
const activeTabId = ref<string | null>(null)
const masterWidth = ref(props.initialWidth)

const activeTab = computed(() => tabs.value.find(t => t.id === activeTabId.value) ?? null)

const hasTabs = computed(() => tabs.value.length > 0)

const masterStyle = computed(() => {
    if (props.fullWidthUntilTab && !hasTabs.value) {
        return { flex: '1 1 0', width: 0 }
    }
    return { width: masterWidth.value + 'px' }
})

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

function onResizeStart(e: MouseEvent): void {
    const startX = e.clientX
    const startWidth = masterWidth.value

    function onMouseMove(e: MouseEvent): void {
        masterWidth.value = Math.max(props.minWidth, startWidth + e.clientX - startX)
    }

    function onMouseUp(): void {
        document.removeEventListener('mousemove', onMouseMove)
        document.removeEventListener('mouseup', onMouseUp)
    }

    document.addEventListener('mousemove', onMouseMove)
    document.addEventListener('mouseup', onMouseUp)
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
    <div class="MasterDetail">
        <div class="MasterDetail__master" :style="masterStyle">
            <slot name="master" :open-tab="openTab"></slot>
        </div>
        <template v-if="hasTabs || !fullWidthUntilTab">
            <div class="MasterDetail__resize" @mousedown="onResizeStart"></div>
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
    </div>
</template>
<style lang="scss">
.MasterDetail {
    display: flex;
    min-width: 0;
    // Flex item in a column parent: without this the automatic min-height (auto)
    // keeps it as tall as its content, so it overgrows the viewport and the
    // master's overflow:auto has nothing to scroll within.
    min-height: 0;

    &__master {
        flex-shrink: 0;
        min-width: v-bind('minWidth + "px"');
        overflow: auto;
        display: flex;
        flex-direction: column;

        > * {
            flex-grow: 1;
            min-height: 0;
        }
    }

    &__resize {
        width: 4px;
        flex-shrink: 0;
        cursor: col-resize;
        background-color: var(--border-color-light);
        &:hover {
            background-color: var(--accent-color);
        }
    }

    &__detail {
        flex-grow: 1;
        min-width: 0;
        min-height: 0;
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
