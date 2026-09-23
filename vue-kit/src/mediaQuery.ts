import { onUnmounted, ref, type Ref } from 'vue'
import { mediaAbove } from '@greendrake/scss-kit/breakpoints'

// Reactive matchMedia binding; detaches its listener with the component.
export const useMediaQuery = (query: string): Ref<boolean> => {
    const mql = matchMedia(query)
    const matches = ref(mql.matches)
    const onChange = (e: MediaQueryListEvent): void => {
        matches.value = e.matches
    }
    mql.addEventListener('change', onChange)
    onUnmounted(() => mql.removeEventListener('change', onChange))
    return matches
}

// True above the shared content-width boundary — the same value the SCSS
// respond-above('narrow') mixin uses (single source: @greendrake/scss-kit).
export const useWideScreen = (): Ref<boolean> => useMediaQuery(mediaAbove('narrow'))
