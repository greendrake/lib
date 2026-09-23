<script setup lang="ts">
import { copyTextToClipboard } from './copyTextToClipboard'
import { messages } from './messages'

const props = withDefaults(
    defineProps<{
        value: string
        showValue?: boolean
    }>(),
    {
        showValue: false
    }
)

const onClick = async (e: MouseEvent) => {
    e.preventDefault()
    const target = e.currentTarget as HTMLElement
    await copyTextToClipboard(props.value)
    target.classList.add('copied')
    setTimeout(() => {
        if (document.body.contains(target)) {
            target.classList.remove('copied')
        }
    }, 2000)
}
</script>
<template>
    <div class="Copy2Clipboard" :title="messages.clickToCopy" @click="onClick">
        <span v-if="showValue" class="Copy2Clipboard__value">{{ value }}</span>
    </div>
</template>
<style lang="scss">
@use '@greendrake/scss-kit' as *;
.Copy2Clipboard {
    cursor: default;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    &::before {
        content: '';
        @include icon;
        @include i('copy');
    }
    &:hover::before {
        background-color: orange;
    }
    &.copied::before {
        background-color: green;
    }
}
</style>
