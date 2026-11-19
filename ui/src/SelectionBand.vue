<script setup lang="ts">
import type { SelectionBandState } from './useSelectionBand'

// A selection band drawn where it stands: render it inside the band's
// container, positioned (relative, say) so that it places the band.
defineProps<{ band: SelectionBandState }>()
</script>
<template>
    <div v-if="band.active" class="SelectionBand" :style="{ left: `${band.left}px`, top: `${band.top}px`, width: `${band.width}px`, height: `${band.height}px` }"></div>
</template>
<style lang="scss">
.SelectionBand {
    position: absolute;
    border: 1px solid var(--row-select-color, rgba(0, 123, 255, 0.4));
    background: var(--row-select-color, rgba(0, 123, 255, 0.08));
    pointer-events: none;
}
</style>
