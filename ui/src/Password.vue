<script setup lang="ts">
import { computed, ref, onMounted, nextTick } from 'vue'
import { hasFinePointer } from '@greendrake/util'
import FixAndroidTyping from './FixAndroidTyping'

const showPassword = ref(false)
const plainInput = ref<HTMLInputElement | null>(null)
const passwordInput = ref<HTMLInputElement | null>(null)
const model = defineModel<string>({ default: '' })
const props = withDefaults(
    defineProps<{
        autofocus?: boolean
        minLength?: number
        // Lands on the wrapper — tests and FieldTip anchor `#<id>`, inner
        // inputs via `#<id> input`.
        id?: string
    }>(),
    {
        minLength: 1
    }
)
const currentInput = computed(() => (showPassword.value ? plainInput : passwordInput))
const onInput = () => FixAndroidTyping(currentInput.value, model)
const onSwitchVisibility = () => {
    // Preserve the caret position
    const caretPos = currentInput.value.value?.selectionStart ?? 0
    showPassword.value = !showPassword.value
    nextTick(() => {
        const input = currentInput.value.value
        if (input) {
            input.focus()
            input.setSelectionRange(caretPos, caretPos)
        }
    })
}
// Pointer devices only — see TextField for why touch is excluded.
if (props.autofocus && hasFinePointer()) {
    onMounted(() => {
        currentInput.value.value?.focus()
    })
}
</script>
<template>
    <div :id="id" class="Password" :class="{ PasswordVisible: showPassword }">
        <input v-if="showPassword" ref="plainInput" v-model="model" type="text" :minlength="minLength" required spellcheck="false" @input="onInput" />
        <input v-else ref="passwordInput" v-model="model" type="password" :minlength="minLength" required @input="onInput" />
        <div @click.prevent="onSwitchVisibility"></div>
    </div>
</template>
<style lang="scss">
@use '@greendrake/scss-kit' as *;
.Password {
    display: flex;
    border: solid 1px var(--font-color);
    border-radius: 2px;
    input {
        border: 0;
        flex-grow: 1;
        &:focus {
            outline: none;
        }
        line-height: var(--line-height-normal);
        @include monospace;
    }
    > div {
        background-color: green;
        mask-size: 70%;
        opacity: 0.8;
        align-self: stretch;
        mask-position: 2px center;
        cursor: default;
        width: 30px;
        &:hover {
            opacity: 1;
        }
        @include icon-base;
        @include i('eye');
    }
    &.PasswordVisible {
        > div {
            @include i('eye-off');
        }
    }
}
</style>
