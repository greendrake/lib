<script setup lang="ts">
import type { ToastVariant } from './showToast'

withDefaults(
    defineProps<{
        // Rendered as plain text; rich content arrives as a VNode via showToast.
        text?: string
        variant?: ToastVariant
    }>(),
    { text: undefined, variant: 'accent' }
)
</script>
<template>
    <div class="Toast" :class="`Toast--${variant}`">
        <template v-if="text">{{ text }}</template>
        <slot v-else />
    </div>
</template>
<style lang="scss">
@use '@greendrake/scss-kit' as *;

// The shared layer showToast mounts every toast into — one per document, the
// toasts stacked down from the top in the order they were raised.
.ToastLayer {
    position: fixed;
    // Below the status bar and cutout, not over them. The layer is fixed, so
    // it sits outside the padding body spends on the same inset — without this
    // it would render across the clock and battery on a native shell. The
    // token is 0 in browsers, leaving the gap as it was.
    top: calc(var(--safe-area-inset-top, 0px) + 10px);
    left: 0;
    right: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-2);
    pointer-events: none;
    z-index: z('toast');
}

.Toast {
    pointer-events: auto;
    max-width: min(92vw, 480px);
    padding: var(--space-3) var(--space-4);
    border-radius: 8px;
    color: var(--toast-color, #fff);
    box-shadow: 0 4px 14px rgb(0 0 0 / 35%);
    a {
        color: var(--toast-color, #fff);
    }
}

// An ordinary toast reports something that worked; the warning variant is the
// failure surface, and reads as one at a glance rather than only in its words.
// `--toast-background` still overrides both, for an app that wants its own.
.Toast--accent {
    background-color: var(--toast-background, var(--accent-color));
}

.Toast--warning {
    background-color: var(--toast-background, var(--warning-color));
}

.BlockingOverlay {
    position: fixed;
    inset: 0;
    z-index: z('modal');
    background-color: rgb(0 0 0 / 25%);
}
</style>
