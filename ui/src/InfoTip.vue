<script setup lang="ts">
// A mark beside something worth explaining: its text shows in a tooltip on hover, on
// focus, and on a tap where there is no hover. A button, so that a keyboard reaches it;
// placed beside a field's <label> rather than in it, where it would become the control
// the label names.
import { Tippy } from 'vue-tippy'
import { messages } from './messages'
import './Tip.scss'

defineProps<{ text: string }>()
</script>
<template>
    <Tippy :content="text" theme="tip" placement="top" trigger="mouseenter focus" class="InfoTip">
        <button type="button" class="InfoTip__mark" :aria-label="messages.aboutThis">?</button>
    </Tippy>
</template>
<style lang="scss">
.InfoTip__mark {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    padding: 0;
    border: 1px solid var(--border-color);
    border-radius: 50%;
    background: transparent;
    color: var(--font-color-muted, var(--font-color));
    font-size: var(--font-xs);
    line-height: 1;
    cursor: help;
}
</style>
