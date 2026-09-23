<script setup lang="ts">
import { computed, ref, watch, onMounted, inject, type Component } from 'vue'
import { hasFinePointer } from '@greendrake/util'
import FieldTip from './FieldTip'
import FixAndroidTyping from './FixAndroidTyping'
import { validationScopeKey } from './validationScope'

const input = ref<HTMLInputElement | HTMLTextAreaElement | null>(null)
const error = ref<string | false>(false)
// Blank input yields '' — the model is always a string, never undefined.
// When a validator is set, the model likewise falls back to '' while the
// typed value is invalid.
const model = defineModel<string>({ default: '' })
const props = withDefaults(
    defineProps<{
        autofocus?: boolean
        // Lands on the input so focusIn(id) / <label for> can target it.
        id?: string
        type?: string
        placeholder?: string
        // Accessible name when no visible label is associated; defaults to the
        // placeholder so a placeholder-only field always names itself.
        ariaLabel?: string
        validator?: (v: string) => true | string
        validateOnBlur?: boolean
        // The rich-text editor component rendered when type === 'rich'. It must
        // speak the same string v-model contract as this field. Supplied by the
        // consumer (see @greendrake/ui-rich) so tiptap is bundled only where
        // rich editing is actually used. Init-time contract: read once below.
        richEditor?: Component
    }>(),
    {
        type: 'text',
        validateOnBlur: true
    }
)
if (props.type === 'rich' && !props.richEditor) {
    throw new Error('TextField: type="rich" requires the richEditor prop')
}
const richEditor = props.richEditor
const inputValue = ref(model.value)
const isInputHere = computed(() => input.value && input.value.isConnected)
const validationScope = inject(validationScopeKey, null)
const onLeave = (e: FocusEvent) => {
    const validator = props.validator
    if (!validator || !props.validateOnBlur) return
    // When a form provides a validation scope, suppress validation if focus
    // is leaving that scope entirely (clicked outside the form, or onto
    // unfocusable content). Validate only when focus stays within the form.
    const scopeEl = validationScope?.value
    if (scopeEl) {
        const next = e.relatedTarget as Node | null
        if (!next || !scopeEl.contains(next)) return
    }
    // this event may be triggered by destroying the housing component, in which case
    // there is no need to validate/set tips.
    // Verify that the element is still part of DOM after a small delay:
    const valueAtBlur = inputValue.value
    setTimeout(() => {
        // Skip validation if the value changed since blur (e.g. form reset due
        // to navigation) or the field is empty (an untouched/emptied field
        // should not nag on its way out).
        if (isInputHere.value && inputValue.value === valueAtBlur && inputValue.value !== '') {
            const validationResult = validator(inputValue.value)
            if (true !== validationResult) {
                error.value = validationResult
            }
        }
    }, 200)
}
let ignoreModelChange = false
watch(inputValue, () => {
    if (error.value !== false) {
        error.value = false
    }
    const modelValue = !props.validator || props.validator(inputValue.value) === true ? inputValue.value : ''
    if (modelValue !== model.value) {
        ignoreModelChange = true
        model.value = modelValue
    }
})
watch(model, m => {
    if (ignoreModelChange) {
        ignoreModelChange = false
    } else if (inputValue.value !== m) {
        inputValue.value = m
    }
})
FieldTip(error, input)
// Pointer devices only: there, landing in the field is a free head start and the
// whole surface stays visible. On touch it would summon the on-screen keyboard
// over the content the user came to read, before they asked to type.
if (props.autofocus && hasFinePointer()) {
    onMounted(() => {
        setTimeout(() => {
            // Only claim focus when nothing else has it. Otherwise a delayed
            // autofocus can race against a modal that opened after this field
            // mounted and steal focus mid-interaction (e.g. between Playwright's
            // .fill focus step and its keyboard.insertText step).
            if (isInputHere.value && input.value && document.activeElement === document.body) {
                input.value.focus()
            }
        }, 300)
    })
}
const onInput = () => FixAndroidTyping(input, inputValue)
const focus = () => {
    if (isInputHere.value && input.value) input.value.focus()
}
defineExpose({ focus })
</script>
<template>
    <textarea v-if="type === 'textarea'" :id="id" ref="input" v-model="inputValue" :placeholder="placeholder" :aria-label="ariaLabel ?? placeholder" spellcheck="false" @blur="onLeave" @input="onInput" />
    <component :is="richEditor!" v-else-if="type === 'rich'" v-model="inputValue" :aria-label="ariaLabel" />
    <input v-else :id="id" ref="input" v-model="inputValue" :type="type" :placeholder="placeholder" :aria-label="ariaLabel ?? placeholder" spellcheck="false" @blur="onLeave" @input="onInput" />
</template>
