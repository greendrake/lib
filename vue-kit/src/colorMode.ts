import { ref } from 'vue'

// Light/dark mode. The palette binds to the .light/.dark classes on <html>,
// falling back to prefers-color-scheme when neither is set (see theme's
// light-dark mixin); this module owns the classes, the saved preference and
// the isDark state.

// Resolved per call, not once at import: this module is reached through the
// package barrel, so binding it eagerly would make merely importing anything
// from @greendrake/vue-kit require a live document.
const root = (): DOMTokenList => document.documentElement.classList
const prefersDark = (): boolean => matchMedia('(prefers-color-scheme: dark)').matches
const isCurrentlyDark = (): boolean => root().contains('dark') || (!root().contains('light') && prefersDark())

export const isDark = ref(false)

// Apply the saved preference; call at the very top of main.ts — before mount,
// so an explicit choice never flashes the OS-preferred palette first.
export const initColorMode = (): void => {
    const saved = localStorage.getItem('colorMode')
    if (saved) {
        root().add(saved)
    }
    isDark.value = isCurrentlyDark()
    // The OS preference decides the palette whenever no explicit choice has
    // been made, and it can change while the app is running — the CSS follows
    // it through its media query, so anything reading `isDark` has to follow
    // it too or the two drift apart. Harmless under an explicit choice, which
    // isCurrentlyDark() already gives precedence to.
    matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
        isDark.value = isCurrentlyDark()
    })
}

export const toggleColorMode = (): void => {
    const classes = root()
    if (classes.contains('dark')) {
        classes.remove('dark')
        classes.add('light')
    } else if (classes.contains('light')) {
        classes.remove('light')
        classes.add('dark')
    } else {
        classes.add(prefersDark() ? 'light' : 'dark')
    }
    isDark.value = isCurrentlyDark()
    localStorage.setItem('colorMode', classes.contains('dark') ? 'dark' : 'light')
}
