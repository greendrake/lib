<script setup lang="ts">
import Loading from './Loading.vue'
</script>
<template>
    <div class="Splash">
        <div class="Splash__content">
            <div class="Splash__logo">
                <slot />
            </div>
            <Loading />
        </div>
    </div>
</template>
<style lang="scss">
@use '@greendrake/scss-kit' as *;

.Splash {
    pointer-events: none;
    position: absolute;
    inset: 0;
    z-index: z('raised');
    display: flex;
    justify-content: center;
    align-items: center;

    &__content {
        text-align: center;
    }

    &__logo {
        font-size: 48px;
        line-height: 48px;
        font-weight: 700;
    }
}
</style>
