<script lang="ts">
export interface SegmentedOption {
    value: string
    label: string
}
</script>
<script setup lang="ts">
import { randomString } from '@greendrake/util'

// A segmented control backed by real radio inputs: native semantics (arrow-key
// group navigation, form participation) with pill presentation. Each input is
// stretched invisibly across its whole segment, so the accessible target IS
// the segment — no sub-24px radio dots (WCAG 2.5.8).
// `ariaLabel` names the group for assistive tech (the bare Available/Wanted
// segments give no context out of the visual layout); when set the container
// becomes an explicit radiogroup.
defineProps<{ options: SegmentedOption[]; ariaLabel?: string }>()
const model = defineModel<string>({ required: true })
const group = randomString()
</script>
<template>
    <div class="SegmentedControl" :role="ariaLabel ? 'radiogroup' : undefined" :aria-label="ariaLabel">
        <label v-for="opt in options" :key="opt.value" class="SegmentedControl__segment" :class="{ 'SegmentedControl__segment--active': model === opt.value }">
            <input v-model="model" type="radio" :name="group" :value="opt.value" />
            <span>{{ opt.label }}</span>
        </label>
    </div>
</template>
<style lang="scss">
.SegmentedControl {
    display: inline-flex;
    border: 1px solid var(--border-color-light);
    border-radius: 999px;
    overflow: hidden;
    background: var(--form-background);
    &__segment {
        position: relative;
        // Segments share the track equally, so the control reads symmetric
        // whether it sizes to content or is stretched by its container.
        flex: 1 1 0;
        display: flex;
        align-items: center;
        justify-content: center;
        min-height: 32px;
        padding: 0 var(--space-3);
        cursor: default;
        white-space: nowrap;
        input {
            // Invisible but real: covers the segment so IT is the hit target,
            // and keyboard focus stays on a native radio.
            position: absolute;
            inset: 0;
            width: 100%;
            height: 100%;
            margin: 0;
            opacity: 0;
            cursor: default;
        }
        &:not(:last-child) {
            border-inline-end: 1px solid var(--border-color-light);
        }
        // End caps carry the track's pill radius so focus rings (inset
        // box-shadows follow border-radius) hug the curve instead of being
        // clipped into a broken C by the track's overflow:hidden.
        &:first-child {
            border-radius: 999px 0 0 999px;
        }
        &:last-child {
            border-radius: 0 999px 999px 0;
        }
        &--active {
            background: var(--accent-color);
            color: var(--accent-contrast);
        }
        // Two-tone inset ring keeps the app's green focus duty without going
        // invisible on the green active fill: the outer accent ring reads on
        // neutral segments, the inner contrast ring reads on the accent fill.
        &:has(input:focus-visible) {
            box-shadow:
                inset 0 0 0 2px var(--accent-color, currentColor),
                inset 0 0 0 4px var(--accent-contrast, transparent);
        }
    }
}
</style>
