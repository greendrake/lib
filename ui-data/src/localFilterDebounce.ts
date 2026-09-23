import type { Ref } from 'vue'

// A SearchField filtering an InfiniteScrollTable should react swiftly when the
// table can satisfy the change from its in-memory set (no API call), but coalesce
// keystrokes when the change triggers a remote fetch. These are the two delays.
const LOCAL_MS = 250
const REMOTE_MS = 600

// The slice of an InfiniteScrollTable's exposed surface this helper needs: its
// own answer to whether a prospective text filter resolves locally.
export interface LocalFilterable {
    willFilterLocally(value: string | undefined): boolean
}

// Builds SearchField's per-keystroke `debounce` resolver from the table's template
// ref. Before the table mounts (ref still empty), and whenever the table reports it
// can't serve the change locally, it yields the longer remote delay.
export const localFilterDebounce =
    <T extends LocalFilterable>(table: Ref<T | undefined>) =>
    (value: string | undefined): number =>
        table.value?.willFilterLocally(value) ? LOCAL_MS : REMOTE_MS
