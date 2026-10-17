<script lang="ts">
import type { Directive } from 'vue'

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

/** One place a node shows: under a parent, or at the root level (null). A node with two parents shows twice. */
export interface TreeOccurrence {
    node: TreeNodeId
    parent: TreeNodeId | null
}

/**
 * A dragged occurrence dropped on a row: inside it (the node goes under the
 * row's node), or before/after it among the row's siblings.
 */
export type TreeMove = { node: TreeNodeId; from: TreeNodeId | null } & ({ position: 'inside'; to: TreeNodeId } | { position: 'before' | 'after'; to: TreeNodeId | null; relativeTo: TreeNodeId })

type DropPosition = TreeMove['position']

type Guide = 'through' | 'blank' | 'elbow' | 'elbow-last'

type Row = {
    // The row's path of ids from its root, as a key: unique per row.
    key: string
    depth: number
    guides: Guide[]
}

/** A node's row: one occurrence of it. */
type NodeRow = Row & {
    id: TreeNodeId
    parent: TreeNodeId | null
    node: TreeNode
    expandable: boolean
}

/** The input of a new child, last under its parent's row. */
type NewRow = Row & { parent: TreeNodeId }

// Joins a path's ids; no id carries the unit separator. A new child's row
// ends its parent's path in an empty id, which no node has.
const pathSeparator = '\u001f'
const pathKey = (parentKey: string | null, id: TreeNodeId): string => (parentKey === null ? String(id) : parentKey + pathSeparator + String(id))
const newRowKey = (parentKey: string): string => pathKey(parentKey, '')

// Focuses an in-place input as it shows, its text selected.
const vSelectOnMount: Directive<HTMLInputElement> = {
    mounted: el => {
        el.focus()
        el.select()
    }
}
</script>
<script setup lang="ts">
// Generic lazy tree. Children are loaded on demand via `loader` and cached per
// node; expanding/collapsing only re-walks the cache. The whole tree is rendered
// as one flat list of the currently-visible nodes (each carrying its depth), so
// selection is a flat, index-addressable problem handled by the shared
// useSelection composable — the same one the data table uses: click = single,
// shift = range, ctrl/cmd = toggle, the arrows move the cursor (shift
// extending the range), ctrl/cmd+A selects all; there are no checkboxes. The
// selected node ids are exposed as a v-model array.
//
// A node may have several parents: it then shows under each, every row keyed
// by its path from the root, while the children cache, expansion and selection
// stay per node. The cursor's row is the focused occurrence — which node, under
// which parent. With `draggable`, a row dropped on another emits `move`. With
// onRename, a double-clicked label turns into an input; with onAddChild,
// addChild() opens one for a new child.
import { ref, computed, reactive, watch, onMounted, nextTick } from 'vue'
import useSelection from './useSelection'

const props = withDefaults(
    defineProps<{
        loader: TreeLoader
        idField?: string
        labelField?: string
        /** The field a rename edits; the label's by default. */
        editField?: string
        multiSelect?: boolean
        draggable?: boolean
        /** Saves a rename; a rejection keeps the input open. */
        onRename?: (node: TreeNodeId, value: string) => Promise<void>
        /** Saves a new child; a rejection keeps the input open. */
        onAddChild?: (parent: TreeNodeId, value: string) => Promise<void>
    }>(),
    {
        idField: 'key',
        labelField: 'title',
        multiSelect: true,
        draggable: false
    }
)

const emit = defineEmits<{
    move: [move: TreeMove]
    /** A row was right-clicked, after it became the focused one, selected. */
    contextmenu: [event: MouseEvent]
}>()

const selected = defineModel<TreeNodeId[]>('selected', { default: () => [] })

const roots = ref<TreeNode[]>([])
const loading = ref(false)
// id -> child node[]; presence means "already loaded".
const childrenCache = reactive(new Map<TreeNodeId, TreeNode[]>())
const expanded = reactive(new Set<TreeNodeId>())
const childLoading = reactive(new Set<TreeNodeId>())

// The row whose input shows — a node's being renamed, or a new child's —
// pending while the consumer saves it.
const editing = ref<{ key: string; pending: boolean } | null>(null)

// Depth-first flatten of every currently-visible row: what the template
// renders. Its node rows are the index space useSelection operates on (range
// selection walks contiguous rows there).
//
// Each row also carries the `guides` it needs to draw the connector lines: one
// cell per indentation column. `ancestorVerticals[i]` (threaded down the walk)
// says whether the ancestor sitting in column i still has a sibling below — i.e.
// whether a vertical line passes through this row at that column. The row's own
// (rightmost) column is the elbow: '├' when more siblings follow, '└' when last.
// A new child's input closes its level, after the nodes.
//
// A node's loaded children are authoritative for whether it expands: a reload
// that empties or fills its list outdates the flag its parent's load carried.
const rows = computed(() => {
    const out: (NodeRow | NewRow)[] = []
    const walk = (nodes: TreeNode[], parent: NodeRow | null, ancestorVerticals: boolean[]) => {
        const depth = parent ? parent.depth + 1 : 0
        const adding = parent !== null && editing.value?.key === newRowKey(parent.key)
        const count = nodes.length + (adding ? 1 : 0)
        const guidesAt = (idx: number): Guide[] => {
            const guides: Guide[] = ancestorVerticals.map(v => (v ? 'through' : 'blank'))
            if (depth > 0) guides.push(idx === count - 1 ? 'elbow-last' : 'elbow')
            return guides
        }
        nodes.forEach((node, idx) => {
            const id = node[props.idField] as TreeNodeId
            const children = childrenCache.get(id)
            const row: NodeRow = {
                key: pathKey(parent?.key ?? null, id),
                id,
                parent: parent?.id ?? null,
                node,
                depth,
                expandable: children ? children.length > 0 : node.expandable,
                guides: guidesAt(idx)
            }
            out.push(row)
            if (expanded.has(id) && children) {
                walk(children, row, depth === 0 ? [] : [...ancestorVerticals, idx < count - 1])
            }
        })
        if (adding) {
            out.push({
                key: newRowKey(parent.key),
                parent: parent.id,
                depth,
                guides: guidesAt(nodes.length)
            })
        }
    }
    walk(roots.value, null, [])
    return out
})

const nodeRows = computed(() => rows.value.filter((row): row is NodeRow => 'node' in row))

const selection = useSelection<TreeNodeId>(nodeRows, 'id', { keyField: 'key', multi: () => props.multiSelect })

// The cursor's row and its occurrence, while visible.
const focusedRow = computed(() => nodeRows.value.find(row => row.key === selection.cursor.value) ?? null)
const focused = computed<TreeOccurrence | null>(() => focusedRow.value && { node: focusedRow.value.id, parent: focusedRow.value.parent })

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

async function loadChildren(id: TreeNodeId): Promise<void> {
    childLoading.add(id)
    try {
        childrenCache.set(id, await props.loader(id))
    } finally {
        childLoading.delete(id)
    }
}

async function loadRoots(): Promise<void> {
    loading.value = true
    try {
        roots.value = await props.loader(null)
    } finally {
        loading.value = false
    }
}

/** Re-fetches a node's children (null: the root level), keeping what is expanded. */
async function reload(nodeId: TreeNodeId | null): Promise<void> {
    await (nodeId === null ? loadRoots() : loadChildren(nodeId))
}

/** The loaded levels holding the node: the ids of the parents it shows under, null for the root level. */
function levelsOf(nodeId: TreeNodeId): (TreeNodeId | null)[] {
    const holds = (nodes: TreeNode[]): boolean => nodes.some(n => n[props.idField] === nodeId)
    const levels: (TreeNodeId | null)[] = holds(roots.value) ? [null] : []
    for (const [parent, children] of childrenCache) {
        if (holds(children)) levels.push(parent)
    }
    return levels
}

/** Expands the node wherever it shows, loading its children first if need be. */
async function expand(nodeId: TreeNodeId): Promise<void> {
    if (!childrenCache.has(nodeId)) {
        await loadChildren(nodeId)
    }
    expanded.add(nodeId)
}

async function toggleExpand(row: NodeRow) {
    if (!row.expandable) return
    if (expanded.has(row.id)) {
        expanded.delete(row.id)
        return
    }
    await expand(row.id)
}

// A right-click focuses its row, selecting it alone unless it is selected
// already, before the consumer's menu opens.
function onRowContextMenu(row: NodeRow, e: MouseEvent): void {
    if (selection.selected.has(row.id)) selection.cursor.value = row.key
    else selection.replace(row.key)
    emit('contextmenu', e)
}

const list = ref<HTMLElement>()

// The cursor's row, moved by a key, scrolls into view.
async function onKeyDown(e: KeyboardEvent): Promise<void> {
    if (selection.onKeyDown(e) === -1) return
    await nextTick()
    list.value!.querySelector('[aria-current]')!.scrollIntoView({ block: 'nearest' })
}

// --- in-place editing ---

const editValue = (row: NodeRow | NewRow): string => ('node' in row ? String(row.node[props.editField ?? props.labelField] ?? '') : '')

/** Opens the input of a new child last under the focused row, expanding it first. */
async function addChild(): Promise<void> {
    const row = focusedRow.value!
    await expand(row.id)
    editing.value = { key: newRowKey(row.key), pending: false }
}

// Enter saves the input through the consumer — a rename that changes the
// value, a new child that has one — and Escape drops it; the input keeps its
// keys from the tree's. A failed save leaves the input open, focused again.
async function onEditKey(row: NodeRow | NewRow, e: KeyboardEvent): Promise<void> {
    e.stopPropagation()
    if (e.key === 'Escape') {
        editing.value = null
        return
    }
    if (e.key !== 'Enter') return
    const input = e.target as HTMLInputElement
    const value = input.value.trim()
    if (value === editValue(row)) {
        editing.value = null
        return
    }
    const edit = editing.value!
    edit.pending = true
    try {
        await ('node' in row ? props.onRename!(row.id, value) : props.onAddChild!(row.parent, value))
        editing.value = null
    } catch (err) {
        edit.pending = false
        await nextTick()
        input.focus()
        throw err
    }
}

// Leaving the input drops it, unless it is saving.
function onEditBlur(): void {
    if (!editing.value?.pending) editing.value = null
}

// --- dragging ---

const dragged = ref<NodeRow | null>(null)
const dropTarget = ref<{ key: string; position: DropPosition } | null>(null)

// Only the row itself drags: a text drag out of its input is no move.
function onDragStart(row: NodeRow, e: DragEvent): void {
    if (e.target !== e.currentTarget) return
    dragged.value = row
    e.dataTransfer!.effectAllowed = 'move'
    e.dataTransfer!.setData('text/plain', String(row.id))
}

// Where over the row the pointer is: its upper third before the row, its
// lower third after it, the middle inside. A row whose path passes through
// the dragged node — any occurrence of it, or any row under one — is no
// target: the drop would put the node under itself.
function dropPosition(row: NodeRow, e: DragEvent): DropPosition | null {
    if (!dragged.value || row.key.split(pathSeparator).includes(String(dragged.value.id))) return null
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    const at = (e.clientY - rect.top) / rect.height
    return at < 1 / 3 ? 'before' : at > 2 / 3 ? 'after' : 'inside'
}

function onDragOver(row: NodeRow, e: DragEvent): void {
    const position = dropPosition(row, e)
    if (!position) {
        dropTarget.value = null
        return
    }
    e.preventDefault()
    e.dataTransfer!.dropEffect = 'move'
    dropTarget.value = { key: row.key, position }
}

function onDragLeave(row: NodeRow): void {
    if (dropTarget.value?.key === row.key) dropTarget.value = null
}

function onDrop(row: NodeRow, e: DragEvent): void {
    const position = dropPosition(row, e)
    const head = dragged.value
    onDragEnd()
    if (!position || !head) return
    e.preventDefault()
    const from = { node: head.id, from: head.parent }
    emit(
        'move',
        position === 'inside'
            ? {
                  ...from,
                  position,
                  to: row.id
              }
            : {
                  ...from,
                  position,
                  to: row.parent,
                  relativeTo: row.id
              }
    )
}

function onDragEnd(): void {
    dragged.value = null
    dropTarget.value = null
}

defineExpose({
    reload,
    expand,
    levelsOf,
    focused,
    addChild
})

onMounted(loadRoots)
</script>
<template>
    <ul ref="list" class="TreeView" :class="{ spinner: loading }" tabindex="0" @keydown="onKeyDown">
        <template v-for="row in rows" :key="row.key">
            <li
                v-if="'node' in row"
                class="TreeView__node"
                :class="{
                    'TreeView__node--selected': selection.selected.has(row.id),
                    'TreeView__node--leaf': !row.expandable
                }"
                :aria-current="row.key === selection.cursor.value || undefined"
                :draggable="(draggable && editing?.key !== row.key) || undefined"
                :data-drop="dropTarget?.key === row.key ? dropTarget.position : undefined"
                @click="selection.pick(row.key, $event)"
                @contextmenu="onRowContextMenu(row, $event)"
                @dragstart="onDragStart(row, $event)"
                @dragover="onDragOver(row, $event)"
                @dragleave="onDragLeave(row)"
                @drop="onDrop(row, $event)"
                @dragend="onDragEnd"
            >
                <span v-for="(guide, i) in row.guides" :key="i" class="TreeView__guide" :class="`TreeView__guide--${guide}`" />
                <button v-if="row.expandable" type="button" class="TreeView__toggle" :class="{ spinner: childLoading.has(row.id) }" @click.stop="toggleExpand(row)">
                    <svg v-if="!childLoading.has(row.id)" class="TreeView__icon" viewBox="0 0 16 16" aria-hidden="true">
                        <line x1="4" y1="8" x2="12" y2="8" />
                        <line v-if="!expanded.has(row.id)" x1="8" y1="4" x2="8" y2="12" />
                    </svg>
                </button>
                <span v-else class="TreeView__spacer" />
                <input v-if="editing?.key === row.key" v-select-on-mount class="TreeView__input" :value="editValue(row)" :disabled="editing.pending" @keydown="onEditKey(row, $event)" @blur="onEditBlur" @click.stop @contextmenu.stop />
                <span v-else class="TreeView__label" @dblclick="onRename && (editing = { key: row.key, pending: false })">{{ row.node[labelField] }}</span>
            </li>
            <!-- A new child's input: its row holds nothing else to act on. -->
            <li v-else class="TreeView__node TreeView__node--leaf">
                <span v-for="(guide, i) in row.guides" :key="i" class="TreeView__guide" :class="`TreeView__guide--${guide}`" />
                <span class="TreeView__spacer" />
                <input v-select-on-mount class="TreeView__input" :disabled="editing!.pending" @keydown="onEditKey(row, $event)" @blur="onEditBlur" />
            </li>
        </template>
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
    outline: none;

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
        padding: 0 var(--space-1);

        &:hover {
            background-color: var(--form-background-hover);
        }
        &--selected,
        &--selected:hover {
            // Neutral gray band (classic tree look); themeable via the shared token.
            background-color: var(--row-select-color, rgba(127, 127, 127, 0.32));
        }

        // Drop targets while dragging: a line on the edge the node lands
        // beside, an outline around the row it lands under.
        &[data-drop='before'] {
            box-shadow: inset 0 2px 0 var(--accent-color);
        }
        &[data-drop='after'] {
            box-shadow: inset 0 -2px 0 var(--accent-color);
        }
        &[data-drop='inside'] {
            box-shadow: inset 0 0 0 1px var(--accent-color);
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
        padding: var(--space-1);
        text-overflow: ellipsis;
        // Row height lives here (not in node padding) so the guide cells stretch
        // the full row and the connector lines run gaplessly between rows.
        line-height: 20px;
        padding-inline-start: var(--space-1);
        padding-inline-end: var(--space-2);
    }
    // In the label's place, as tall as its row; its text selects as the
    // tree's does not.
    &__input {
        align-self: center;
        flex-grow: 1;
        min-width: 0;
        margin: 0;
        margin-inline-end: var(--space-2);
        padding: var(--space-1);
        border: 1px solid var(--border-color);
        border-radius: 0;
        background: var(--form-background);
        color: var(--font-color);
        font: inherit;
        line-height: 18px;
        user-select: text;
    }
}
</style>
