<script lang="ts">
// An operator's decision: what the button says, and what recording it does
// with the note — which it may take (`optional`), wait for as its reason
// (`required`), or ignore (absent). One with `confirm` asks that first.
export interface NoteAction {
    label: string
    fn: (note: string) => Promise<unknown>
    note?: 'optional' | 'required'
    confirm?: string
}
</script>
<script setup lang="ts">
import { computed, ref } from 'vue'
import { Modal, useAsyncAction } from '@greendrake/ui'

// The operator-decision shell: a note where a decision takes one, and a button
// per decision that records it. The parent owns the (silent) API calls and
// re-reads on @done; this owns the note, the shared in-flight guard, the
// confirmation and the error line. With `choices` the note is one of them,
// picked rather than typed — a reason code — `placeholder` naming the pick.
const props = defineProps<{ actions: NoteAction[]; placeholder: string; choices?: readonly string[] }>()
const emit = defineEmits<{ done: [] }>()

const note = ref('')
const takesNote = computed(() => props.actions.some(a => a.note))
// The decision waiting on its confirmation.
const confirming = ref<NoteAction | null>(null)

const action = useAsyncAction(async (a: NoteAction) => {
    confirming.value = null
    await a.fn(note.value.trim())
    note.value = ''
    emit('done')
})

const request = (a: NoteAction): void => {
    if (a.confirm) confirming.value = a
    else void action.run(a)
}
</script>
<template>
    <div class="NoteActions">
        <template v-if="takesNote">
            <select v-if="choices" v-model="note" :aria-label="placeholder" :disabled="action.acting.value">
                <option value="">{{ placeholder }}</option>
                <option v-for="choice in choices" :key="choice" :value="choice">{{ choice }}</option>
            </select>
            <input v-else v-model="note" :placeholder="placeholder" :disabled="action.acting.value" />
        </template>
        <button v-for="a in actions" :key="a.label" :disabled="action.acting.value || (a.note === 'required' && !note.trim())" @click="request(a)">{{ a.label }}</button>
        <div v-if="action.error.value" class="NoteActions__error">{{ action.error.value }}</div>
        <Modal v-if="confirming" :title="confirming.label" :ok-label="confirming.label" @ok="action.run(confirming)" @cancel="confirming = null">
            <p>{{ confirming.confirm }}</p>
        </Modal>
    </div>
</template>
<style lang="scss">
.NoteActions {
    margin-top: var(--space-4);
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex-wrap: wrap;
    input {
        flex: 1 1 200px;
    }
    &__error {
        color: var(--error-color);
        flex-basis: 100%;
    }
}
</style>
