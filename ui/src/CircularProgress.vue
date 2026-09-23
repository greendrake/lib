<script lang="ts">
// Geometry is static: r=16 inside a 36×36 viewBox leaves headroom for the 3px
// stroke (16 + 1.5 = 17.5 < 18). Kept at module scope so it isn't rebuilt per
// instance.
const RADIUS = 16
const CIRCUMFERENCE = 2 * Math.PI * RADIUS
</script>
<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ progress: number }>()

const clamped = computed(() => Math.max(0, Math.min(100, props.progress)))
// Full circumference at 0%, zero at 100% — the dash grows clockwise from the
// top (the arc circle is rotated -90deg in CSS).
const dashOffset = computed(() => CIRCUMFERENCE * (1 - clamped.value / 100))
</script>
<template>
    <svg class="CircularProgress" viewBox="0 0 36 36" role="progressbar" :aria-valuenow="Math.round(clamped)" aria-valuemin="0" aria-valuemax="100">
        <circle class="CircularProgress__track" cx="18" cy="18" :r="RADIUS" />
        <circle class="CircularProgress__arc" cx="18" cy="18" :r="RADIUS" :stroke-dasharray="CIRCUMFERENCE" :stroke-dashoffset="dashOffset" />
        <text class="CircularProgress__label" x="18" y="18">{{ Math.round(clamped) }}%</text>
    </svg>
</template>
<style lang="scss">
.CircularProgress {
    width: 100%;
    height: 100%;
    display: block;

    &__track,
    &__arc {
        fill: none;
        stroke-width: 3;
    }

    &__track {
        stroke: var(--circular-progress-track, #c2c6cc);
    }

    &__arc {
        stroke: var(--circular-progress-arc, #2ea043);
        stroke-linecap: round;
        transform: rotate(-90deg);
        transform-origin: 50% 50%;
        transition: stroke-dashoffset 0.15s linear;
    }

    &__label {
        fill: var(--circular-progress-label, #fff);
        font-size: 9px;
        font-weight: 600;
        text-anchor: middle;
        dominant-baseline: central;
    }
}
</style>
