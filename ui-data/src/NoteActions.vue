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

// The decisions a suspendable record offers: Suspend, on a required reason,
// while it is not suspended; Unsuspend while it is.
export const suspensionActions = (suspended: boolean, suspend: (reason: string) => Promise<unknown>, unsuspend: () => Promise<unknown>): NoteAction[] =>
    suspended
        ? [{ label: 'Unsuspend', fn: unsuspend }]
        : [
              {
                  label: 'Suspend',
                  fn: suspend,
                  note: 'required'
              }
          ]
</script>
<script setup lang="ts">
import { computed, ref } from 'vue'
import { Modal } from '@greendrake/ui'
import { useAsyncAction } from './useAsyncAction'

// The operator-decision shell: a note where a decision takes one, and a button
// per decision that records it. The parent owns the (silent) API calls and
// re-reads on @done; this owns the note, the shared in-flight guard, the
// confirmation and the error line.
const props = defineProps<{ actions: NoteAction[]; placeholder: string }>()
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
        <input v-if="takesNote" v-model="note" :placeholder="placeholder" :disabled="action.acting.value" />
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
