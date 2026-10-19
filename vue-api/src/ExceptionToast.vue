<script setup lang="ts">
import { computed, ref } from 'vue'
import { messages } from '@greendrake/ui'
import { apiMessages } from './messages'
import { getGoHome } from './uxConfig'
import type { ExceptionKind } from './exceptionState'

const props = defineProps<{
    kind: Exclude<ExceptionKind, false>
    // Whether a usable page is already on screen (see exceptionState). It
    // decides what a not-found can offer: somewhere to go, or just an
    // acknowledgement.
    canStayPut: boolean
    retry: () => Promise<void> | void
    cancel: () => void
}>()

const counter = ref(1)
const isRetrying = ref(false)

const kindMessages = computed<Record<Exclude<ExceptionKind, false>, string>>(() => ({
    other: apiMessages.errorOther,
    network: apiMessages.errorNetwork,
    notfound: apiMessages.errorNotFound
}))

// A not-found has one outcome, acknowledging it: retrying would ask for what is
// not there again. Everything else may yet go through, and is offered again.
const isAcknowledgeOnly = computed(() => props.kind === 'notfound')

// A not-found reached from a page the visitor is already on leaves them there:
// the navigation never happened, what they were looking at is still behind the
// toast, and offering the home page would be sending them away from it. Only a
// visitor who arrived straight at the missing thing — a shared link opened
// cold, so nothing else is loaded — has anywhere to be sent.
const notFoundLeavesNowhere = computed(() => isAcknowledgeOnly.value && !props.canStayPut)

const button = computed(() => {
    if (notFoundLeavesNowhere.value) {
        return apiMessages.toHomePage
    }
    if (isAcknowledgeOnly.value) {
        return messages.ok
    }
    return isRetrying.value ? apiMessages.trying : apiMessages.tryAgain
})

const onClick = (): void => {
    if (isAcknowledgeOnly.value) {
        props.cancel()
        if (notFoundLeavesNowhere.value) {
            getGoHome()()
        }
        return
    }
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
}

const message = computed(() => `${kindMessages.value[props.kind]}${counter.value > 1 ? ` (${counter.value})` : ''}`)
</script>
<template>
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
