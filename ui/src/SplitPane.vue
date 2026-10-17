<script setup lang="ts">
// Two panes side by side, the start one resized by dragging the handle between
// them. Without an `end` slot the start pane spans the whole width alone.
import { ref } from 'vue'
import useWidthDrag from './useWidthDrag'

const props = withDefaults(
    defineProps<{
        initialWidth?: number
        minWidth?: number
    }>(),
    {
        initialWidth: 150,
        minWidth: 100
    }
)

const width = ref(props.initialWidth)
const { active: resizing, start, move, end } = useWidthDrag(() => props.minWidth)
</script>
<template>
    <div class="SplitPane" :class="{ 'SplitPane--resizing': resizing }">
        <div class="SplitPane__start" :style="$slots.end ? { width: width + 'px' } : { flex: '1 1 0', width: 0 }">
            <slot name="start"></slot>
        </div>
        <template v-if="$slots.end">
            <div class="SplitPane__handle" @pointerdown="start($event, width, w => (width = w))" @pointermove="move" @pointerup="end" @pointercancel="end"></div>
            <div class="SplitPane__end">
                <slot name="end"></slot>
            </div>
        </template>
    </div>
</template>
<style lang="scss">
@use '@greendrake/scss-kit' as *;

.SplitPane {
    display: flex;
    min-width: 0;
    // Flex item in a column parent: without this the automatic min-height (auto)
    // keeps it as tall as its content, so it overgrows the viewport and the
    // panes' overflow has nothing to scroll within.
    min-height: 0;

    &--resizing {
        @include dragging(col-resize);
    }

    &__start,
    &__end {
        min-height: 0;
        display: flex;
        flex-direction: column;

        > * {
            flex-grow: 1;
            min-height: 0;
        }
    }

    &__start {
        flex-shrink: 0;
        min-width: v-bind('minWidth + "px"');
        overflow: auto;
    }

    &__handle {
        width: 4px;
        flex-shrink: 0;
        cursor: col-resize;
        // The drag is handled via pointer events; without this the browser
        // claims touch drags for panning and cancels them.
        touch-action: none;
        background-color: var(--border-color-light);
        &:hover {
            background-color: var(--accent-color);
        }
    }

    &__end {
        flex-grow: 1;
        min-width: 0;
    }
}
</style>
