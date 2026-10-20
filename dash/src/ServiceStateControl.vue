<script setup lang="ts">
// One backend service, live: its state, why it failed if it did, and the one
// command that state accepts (CONTROLS). The state is the backend's document,
// kept where the host made its socket (keepServiceStates), so every operator
// watching sees the same thing at the same time — including transitions
// somebody else started — and a control mounted at any point finds it there.
// Several controls share one api and one states, each showing the service it
// names.
import { computed, ref, watch } from 'vue'
import { Loading } from '@greendrake/ui'
import { CONTROLS, TRANSITIONS, type ServiceCommand, type ServiceState, type ServiceStateMethods, type ServiceStates } from '@greendrake/service-state'
import type { ApiCaller } from '@greendrake/vue-api'
import { serviceStateMessages } from './serviceStateMessages'

const props = defineProps<{
    // Which of the backend's services this is: every call names it, and it is
    // what its entry in the states is keyed by.
    service: string
    api: ApiCaller<ServiceStateMethods>
    states: ServiceStates
}>()

// Absent until the backend has read the service with somebody watching: the
// control shows its own Loading meanwhile, never a state nobody verified.
const status = computed(() => props.states[props.service])

const command = computed(() => (status.value ? CONTROLS[status.value.state] : null))

// The command's call, while it is out.
const calling = ref(false)
// A start or stop acknowledged while the document still shows the state it was
// offered from: the acknowledgement can arrive ahead of the patch that moves
// the service — a call over HTTP beside the socket — and the button must not
// come back over a state the service is already leaving. Cleared as the state
// moves. A call that fails sets none, and nor does a refresh, whose reading has
// landed by the time it answers and may change nothing.
const awaiting = ref<ServiceState>()
watch(
    () => status.value?.state,
    state => {
        if (state !== awaiting.value) {
            awaiting.value = undefined
        }
    }
)

// Answers nothing: where the service went is the document's to say.
const run = async (offered: ServiceCommand | 'refresh'): Promise<void> => {
    await props.api.with({ pending: calling }).call(`service.${offered}`, { service: props.service })
    if (offered !== 'refresh' && status.value?.state === TRANSITIONS[offered].from) {
        awaiting.value = TRANSITIONS[offered].from
    }
}
</script>
<template>
    <div class="ServiceStateControl" :data-state="status?.state">
        <Loading v-if="!status" />
        <template v-else>
            <span class="ServiceStateControl-state">{{ status.state }}</span>
            <p v-if="status.state === 'ERROR'" class="ServiceStateControl-error">{{ status.error }}</p>
            <button v-if="command" type="button" :disabled="calling || awaiting !== undefined" @click="run(command)">{{ serviceStateMessages[command] }}</button>
        </template>
    </div>
</template>
<style lang="scss">
.ServiceStateControl {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 1rem;
    align-self: start;
    margin: 2rem;
    padding: 2rem;
    min-width: 16rem;
    border: 2px solid var(--border-color-light);
    border-radius: 0.5rem;

    // The border is the state, read before the word is: running, broken, or
    // neither.
    &[data-state='ON'] {
        border-color: var(--accent-color);
    }

    &[data-state='ERROR'] {
        border-color: var(--error-color);
    }

    &-state {
        font-size: 1.5rem;
        font-weight: 600;
        letter-spacing: 0.1em;
    }

    &-error {
        margin: 0;
        color: var(--error-color);
        text-align: center;
    }
}
</style>
