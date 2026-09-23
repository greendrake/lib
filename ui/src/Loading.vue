<script setup lang="ts">
import { messages } from './messages'
</script>
<template>
    <div class="Loading">{{ messages.loading }} <span>.</span><span>.</span><span>.</span></div>
</template>
<style lang="scss" scoped>
@keyframes LoadingDots {
    0% {
        opacity: 1;
    }
    50% {
        opacity: 0;
    }
}
div {
    white-space: nowrap;
    span {
        letter-spacing: 3px;
        animation: LoadingDots 1s infinite linear;
        &:nth-child(1) {
            animation-delay: 0s;
        }
        &:nth-child(2) {
            animation-delay: 0.2s;
        }
        &:nth-child(3) {
            animation-delay: 0.4s;
        }
    }
}
</style>
