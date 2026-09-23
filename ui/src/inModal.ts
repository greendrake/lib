import type { InjectionKey } from 'vue'

// Modal provides this so descendants can tell they render inside a modal
// window rather than the page. Surfaces that pin themselves to the viewport
// (e.g. AsyncSearch's narrow-viewport results takeover) are wrong inside one:
// the sheet already owns the screen, sits above them in the stacking order,
// and leaves them no room when it is pinned to the bottom edge.
export const inModalKey: InjectionKey<true> = Symbol('inModal')
