<script setup lang="ts">
defineProps<{
    label?: string
    // Set when the slot holds a GROUP of controls (radio set, composite row):
    // the caption then names a role="group" wrapper instead of wrapping the
    // content in a <label>, which would smear one label across every control.
    group?: boolean
    // Set when the slot's control cannot be labelled implicitly — a
    // contenteditable is not a labelable element. A <label> around one binds to
    // the first labelable descendant instead, and a rich editor's is its
    // toolbar's first <button>: the caption would then name that button, and
    // every click in the field would be FORWARDED to it as a real click, so
    // clicking into the text would silently press Bold.
    //
    // Such a control names itself (a rich editor puts its aria-label on the
    // contenteditable), so the caption is left as plain text — no role="group",
    // which would only announce the name a second time.
    unlabelable?: boolean
}>()
</script>
<template>
    <div class="Field">
        <template v-if="label && group">
            <span class="Field__caption">{{ label }}</span>
            <div role="group" :aria-label="label" class="Field__group">
                <slot />
            </div>
        </template>
        <!-- No wrapper at all: .Field is already the column the <label> below
             would have been, so the caption and control sit exactly where the
             labelled variant puts them. -->
        <template v-else-if="label && unlabelable">
            <span class="Field__caption">{{ label }}</span>
            <slot />
        </template>
        <!-- The single control renders INSIDE the label (implicit association):
             the caption is the control's accessible name, zero id plumbing. -->
        <label v-else-if="label">
            <span class="Field__caption">{{ label }}</span>
            <slot />
        </label>
        <slot v-else />
    </div>
</template>
<style lang="scss" scoped>
.Field {
    display: flex;
    flex-direction: column;
    > label {
        display: flex;
        flex-direction: column;
    }
}
</style>
