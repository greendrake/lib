<script setup lang="ts">
// A button that reloads what it stands by: the refresh icon, named for
// assistive tech and in its tooltip. Its click is the host's listener.
import { messages } from './messages'
</script>
<template>
    <button type="button" class="RefreshButton" :aria-label="messages.refresh" :title="messages.refresh"></button>
</template>
<style lang="scss">
@use '@greendrake/scss-kit' as *;
.RefreshButton {
    @include icon;
    @include i('refresh');
    cursor: default;
    flex-shrink: 0;
    padding: 0;
    opacity: 0.4;
    &:hover {
        opacity: 1;
    }
}
</style>
