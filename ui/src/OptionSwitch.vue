<script lang="ts">
export type OptionValue = string | number | boolean
// Array form: a bare value renders String(value) as its title; a [value, title]
// tuple sets the title explicitly. Map form: value → title.
export type OptionsArray<T extends OptionValue = OptionValue> = Array<T | [T, string]>
export type OptionsMap<T extends OptionValue = OptionValue> = Map<T, string>
</script>
<script setup lang="ts" generic="T extends string | number | boolean">
import { computed, ref, onUnmounted } from 'vue'
import Form, { type FormState } from './Form'

// Radio-style switch between a fixed set of values: clicking an option runs
// saveHandler through the Form state machine (with optional confirmation) and
// commits the model on success. Each option can carry extra markup via a slot
// named after its value.
const props = withDefaults(
    defineProps<{
        label?: string
        options: OptionsArray<T> | OptionsMap<T>
        requireConfirmation?: boolean
        disabledOptions?: T[]
        saveHandler?: (value: T) => Promise<void> | void
    }>(),
    {
        disabledOptions: () => []
    }
)
const model = defineModel<T>()

// Re-emitted from the root so hosts can react to the interaction itself
// (e.g. dismissing a popper that would cover the confirm step).
const emit = defineEmits<{ click: [event: MouseEvent] }>()

const optionsMap = computed(() => {
    if (!Array.isArray(props.options)) {
        return props.options
    }
    const map = new Map<T, string>()
    for (const el of props.options) {
        if (Array.isArray(el)) {
            map.set(el[0], el[1])
        } else {
            map.set(el, String(el))
        }
    }
    return map
})
if (model.value !== undefined && !optionsMap.value.has(model.value)) {
    throw new Error(`OptionSwitch value is not among the options: ${model.value}`)
}
const values = computed(() => Array.from(optionsMap.value.keys()))
const titles = computed(() => Array.from(optionsMap.value.values()))
const value2index = computed(() => new Map(values.value.map((v, i) => [v, i])))
const currentValueIndex = computed(() => (model.value === undefined ? undefined : value2index.value.get(model.value)))
const candidateValue = ref<T | null>(null)
const candidateValueIndex = computed(() => (candidateValue.value !== null ? value2index.value.get(candidateValue.value) : undefined))
const form = new Form({
    alwaysValid: true,
    alwaysDirty: true,
    requireConfirmation: props.requireConfirmation,
    handler: async () => {
        // action() is only ever reached via onLabelClick, which sets the candidate.
        const value = candidateValue.value!
        await props.saveHandler?.(value)
        model.value = value
    },
    successTimeout: false
})
onUnmounted(() => form.destroy())
const disabledSet = computed(() => new Set(props.disabledOptions))
const onLabelClick = async (vi: number) => {
    if (disabledSet.value.has(values.value[vi])) {
        return
    }
    candidateValue.value = values.value[vi]
    await form.action()
}
const stateIs = (s: FormState) => computed(() => form.state.value === s)
const isInProgress = stateIs('inprogress')
const isError = stateIs('error')
const isConfirm = stateIs('confirm')
</script>
<template>
    <div class="OptionSwitch__Wrap">
        <label v-if="label">{{ label }}</label>
        <div :class="{ OptionSwitch: true, saving: isInProgress, error: isError }" @click="e => emit('click', e)">
            <div v-for="(title, i) in titles" :key="String(values[i])" :data-index="i" :class="{ confirm: isConfirm && candidateValueIndex === i, current: currentValueIndex === i, disabled: disabledSet.has(values[i]) }" @click="onLabelClick(i)">
                <div v-if="title">{{ title }}</div>
                <slot :name="String(values[i])" />
            </div>
        </div>
    </div>
</template>
<style lang="scss">
.OptionSwitch {
    position: relative;
    > div {
        display: flex;
        align-items: start;
        // Let the label and slotted content (e.g. a SelectBox) shrink below
        // their content width instead of forcing the row past its container.
        // Flex items default to min-width:auto; a wide, non-wrapping slot value
        // would otherwise overflow. The radio glyph keeps its fixed width via
        // its own min-width.
        > * {
            min-width: 0;
        }
    }
}
</style>
