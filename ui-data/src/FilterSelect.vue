<script setup lang="ts" generic="S extends string">
// A list's filter over a closed set: any value — the empty one, worded
// `anyLabel` — or one of `options`, shown as they are. `label` is its
// accessible name.
defineProps<{ options: readonly S[]; label: string; anyLabel: string }>()
const value = defineModel<S | ''>({ required: true })
</script>
<template>
    <select v-model="value" :aria-label="label">
        <option value="">{{ anyLabel }}</option>
        <option v-for="option in options" :key="option" :value="option">{{ option }}</option>
    </select>
</template>
