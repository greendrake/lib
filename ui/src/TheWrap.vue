<template>
    <div class="TheWrap">
        <div class="TheWrap__content">
            <slot name="top" />
            <slot />
        </div>
        <slot name="bottom" />
    </div>
</template>
<style lang="scss" scoped>
// Page layout shell: `top` slot + routed content stretch, the `bottom` slot
// (footer) is pushed to the viewport bottom on short pages. When only a single
// child renders (e.g. a splash), it is centered vertically instead.
// CSS contract: --max-content-width (optional) caps the shell's width; without
// it the shell spans its container.
.TheWrap {
    width: 100%;
    max-width: var(--max-content-width, none);
    min-height: 100%;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    &__content {
        flex: 1 0 auto;
    }
    &:has(> :only-child) {
        justify-content: center;
    }
}
</style>
