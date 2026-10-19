<script setup lang="ts">
// What failed, in words, next to where it was asked for.
defineProps<{ text: string }>()
</script>
<template>
    <span class="Problem">{{ text }}</span>
</template>
<style lang="scss">
.Problem {
    color: var(--error-color);
}
</style>
