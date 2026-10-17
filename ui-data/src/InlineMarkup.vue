<script setup lang="ts">
// Markup rendered as one line of its inline formatting (inlineMarkup), cut
// with an ellipsis where it overflows.
import { computed } from 'vue'
import { inlineMarkup } from './inlineMarkup'

const props = defineProps<{
    markup: string
}>()

// Safe to v-html: the fragment holds nothing but bare allowlisted elements
// and text, which serializing escapes.
const html = computed(() => {
    const holder = document.createElement('div')
    holder.appendChild(inlineMarkup(props.markup))
    return holder.innerHTML
})
</script>
<template>
    <span class="InlineMarkup" v-html="html"></span>
</template>
<style lang="scss">
.InlineMarkup {
    display: block;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
}
</style>
