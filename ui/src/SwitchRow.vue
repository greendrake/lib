<script setup lang="ts">
// An on/off switch shaped as a row: the label at the leading edge, a track and
// knob trailing, showing the current state rather than leaving an ambiguous
// label to imply it.
//
// An <a role="switch"> rather than a checkbox: in a menu the row sits among
// menu items and has to read and be styled as one of them (the host classes it
// `menu-item`), while the role keeps its on/off state honest to assistive tech.
// Layout comes from the host's class, which side the label sits on included —
// everything here is the switch itself.
const on = defineModel<boolean>({ required: true })
defineProps<{
    label: string
    // Tooltip, where what the switch DOES is worth naming apart from what it
    // is: the colour-mode row is labelled "Dark mode" but offers "Light mode"
    // while dark. Defaults to the label.
    title?: string
}>()
</script>
<template>
    <!-- .stop, because MenuButton's popper dismisses itself on any click within
         it: right for an item that IS a choice, wrong for a control that acts in
         place. Without it the menu vanishes on the very frame the knob would
         have moved, and the setting can only be read back by reopening it. -->
    <a href="#" class="SwitchRow" role="switch" :aria-checked="on" :title="title ?? label" @click.prevent.stop="on = !on">
        <span class="SwitchRow__text">{{ label }}</span>
        <span class="SwitchRow__toggle" :class="{ 'SwitchRow__toggle--on': on }"><span class="SwitchRow__knob"></span></span>
    </a>
</template>
<style lang="scss">
.SwitchRow {
    // The row is an <a> (for role="switch"), so the global `a, a span` rule would
    // paint it violet — but it's a toggle, not a link. Pin the label span too
    // (the `a span` rule explicitly colours it, so the anchor colour alone won't
    // reach it).
    color: var(--font-color);
    text-decoration: none;
    gap: var(--space-2);
    &__text {
        color: var(--font-color);
    }
    &__toggle {
        flex: 0 0 auto;
        width: 40px;
        height: 22px;
        border-radius: var(--border-radius-pill);
        background: var(--border-color);
        transition: background 0.15s;
        position: relative;
    }
    &__knob {
        position: absolute;
        top: 2px;
        inset-inline-start: 2px;
        width: 18px;
        height: 18px;
        border-radius: 50%;
        background: #fff;
        transition: transform 0.15s;
    }
    &__toggle--on {
        background: var(--accent-color);
        .SwitchRow__knob {
            // The knob travels toward the inline end, which is leftwards under
            // an RTL layout — transforms have no logical form, so the direction
            // is stated rather than derived.
            transform: translateX(18px);
        }
        [dir='rtl'] & .SwitchRow__knob {
            transform: translateX(-18px);
        }
    }
}
</style>
