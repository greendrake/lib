<script lang="ts">
// Node shape (whatever the loader returns) must carry at least:
//   - [idField]    (default 'key')   — unique string/number id
//   - [labelField] (default 'title') — display text
//   - expandable                     — whether it has children to load
export interface TreeNode {
    expandable: boolean
    [key: string]: unknown
}

export type TreeNodeId = string | number

// loader(parentId | null) => Promise<TreeNode[]>  (null → root level)
export type TreeLoader = (parentId: TreeNodeId | null) => Promise<TreeNode[]>

type Guide = 'through' | 'blank' | 'elbow' | 'elbow-last'

type VisibleEntry = {
    id: TreeNodeId
    node: TreeNode
    depth: number
    expandable: boolean
    guides: Guide[]
}
</script>
<script setup lang="ts">
// Generic lazy tree. Children are loaded on demand via `loader` and cached per
// node; expanding/collapsing only re-walks the cache. The whole tree is rendered
// as one flat list of the currently-visible nodes (each carrying its depth), so
// selection is a flat, index-addressable problem handled by the shared
// useSelection composable — the same one the data table uses. Selection is
// mouse-driven (click = single, shift = range, ctrl/cmd = toggle); there are no
// checkboxes. The selected node ids are exposed as a v-model array.
import { ref, computed, reactive, watch, onMounted } from 'vue'
import useSelection from './useSelection'

const props = withDefaults(
    defineProps<{
        loader: TreeLoader
        idField?: string
        labelField?: string
        multiSelect?: boolean
    }>(),
    {
        idField: 'key',
        labelField: 'title',
        multiSelect: true
    }
)

const selected = defineModel<TreeNodeId[]>('selected', { default: () => [] })

const roots = ref<TreeNode[]>([])
const loading = ref(false)
// id -> child node[]; presence means "already loaded".
const childrenCache = reactive(new Map<TreeNodeId, TreeNode[]>())
const expanded = reactive(new Set<TreeNodeId>())
const childLoading = reactive(new Set<TreeNodeId>())

// Depth-first flatten of every currently-visible node. This ordered list is both
// what the template renders and the index space useSelection operates on (range
// selection walks contiguous entries here).
//
// Each entry also carries the `guides` it needs to draw the connector lines: one
// cell per indentation column. `ancestorVerticals[i]` (threaded down the walk)
// says whether the ancestor sitting in column i still has a sibling below — i.e.
// whether a vertical line passes through this row at that column. The node's own
// (rightmost) column is the elbow: '├' when more siblings follow, '└' when last.
const visibleNodes = computed(() => {
    const out: VisibleEntry[] = []
    const walk = (nodes: TreeNode[], depth: number, ancestorVerticals: boolean[]) => {
        nodes.forEach((node, idx) => {
            const id = node[props.idField] as TreeNodeId
            const isLast = idx === nodes.length - 1
            const guides: Guide[] = ancestorVerticals.map(v => (v ? 'through' : 'blank'))
            if (depth > 0) guides.push(isLast ? 'elbow-last' : 'elbow')
            out.push({
                id,
                node,
                depth,
                expandable: node.expandable,
                guides
            })
            const children = childrenCache.get(id)
            if (expanded.has(id) && children) {
                walk(children, depth + 1, depth === 0 ? [] : [...ancestorVerticals, !isLast])
            }
        })
    }
    walk(roots.value, 0, [])
    return out
})

const selection = useSelection<TreeNodeId>(visibleNodes, 'id')

// Selection is owned by useSelection's Set; the v-model array is a mirror. Seed
// the Set from any initial model value, then keep both in sync. Each watcher
// no-ops when the two already agree, so the round-trip terminates.
for (const id of selected.value) selection.selected.add(id)

watch(
    () => [...selection.selected],
    arr => {
        if (arr.length !== selected.value.length || arr.some(id => !selected.value.includes(id))) {
            selected.value = arr
        }
    }
)

watch(selected, val => {
    const set = selection.selected
    if (set.size === val.length && val.every(id => set.has(id))) return
    set.clear()
    for (const id of val) set.add(id)
})

async function toggleExpand(entry: VisibleEntry) {
    if (!entry.expandable) return
    if (expanded.has(entry.id)) {
        expanded.delete(entry.id)
        return
    }
    if (!childrenCache.has(entry.id)) {
        childLoading.add(entry.id)
        try {
            childrenCache.set(entry.id, await props.loader(entry.id))
        } finally {
            childLoading.delete(entry.id)
        }
    }
    expanded.add(entry.id)
}

function onRowClick(entry: VisibleEntry, e: MouseEvent) {
    if (props.multiSelect && e.shiftKey) selection.rangeTo(entry.id)
    else if (props.multiSelect && (e.ctrlKey || e.metaKey)) selection.toggle(entry.id)
    else selection.replace(entry.id)
}

onMounted(async () => {
    loading.value = true
    try {
        roots.value = await props.loader(null)
    } finally {
        loading.value = false
    }
})
</script>
<template>
    <ul class="TreeView" :class="{ spinner: loading }">
        <li
            v-for="entry in visibleNodes"
            :key="entry.id"
            class="TreeView__node"
            :class="{
                'TreeView__node--selected': selection.selected.has(entry.id),
                'TreeView__node--leaf': !entry.expandable
            }"
            @click="onRowClick(entry, $event)"
        >
            <span v-for="(guide, i) in entry.guides" :key="i" class="TreeView__guide" :class="`TreeView__guide--${guide}`" />
            <button v-if="entry.expandable" type="button" class="TreeView__toggle" :class="{ spinner: childLoading.has(entry.id) }" @click.stop="toggleExpand(entry)">
                <svg v-if="!childLoading.has(entry.id)" class="TreeView__icon" viewBox="0 0 16 16" aria-hidden="true">
                    <line x1="4" y1="8" x2="12" y2="8" />
                    <line v-if="!expanded.has(entry.id)" x1="8" y1="4" x2="8" y2="12" />
                </svg>
            </button>
            <span v-else class="TreeView__spacer" />
            <span class="TreeView__label">{{ entry.node[labelField] }}</span>
        </li>
    </ul>
</template>
<style lang="scss">
// One column width, shared by guide cells, the toggle box, and the leaf spacer so
// connectors line up under their parent's box exactly.
$indent: 16px;
$line-color: var(--tree-line-color, var(--border-color-light));

.TreeView {
    list-style: none;
    margin: 0;
    padding: 0;
    user-select: none;

    &.spinner {
        min-height: 40px;
    }

    &__node {
        display: flex;
        align-items: stretch;
        // Reset the site's global list-item margin; the rows must abut so the
        // connector lines run continuously from one to the next.
        margin: 0;
        cursor: default;
        white-space: nowrap;

        &:hover {
            background-color: var(--form-background-hover);
        }
        &--selected,
        &--selected:hover {
            // Neutral gray band (classic tree look); themeable via the shared token.
            background-color: var(--row-select-color, rgba(127, 127, 127, 0.32));
        }
    }

    // One indentation column. Lines are centered on the column (left/top 50%, a
    // 1px line pulled back half its width) so they sit exactly on the box centre
    // and the box's glyph strokes: a vertical (::before) and, for elbows, a
    // horizontal stub (::after) reaching the node's box.
    &__guide {
        position: relative;
        flex-shrink: 0;
        width: $indent;

        &--through::before,
        &--elbow::before,
        &--elbow-last::before {
            content: '';
            position: absolute;
            top: 0;
            left: 50%;
            width: 1px;
            margin-left: -0.5px;
            background: $line-color;
        }
        // Sibling line passing through, and the ├ elbow: full height.
        &--through::before,
        &--elbow::before {
            bottom: 0;
        }
        // The └ elbow (last child): vertical stops at the connector.
        &--elbow-last::before {
            height: 50%;
        }
        &--elbow::after,
        &--elbow-last::after {
            content: '';
            position: absolute;
            top: 50%;
            margin-top: -0.5px;
            left: 50%;
            right: 0;
            height: 1px;
            background: $line-color;
        }
    }

    // A leaf has no box, just an aligning spacer in the box slot. Extend the
    // elbow's horizontal across that slot so the line reaches the label rather
    // than stopping short where a box would have been.
    &__node--leaf &__guide--elbow::after,
    &__node--leaf &__guide--elbow-last::after {
        right: -$indent;
    }

    // +/- box: a sharp square centered on the node's vertical spine. box-sizing is
    // border-box (global), so width/height include the 1px border → a true 16×16
    // square; padding:0 overrides the app's global button padding.
    &__toggle {
        align-self: center;
        flex-shrink: 0;
        width: $indent;
        height: $indent;
        padding: 0;
        display: inline-flex;
        align-items: center;
        justify-content: center;
        border: 1px solid var(--border-color-light);
        background: var(--form-background);
        color: var(--font-color);
        cursor: default;

        &.spinner::before {
            width: 12px;
            height: 12px;
            margin-top: -6px;
            margin-left: -6px;
        }
    }
    // Plus/minus as centered strokes: the '+' vertical sits on the box centre
    // (= the connector spine), its horizontal on the row centre.
    &__icon {
        display: block;
        width: 100%;
        height: 100%;
        stroke: currentColor;
        stroke-width: 1.5;
    }
    // Keeps leaf labels aligned with their expandable siblings'.
    &__spacer {
        flex-shrink: 0;
        width: $indent;
    }
    &__label {
        align-self: center;
        flex-grow: 1;
        overflow: hidden;
        text-overflow: ellipsis;
        // Row height lives here (not in node padding) so the guide cells stretch
        // the full row and the connector lines run gaplessly between rows.
        line-height: 20px;
        padding-inline-start: var(--space-1);
        padding-inline-end: var(--space-2);
    }
}
</style>
