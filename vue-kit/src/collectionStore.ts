import { computed, ref, type Ref } from 'vue'
import { defineStore } from 'pinia'
import type { Comparator } from '@greendrake/util'

export interface CollectionStoreOptions<T, K extends PropertyKey = never> {
    load: () => Promise<T[]>
    // Applied after every load so lists are stable across mounts.
    compare?: Comparator<T>
    // Enables the `byKey` computed index for O(1) formatter lookups.
    indexBy?: (item: T) => K
}

// Factory for shared, lazily-loaded, request-deduplicated reference
// collections (VLANs, policies, saved searches, …). `items` is a plain
// reactive array — imperative row surgery is ordinary array mutation.
// ensure() loads once per session, deduping concurrent callers via the
// in-flight promise; mutating callers invalidate() so the next ensure()
// re-fetches. Return type is pinia's (refs unwrap on the store instance).
export const defineCollectionStore = <T, K extends PropertyKey = never>(id: string, options: CollectionStoreOptions<T, K>) =>
    defineStore(id, () => {
        const items = ref<T[]>([]) as Ref<T[]>
        let inFlight: Promise<void> | null = null

        const ensure = (): Promise<void> => {
            if (!inFlight) {
                inFlight = (async () => {
                    const loaded = await options.load()
                    items.value = options.compare ? [...loaded].sort(options.compare) : loaded
                })()
            }
            return inFlight
        }

        const invalidate = (): void => {
            inFlight = null
        }

        const byKey = computed(() => {
            const index = {} as Record<K, T>
            if (options.indexBy) {
                for (const item of items.value) {
                    index[options.indexBy(item)] = item
                }
            }
            return index
        })

        return {
            items,
            byKey,
            ensure,
            invalidate
        }
    })
