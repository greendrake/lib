import type { Ref } from 'vue'

type InputElement = HTMLInputElement | HTMLTextAreaElement

// Some Android keyboards compose text without firing the events Vue's v-model
// relies on, leaving the model behind the actual input value. Called from an
// @input handler, this syncs the model from the DOM when they diverge.
export default (inputRef: Ref<InputElement | null>, modelRef: Ref<string>, isValid?: (value: string) => boolean): void => {
    const input = inputRef.value
    if (input && input.value !== modelRef.value && (!isValid || isValid(input.value))) {
        modelRef.value = input.value
    }
}
