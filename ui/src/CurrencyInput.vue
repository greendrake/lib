<script lang="ts">
const isValid = (v: string): boolean => !v || /^[\d,.]+$/.test(v)
const limitPrecision = (n: string, precision: number): string => {
    if (!n.includes('.')) {
        return n
    }
    const parts = n.split('.')
    if (!precision) {
        return parts[0]
    }
    return parts[0] + '.' + parts[1].substring(0, precision)
}
const doInsertThousandsSeparators = (n: string): string => n.replace(/\D/g, '').replace(/\B(?=(\d{3})+(?!\d))/g, ',')
const insertThousandsSeparators = (n: string): string => {
    if (!n.includes('.')) {
        return doInsertThousandsSeparators(n)
    }
    const parts = n.split('.')
    return doInsertThousandsSeparators(parts[0]) + '.' + parts[1]
}
const ensureDecimals = (n: string, precision: number): string => {
    const parts = n.split('.')
    if (!precision) {
        return parts[0]
    }
    return parts[0] + '.' + (parts[1] || '').padEnd(precision, '0')
}
</script>
<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import { hasFinePointer } from '@greendrake/util/browser'

const props = withDefaults(
    defineProps<{
        // Accessible name for the amount input (the visible caption usually
        // names the composite row, not this control).
        ariaLabel?: string
        placeholder?: string
        autofocus?: boolean
        symbol?: string
        hideSymbol?: boolean
        doNotShowZeroDecimals?: boolean
        forceDecimals?: boolean
        disabled?: boolean
        min?: number | string | null
        precision?: number
    }>(),
    {
        precision: 2
    }
)
const model = defineModel<string | null>()
const groomValue = (v: string | null, forceDecimals?: boolean): string => {
    if (v === null || v === '') {
        return ''
    }
    let result = insertThousandsSeparators(limitPrecision(v, props.precision))
    if (props.forceDecimals || forceDecimals) {
        result = ensureDecimals(result, props.precision)
    }
    if (props.doNotShowZeroDecimals && result.endsWith(`.${'0'.repeat(props.precision)}`)) {
        result = result.substring(0, result.length - (1 + props.precision))
    }
    return result
}
const input = ref<HTMLInputElement | null>(null)
let previous: string | null = null
const isFullyValid = (v: string): boolean => {
    if (!isValid(v)) {
        return false
    }
    if (props.min && Number(props.min) > Number(v)) {
        return false
    }
    return true
}
const onInput = () => {
    const inp = input.value
    if (!inp) return
    if (isFullyValid(inp.value)) {
        previous = inp.value
        if (inp.value) {
            const groomed = groomValue(inp.value)
            if (inp.value !== groomed) {
                const whole = inp.value.split('.')[0]
                const caretPos = insertThousandsSeparators(whole).length - whole.length + (inp.selectionStart ?? 0)
                inp.value = groomed
                inp.setSelectionRange(caretPos, caretPos)
            }
        }
        model.value = inp.value ? ensureDecimals(inp.value.replace(/,/g, ''), props.precision) : null
    } else if (previous !== null && input.value) {
        input.value.value = previous
    }
}
const model2input = () => {
    if (input.value) {
        input.value.value = groomValue(model.value ?? null)
    }
}
watch(model, mv => {
    if (input.value && groomValue(input.value.value, true) !== groomValue(mv ?? null, true)) {
        model2input()
    }
})
onMounted(() => {
    if (model.value) {
        model2input()
    }
    // Pointer devices only — see TextField for why touch is excluded.
    if (props.autofocus && input.value && hasFinePointer()) {
        input.value.focus()
    }
})
</script>
<template>
    <div :data-currency="symbol" class="CurrencyInput" :class="{ 'CurrencyInput--no-symbol': hideSymbol }">
        <input ref="input" type="text" :aria-label="ariaLabel" :placeholder="placeholder" :disabled="disabled" @input="onInput" />
    </div>
</template>
<style lang="scss">
.CurrencyInput {
    position: relative;
    input {
        text-align: end;
        width: 100%;
    }
    &:not(.CurrencyInput--no-symbol) {
        &::before {
            opacity: 0.8;
            font-weight: bold;
            content: attr(data-currency);
            position: absolute;
            top: 50%;
            transform: translateY(-50%);
            inset-inline-start: 8px;
        }
        input {
            padding-inline-start: 30px;
        }
    }
}
</style>
