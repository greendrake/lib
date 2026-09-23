<script setup lang="ts">
// Attrs are placed by hand (see the template): class/style on the root, the
// rest — placeholder, role, aria-* — on the inner input. That split is what Vue
// does by default and what this option suspends; forwarding the whole $attrs
// put a host's class on the <input>, where a rule written against the field
// silently never matched (the busy affordance `.SearchField.spinner::before`,
// driven by AsyncSearch, is exactly that case).
defineOptions({ inheritAttrs: false })
import { ref, watch } from 'vue'
import { Debouncer } from '@greendrake/util'
import TextField from './TextField.vue'
import { messages } from './messages'

const model = defineModel<string>({ default: '' })
const internalModel = ref(model.value)
const props = withDefaults(
    defineProps<{
        placeholder?: string
        autofocus?: boolean
        // true ⇒ default delay; a number ⇒ fixed delay (ms); a function ⇒ delay
        // decided per keystroke from the prospective value (e.g. a swift debounce
        // when the consumer knows the change resolves locally, a longer one when it
        // triggers a remote fetch).
        debounce?: boolean | number | ((value: string) => number)
    }>(),
    {
        debounce: undefined
    }
)
// One Debouncer instance for the field's lifetime so each keystroke invalidates the
// previous pending fire; the delay is resolved per fire (see watch below) to support
// the function form. ms 0 here is just a placeholder — fire() always gets an explicit delay.
const debouncer = props.debounce ? new Debouncer(0) : undefined
const resolveDelay = (value: string): number => {
    const d = props.debounce
    if (typeof d === 'function') return d(value)
    if (d === true) return 600
    return d || 0
}
const clear = () => {
    model.value = ''
}
// Commit the current (un-debounced) value immediately and drop any pending
// debounced update, so an explicit "search now" (Enter) acts on exactly what's
// typed right now. Returns whether the value actually changed — letting the caller
// distinguish a real update (the model watcher will react) from a no-op re-trigger.
const flush = (): boolean => {
    debouncer?.cancel()
    if (model.value === internalModel.value) return false
    model.value = internalModel.value
    return true
}
defineExpose({ flush })
watch(model, v => {
    if (v !== internalModel.value) {
        internalModel.value = v
    }
})
watch(internalModel, async v => {
    if (debouncer) {
        const proceed = await debouncer.fire(resolveDelay(v))
        if (!proceed()) {
            return
        }
    }
    model.value = v
})
</script>
<template>
    <div class="SearchField" :class="$attrs.class" :style="$attrs.style" @keyup.esc="clear">
        <!-- Escape handled on the root (keyup bubbles from the inner input). A
             comment ABOVE this div would render the component as a fragment in
             dev builds and break $el consumers (AsyncSearch anchors its popper
             on it) — the attr-splitting rationale is in the script instead. -->
        <TextField v-model="internalModel" :placeholder="placeholder" :autofocus="autofocus" v-bind="{ ...$attrs, class: undefined, style: undefined }" />
        <button v-if="internalModel" type="button" class="clear" :aria-label="messages.clear" @click="clear" />
    </div>
</template>
<style scoped lang="scss">
.SearchField {
    display: flex;
    position: relative;
    input {
        padding-inline: 30px;
        flex-grow: 1;
        width: 100%;
    }
    .clear {
        position: absolute;
        inset-inline-end: 4px;
        top: 50%;
        transform: translateY(-50%);
        width: 24px;
        height: 24px;
        padding: 0;
        border: none;
        background-color: var(--font-color);
        background-image: none;
        opacity: 0.5;
        cursor: default;
        mask-position: center center;
        mask-repeat: no-repeat;
        mask-image: url("data:image/svg+xml,%3Csvg width='20' height='20' viewBox='0 0 20 20' fill='none' xmlns='http://www.w3.org/2000/svg'%3E%3Cpath d='M5 5L15 15M15 5L5 15' stroke='currentColor' stroke-width='2' stroke-linecap='round'/%3E%3C/svg%3E");
        &:hover {
            opacity: 0.8;
        }
    }
    &::after {
        content: '';
        background-color: var(--font-color);
        width: 32px;
        height: 70%;
        display: block;
        position: absolute;
        left: 0;
        top: 50%;
        transform: translateY(-50%);
        opacity: 0.5;
        mask-position: center center;
        mask-repeat: no-repeat;
        mask-image: url("data:image/svg+xml,%3Csvg width='20' height='20' viewBox='0 0 20 20' fill='none' xmlns='http://www.w3.org/2000/svg'%3E%3Cpath d='M12.6923 8.92308C12.6923 11.0012 11.0012 12.6923 8.92308 12.6923C6.84495 12.6923 5.15385 11.0012 5.15385 8.92308C5.15385 6.84495 6.84495 5.15385 8.92308 5.15385C11.0012 5.15385 12.6923 6.84495 12.6923 8.92308ZM17 15.9231C17 15.637 16.8822 15.3594 16.6887 15.1659L13.8029 12.28C14.4844 11.2957 14.8462 10.1178 14.8462 8.92308C14.8462 5.65024 12.1959 3 8.92308 3C5.65024 3 3 5.65024 3 8.92308C3 12.1959 5.65024 14.8462 8.92308 14.8462C10.1178 14.8462 11.2957 14.4844 12.28 13.8029L15.1659 16.6803C15.3594 16.8822 15.637 17 15.9231 17C16.512 17 17 16.512 17 15.9231Z' fill='currentColor'/%3E%3C/svg%3E%0A");
    }
}
</style>
