<script setup lang="ts">
import { computed, ref } from 'vue'
import { messages } from '@greendrake/ui'
import { getGoHome } from './uxConfig'
import type { ExceptionKind } from './exceptionState'

const props = defineProps<{
    error: Error
    kind: Exclude<ExceptionKind, false>
    // Whether a usable page is already on screen (see exceptionState). It
    // decides what a not-found can offer: somewhere to go, or just an
    // acknowledgement.
    canStayPut: boolean
    hide: () => void
    retry: () => Promise<void> | void
    cancel: () => void
}>()

const counter = ref(1)
const isRetrying = ref(false)

const kindMessages = computed<Record<string, string>>(() => ({
    other: messages.errorOther,
    network: messages.errorNetwork,
    notfound: messages.errorNotFound
}))

// A not-found reached from a page the visitor is already on leaves them there:
// the navigation never happened, what they were looking at is still behind the
// toast, and offering the home page would be sending them away from it. Only a
// visitor who arrived straight at the missing thing — a shared link opened
// cold, so nothing else is loaded — has anywhere to be sent.
const notFoundLeavesNowhere = computed(() => props.kind === 'notfound' && !props.canStayPut)

const button = computed(() => {
    if (notFoundLeavesNowhere.value) {
        return messages.toHomePage
    }
    if (props.kind === 'oneoutcome' || props.kind === 'notfound') {
        return messages.ok
    }
    return isRetrying.value ? messages.trying : messages.tryAgain
})

// Retryable kinds (network/other) get a Cancel button; acknowledge-only kinds
// (notfound/oneoutcome) have a single outcome.
const isAcknowledgeOnly = computed(() => props.kind === 'notfound' || props.kind === 'oneoutcome')

const onClick = (): void => {
    switch (props.kind) {
        case 'notfound':
            props.cancel()
            if (notFoundLeavesNowhere.value) {
                getGoHome()()
            }
            break
        case 'oneoutcome':
            props.cancel()
            break
        default: {
            const retried = props.retry()
            if (retried instanceof Promise) {
                isRetrying.value = true
                retried
                    .then(() => {
                        counter.value++
                    })
                    .finally(() => {
                        isRetrying.value = false
                    })
            }
            break
        }
    }
}

const message = computed(() => {
    if (props.kind === 'oneoutcome') {
        return props.error.message || messages.errorOther
    }
    return `${kindMessages.value[props.kind]}${counter.value > 1 ? ` (${counter.value})` : ''}`
})
</script>
<template>
    <Teleport to="body">
        <div class="BlockingOverlay" />
    </Teleport>
    <div class="ExceptionToast">
        <div>{{ message }}</div>
        <div class="ExceptionToast__buttons">
            <button v-if="!isAcknowledgeOnly" @click.prevent="cancel">{{ messages.cancel }}</button>
            <div />
            <button :class="{ disabled: isRetrying }" @click.prevent="onClick">{{ button }}</button>
        </div>
    </div>
</template>
<style lang="scss" scoped>
.ExceptionToast {
    &__buttons {
        margin-top: 8px;
        display: flex;
        justify-content: space-between;
        div {
            flex-grow: 1;
        }
        button {
            padding: 0 8px;
            height: 32px;
        }
    }
}
</style>
