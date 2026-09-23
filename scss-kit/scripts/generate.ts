// Emits _breakpoints.scss and src/breakpoints.ts from src/breakpoints.json so the
// SCSS and TS breakpoint definitions (values AND boundary semantics) stay single-sourced.
import breakpoints from '../src/breakpoints.json'

const names = Object.keys(breakpoints) as (keyof typeof breakpoints)[]

const header = `GENERATED from src/breakpoints.json by scripts/generate.ts — do not edit, regenerate via \`bun run generate\`.
A breakpoint value is the inclusive upper bound of the viewport range below it:
"below" matches width <= value; "above" matches width >= value + 1px.
narrow (600): the content/wide-screen boundary — a viewport is "wide" from 601px up.`

const comment = header
    .split('\n')
    .map(line => `// ${line}`)
    .join('\n')

const scss = `${comment}
@use 'lookup' as *;

$breakpoints: (
${names.map(name => `    '${name}': ${breakpoints[name]}px`).join(',\n')}
);

@function breakpoint($name) {
    @return lookup($breakpoints, $name, 'breakpoint');
}

@mixin respond-below($name) {
    $max: breakpoint($name);
    @media (max-width: $max) {
        @content;
    }
}

@mixin respond-above($name) {
    $min: breakpoint($name) + 1px;
    @media (min-width: $min) {
        @content;
    }
}

@mixin respond-above-height($name) {
    $min: breakpoint($name) + 1px;
    @media (min-height: $min) {
        @content;
    }
}
`

const ts = `${comment}

export const breakpoints = {
${names.map(name => `    ${name}: ${breakpoints[name]}`).join(',\n')}
} as const

export type BreakpointName = keyof typeof breakpoints

// Media-query strings with the same semantics as the SCSS respond-above/respond-below mixins
export const mediaAbove = (name: BreakpointName): string => \`(min-width: \${breakpoints[name] + 1}px)\`
export const mediaBelow = (name: BreakpointName): string => \`(max-width: \${breakpoints[name]}px)\`
`

await Bun.write(`${import.meta.dir}/../_breakpoints.scss`, scss)
await Bun.write(`${import.meta.dir}/../src/breakpoints.ts`, ts)
