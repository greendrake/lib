<script setup lang="ts">
// One backend service, live: its state, why it failed if it did, and the one
// command that state accepts (CONTROLS). The state comes from the server's
// pushes, so every operator watching sees the same thing at the same time —
// including transitions somebody else started. Several controls can share one
// api and transport, each following the service it names.
import { computed, onMounted, ref } from 'vue'
import { Loading } from '@greendrake/ui'
import { CONTROLS, SERVICE_STATE_EVENT, type ServiceStateMethods, type ServiceStatePushes, type ServiceStatus } from '@greendrake/service-state'
import type { WsTransport } from '@greendrake/rpc'
import { backgroundCall, useLive, type ApiCaller, type CallOptions } from '@greendrake/vue-api'
import { serviceStateMessages } from './serviceStateMessages'

const props = defineProps<{
    // Which of the backend's services this is: every call names it, and it is
    // what this control's pushes are told apart by on a socket shared with
    // others.
    service: string
    api: ApiCaller<ServiceStateMethods>
    transport: WsTransport<ServiceStatePushes>
}>()

const status = ref<ServiceStatus>()
const busy = ref(false)

const command = computed(() => (status.value ? CONTROLS[status.value.state] : null))

const read = async (call: CallOptions = {}): Promise<void> => {
    status.value = await props.api.with(call).call('service.status', { service: props.service })
}

// The control's own Loading stands in until the first reading lands, so the
// page's spinner would only say the same thing twice. A failure still reaches
// the error toast: there is nothing else to show here without a status.
onMounted(() => read({ pending: false }))

useLive(props.transport, {
    events: {
        [SERVICE_STATE_EVENT]: {
            when: (data: ServiceStatus) => data.service === props.service,
            apply: (data: ServiceStatus) => (status.value = data)
        }
    },
    // A socket that dropped missed whatever changed while it was gone, and the
    // state it comes back to may be nothing like the one it left. In the
    // background: nobody asked for this read.
    resync: () => read(backgroundCall(true))
})

const run = async (offered: NonNullable<typeof command.value>): Promise<void> => {
    status.value = await props.api.with({ pending: busy }).call(`service.${offered}`, { service: props.service })
}
</script>
<template>
    <div class="ServiceStateControl" :data-state="status?.state">
        <Loading v-if="!status" />
        <template v-else>
            <span class="ServiceStateControl-state">{{ status.state }}</span>
            <p v-if="status.error" class="ServiceStateControl-error">{{ status.error }}</p>
            <button v-if="command" type="button" :disabled="busy" @click="run(command)">{{ serviceStateMessages[command] }}</button>
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
