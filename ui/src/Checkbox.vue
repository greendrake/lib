<script setup lang="ts">
import { computed } from 'vue'
import { randomString } from '@greendrake/util'

const props = defineProps<{
    label?: string
    // Render as an iOS-style sliding switch instead of the default tick box.
    switch?: boolean
    disabled?: boolean
}>()

const value = defineModel<boolean>()
const id = randomString('checkbox')
const cls = computed(() => ['Checkbox', { 'Checkbox--switch': props.switch }])
</script>
<template>
    <div :class="cls">
        <input :id="id" v-model="value" type="checkbox" :disabled="props.disabled" />
        <label v-if="props.label" :for="id">{{ props.label }}</label>
    </div>
</template>
<style lang="scss">
.Checkbox {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    // 24px minimum: WCAG 2.5.8 target size (the box itself is the target).
    --checkbox-size: 24px;

    &:not(.Checkbox--switch) {
        input[type='checkbox'] {
            padding: 0;
            appearance: none;
            -webkit-appearance: none;
            // Grid-centre the tick glyph — fixed margins would drift off
            // centre the moment the size token changes.
            display: grid;
            place-content: center;
            width: var(--checkbox-size);
            height: var(--checkbox-size);
            max-width: var(--checkbox-size);
            max-height: var(--checkbox-size);
            min-width: var(--checkbox-size);
            min-height: var(--checkbox-size);
            border: 1px solid var(--border-color);
            border-radius: var(--border-radius);
            background: var(--input-background);
            cursor: default;
        }

        input[type='checkbox']:checked::after {
            content: '';
            width: 6px;
            height: 10px;
            border: solid var(--font-color);
            border-width: 0 2px 2px 0;
            // The ink is the glyph box's right+bottom strokes, so the rotated
            // tick reads below/right of the geometric centre; the margin
            // biases the box up to optical centre.
            margin-bottom: 3px;
            transform: rotate(45deg);
        }
    }

    // iOS-style sliding switch: a pill track with a knob that slides to the right
    // and the track turns accent-coloured when checked.
    &.Checkbox--switch {
        --switch-width: 40px;
        --switch-height: 24px;
        --switch-knob: calc(var(--switch-height) - 4px);

        input[type='checkbox'] {
            position: relative;
            padding: 0;
            appearance: none;
            -webkit-appearance: none;
            width: var(--switch-width);
            min-width: var(--switch-width);
            height: var(--switch-height);
            border: none;
            border-radius: var(--switch-height);
            background: var(--border-color);
            transition: background 0.2s ease;
        }

        input[type='checkbox']::after {
            content: '';
            position: absolute;
            top: 2px;
            left: 2px;
            width: var(--switch-knob);
            height: var(--switch-knob);
            border-radius: 50%;
            background: #fff;
            transition: transform 0.2s ease;
        }

        input[type='checkbox']:checked {
            background: var(--accent-color);
        }

        input[type='checkbox']:checked::after {
            transform: translateX(calc(var(--switch-width) - var(--switch-height)));
        }
    }

    input[type='checkbox']:disabled {
        opacity: 0.5;
        cursor: not-allowed;
    }
}
</style>
