<script setup lang="ts">
// A notice set apart from the content around it, marked with the info glyph;
// optional controls (an `actions` slot) stack beneath the message.
</script>
<template>
    <div class="InfoBlock">
        <slot />
        <div v-if="$slots.actions" class="InfoBlock__actions">
            <slot name="actions" />
        </div>
    </div>
</template>
<style lang="scss">
@use '@greendrake/scss-kit' as *;
.InfoBlock {
    --icon-space-width: 50px;
    margin-top: var(--space-4);
    position: relative;
    padding: var(--space-4) var(--space-4) var(--space-4) var(--icon-space-width);
    border: 1px solid var(--infoblock-line-color);
    border-radius: var(--border-radius);
    line-height: var(--line-height-tight);
    &::before {
        content: '';
        position: absolute;
        inset-inline-start: 0;
        top: 0;
        bottom: 0;
        width: var(--icon-space-width);
        mask-size: 24px;
        mask-position: center 14px;
        @include icon-base;
        @include i('info');
    }
    // Optional controls (e.g. an escalation note + button) rendered within the
    // block, separated from the message and stacked left-aligned.
    &__actions {
        margin-top: var(--space-3);
        display: flex;
        flex-direction: column;
        align-items: stretch;
        gap: var(--space-2);
    }
}
</style>
