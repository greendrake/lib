// The design-token gate: colour, radius, spacing, shadow and font-size
// declarations must resolve to a token (var(--…)), not a hardcoded literal, so
// drift can't creep in. Runs over an app's SCSS and the SCSS in Vue <style>
// blocks.
//
// `ignoreFunctions` (plugin default true) already exempts color-mix()/rgba()/
// gradients/calc() — only bare literals (#fff, 4px, 16px) are caught. Property
// lists are explicit (not a broad /color/) so they never match custom-property
// *definitions* like `--font-color: #222`, which are the tokens themselves.
//
// The plugins are named, not imported: stylelint resolves them from the
// consumer's config file, whose package installs them.

const STRICT_PROPS = [
    'color',
    'background',
    'background-color',
    'border-color',
    '/^border-(top|right|bottom|left)-color$/',
    'outline-color',
    'fill',
    'box-shadow',
    'border-radius',
    '/^border-(top|bottom)-(left|right)-radius$/',
    'font-size',
    '/^(padding|margin)/', // shorthand + per-side longhands
    'gap',
    'row-gap',
    'column-gap'
]

export default {
    plugins: ['stylelint-declaration-strict-value', 'stylelint-high-performance-animation'],
    overrides: [
        { files: ['**/*.scss'], customSyntax: 'postcss-scss' },
        { files: ['**/*.vue'], customSyntax: 'postcss-html' }
    ],
    rules: {
        'scale-unlimited/declaration-strict-value': [
            STRICT_PROPS,
            {
                // Bare literals that are legitimately not tokens: CSS-wide
                // keywords; zero and percentages; pure black/white; negative
                // values (intentional overlaps); 1–2px hairlines.
                ignoreValues: ['currentColor', 'transparent', 'inherit', 'initial', 'unset', 'none', 'auto', '/^0[a-z%]*$/', '/%$/', '#fff', '#000', 'white', 'black', '/^-/', '/^[12]px$/', '/^[0-9.]+r?em$/'],
                ignoreFunctions: true,
                disableFix: true
            }
        ],
        // Only flag animations of layout-triggering properties (the costly ones);
        // paint-only transitions (background/colour/filter) are acceptable here.
        'plugin/no-low-performance-animation-properties': [true, { ignore: 'paint-properties' }]
    }
}
