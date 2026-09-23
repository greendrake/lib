<script lang="ts">
import type { ServiceState, ServiceStateMethods } from '@greendrake/service-state'

// The command a state offers, if any. Every other state — the two the service
// passes through mid-transition — has no row and therefore no button: there is
// no interfering with something already under way.
interface StateCommand {
    label: string
    method: Exclude<keyof ServiceStateMethods, 'service.status'>
}

const COMMANDS: Partial<Record<ServiceState, StateCommand>> = {
    ON: { label: 'Turn OFF', method: 'service.stop' },
    OFF: { label: 'Turn ON', method: 'service.start' },
    ERROR: { label: 'Refresh', method: 'service.refresh' }
}
</script>
<script setup lang="ts">
// One backend service, live: its state, why it failed if it did, and the one
// command that state accepts. The state comes from the server's pushes, so
// every operator watching sees the same thing at the same time — including
// transitions somebody else started.
import { computed, onMounted, ref } from 'vue'
import { Loading } from '@greendrake/ui'
import { SERVICE_STATE_EVENT, type ServiceStatePushes, type ServiceStatus } from '@greendrake/service-state'
import type { WsTransport } from '@greendrake/rpc'
import { backgroundCall, useLive, type ApiClient, type CallOptions } from '@greendrake/vue-api'

const props = defineProps<{
    api: ApiClient<ServiceStateMethods>
    transport: WsTransport<ServiceStatePushes>
}>()

const status = ref<ServiceStatus>()
const busy = ref(false)

const command = computed(() => (status.value ? COMMANDS[status.value.state] : undefined))

const read = async (call?: CallOptions): Promise<void> => {
    status.value = await props.api.call('service.status', [], call)
}

onMounted(() => read())

useLive(props.transport, {
    events: { [SERVICE_STATE_EVENT]: { apply: (data: ServiceStatus) => (status.value = data) } },
    // A socket that dropped missed whatever changed while it was gone, and the
    // state it comes back to may be nothing like the one it left. In the
    // background: nobody asked for this read.
    resync: () => read(backgroundCall(true))
})

const run = async (method: StateCommand['method']): Promise<void> => {
    status.value = await props.api.call(method, [], { pending: busy })
}
</script>
<template>
    <div class="ServiceStateControl" :data-state="status?.state">
        <Loading v-if="!status" />
        <template v-else>
            <span class="ServiceStateControl-state">{{ status.state }}</span>
            <p v-if="status.error" class="ServiceStateControl-error">{{ status.error }}</p>
            <button v-if="command" type="button" :disabled="busy" @click="run(command.method)">{{ command.label }}</button>
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
