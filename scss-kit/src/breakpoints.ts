// GENERATED from src/breakpoints.json by scripts/generate.ts — do not edit, regenerate via `bun run generate`.
// A breakpoint value is the inclusive upper bound of the viewport range below it:
// "below" matches width <= value; "above" matches width >= value + 1px.
// narrow (600): the content/wide-screen boundary — a viewport is "wide" from 601px up.

export const breakpoints = {
    tooNarrow: 320,
    narrow: 600
} as const

export type BreakpointName = keyof typeof breakpoints

// Media-query strings with the same semantics as the SCSS respond-above/respond-below mixins
export const mediaAbove = (name: BreakpointName): string => `(min-width: ${breakpoints[name] + 1}px)`
export const mediaBelow = (name: BreakpointName): string => `(max-width: ${breakpoints[name]}px)`
