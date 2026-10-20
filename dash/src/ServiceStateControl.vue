<script setup lang="ts">
// One backend service, live: its state, why it failed if it did, and the one
// command that state accepts (CONTROLS). The state is the service's document,
// which the control watches through the keeper for as long as it is mounted
// (keepServiceStates), so every operator watching sees the same thing at the
// same time — including transitions somebody else started — and the service is
// read only while some control shows it. Several controls share one api and
// one keeper, each showing the service it names.
import { computed, onActivated, onDeactivated, onUnmounted, ref, watch } from 'vue'
import { Loading, errorMessage } from '@greendrake/ui'
import { CONTROLS, TRANSITIONS, type ServiceCommand, type ServiceState, type ServiceStateMethods } from '@greendrake/service-state'
import type { ApiCaller } from '@greendrake/vue-api'
import type { ServiceStateKeeper } from './keeper'
import { serviceStateMessages } from './serviceStateMessages'

const props = defineProps<{
    // Which of the backend's services this is: every call names it, and the
    // control follows it as it changes.
    service: string
    // The commands' caller, over the socket or HTTP alike.
    api: ApiCaller<ServiceStateMethods>
    services: ServiceStateKeeper
}>()

// Watched while shown: from mounting to unmounting, and not while a kept-alive
// panel it is on is deactivated, which would otherwise have the service read
// for the page's life.
let release: (() => void) | undefined
const show = (): void => {
    release ??= props.services.watch(() => props.service)
}
const hide = (): void => {
    release?.()
    release = undefined
}
show()
onActivated(show)
onDeactivated(hide)
onUnmounted(hide)

// Absent until the backend has read the service with somebody watching: the
// control shows its own Loading meanwhile, never a state nobody verified — or,
// where watching it was refused, why.
const status = computed(() => props.services.status(props.service))
const failure = computed(() => props.services.failure(props.service))

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
        <template v-if="status">
            <span class="ServiceStateControl-state">{{ status.state }}</span>
            <p v-if="status.state === 'ERROR'" class="ServiceStateControl-error">{{ status.error }}</p>
            <button v-if="command" type="button" :disabled="calling || awaiting !== undefined" @click="run(command)">{{ serviceStateMessages[command] }}</button>
        </template>
        <p v-else-if="failure" class="ServiceStateControl-error">{{ errorMessage(failure) }}</p>
        <Loading v-else />
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
