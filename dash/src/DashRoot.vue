<script lang="ts">
import type { VNode } from 'vue'

// Renders its default slot and adds no element of its own. Vue's Fragment
// cannot serve as a dynamic outlet here: `<component :is>` hands children over
// as SLOTS, while a Fragment vnode renders only ARRAY children — it drops a
// slots object on the floor and renders nothing, with no warning. A functional
// component consumes the slot properly and is still wrapper-free.
const Passthrough = (_: unknown, { slots }: { slots: { default?: () => VNode[] } }): VNode[] | undefined => slots.default?.()
</script>
<script setup lang="ts">
import { computed, Suspense } from 'vue'
import { Splash } from '@greendrake/ui'
import { useAppState } from '@greendrake/vue-kit'

const props = withDefaults(
    defineProps<{
        // One line per <div>, laid out by the Splash component.
        splash: string[]
        // For dashboards whose route component has async setup. The splash then
        // doubles as the boundary's initial content: when isFirstLoading flips,
        // the swapped-in route component suspends and Suspense retains the
        // splash until it resolves — no blank frame in between. Off by default
        // so dashboards that don't need it take neither the boundary nor Vue's
        // experimental-feature warning.
        suspense?: boolean
    }>(),
    { suspense: false }
)

// The boundary is the only difference between the two cases: with suspense the
// content sits inside Suspense, without it the passthrough renders it bare.
const outlet = computed(() => (props.suspense ? Suspense : Passthrough))

const appState = useAppState()
</script>
<template>
    <component :is="outlet">
        <Splash v-if="appState.isFirstLoading">
            <div v-for="line in splash" :key="line">{{ line }}</div>
        </Splash>
        <router-view v-else />
    </component>
</template>
