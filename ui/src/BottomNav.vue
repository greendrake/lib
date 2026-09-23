<script lang="ts">
export interface BottomNavTab {
    id: string
    label: string
    // The route this tab navigates to. Omit for a tab that acts on the app
    // shell instead of navigating — an overlay whose existence is a property of
    // the viewport, not of the URL. Such a tab renders as a button and reports
    // its state through `select` + aria-expanded rather than aria-current.
    to?: string
    // Marks this tab active. The app owns the mapping (a tab usually covers a
    // family of routes, not one path), so it is a plain flag, not a matcher.
    active: boolean
    // Optional attention count rendered as a badge on the tab icon.
    badge?: number
}
</script>
<script setup lang="ts">
import { RouterLink } from 'vue-router'
defineProps<{ tabs: BottomNavTab[] }>()
const emit = defineEmits<{ select: [id: string] }>()
</script>
<template>
    <!-- Fixed bottom tab bar for narrow viewports. Apps style the per-tab icon
         via the .BottomNav__icon--<id> class hook (mask-image pattern) and hide
         the bar on wide viewports; the safe-area inset is honoured here so the
         bar clears the home indicator in native shells. The badge is a sibling
         of the masked icon, not a child — a mask clips its descendants. -->
    <nav class="BottomNav">
        <component
            :is="tab.to ? RouterLink : 'button'"
            v-for="tab in tabs"
            :key="tab.id"
            :to="tab.to"
            :type="tab.to ? undefined : 'button'"
            class="BottomNav__tab"
            :class="{ 'BottomNav__tab--active': tab.active }"
            :aria-current="tab.to && tab.active ? 'page' : undefined"
            :aria-expanded="tab.to ? undefined : tab.active"
            @click="tab.to || emit('select', tab.id)"
        >
            <span class="BottomNav__glyph" aria-hidden="true">
                <span class="BottomNav__icon" :class="`BottomNav__icon--${tab.id}`"></span>
                <span v-if="tab.badge" class="BottomNav__badge">{{ tab.badge }}</span>
            </span>
            <span class="BottomNav__label">{{ tab.label }}</span>
        </component>
        <!-- Trailing cell for something that is not navigation: a page's own
             actions, which on a narrow viewport have nowhere else persistent to
             live. Empty on every page that offers none, and the tabs take the
             whole bar then — so a page with actions narrows them, which is the
             cost of not reserving a cell nothing usually fills. -->
        <slot />
    </nav>
</template>
<style lang="scss">
@use '@greendrake/scss-kit' as *;
.BottomNav {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 0;
    z-index: z('sticky');
    display: flex;
    height: calc(var(--bottomnav-height) + var(--safe-area-inset-bottom, 0px));
    padding-bottom: var(--safe-area-inset-bottom, 0px);
    background: var(--form-background);
    border-top: 1px solid var(--border-color-light);
    &__tab {
        flex: 1 1 0;
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        gap: 2px;
        color: var(--font-color-muted, var(--font-color));
        text-decoration: none;
        // A tab is a link or, when it opens a shell overlay, a button — which
        // arrives with a UA background, border and font of its own.
        background: none;
        border: none;
        font: inherit;
        cursor: pointer;
        &--active {
            color: var(--accent-color);
            .BottomNav__icon {
                background-color: var(--accent-color);
            }
        }
    }
    &__glyph {
        position: relative;
        width: 24px;
        height: 24px;
    }
    &__icon {
        @include icon-base;
        display: block;
        width: 100%;
        height: 100%;
        mask-position: center center;
        background-color: var(--font-color-muted, var(--font-color));
    }
    &__badge {
        position: absolute;
        top: -6px;
        right: -10px;
        background: var(--color-attention-fill, var(--accent-color));
        color: var(--color-attention-contrast, #fff);
        border-radius: 999px;
        padding: 0 5px;
        font-size: 11px;
        line-height: 16px;
        min-width: 16px;
        text-align: center;
    }
    &__label {
        font-size: var(--font-xs);
        line-height: 1;
        // Follow the tab's own colour (muted when inactive, accent when active).
        // Without this the theme's global `a span` link tint paints every label
        // violet, so the active/inactive distinction collapses to just the icon.
        color: inherit;
    }
}
</style>
