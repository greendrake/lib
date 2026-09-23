export default {
    printWidth: 9999,
    tabWidth: 4,
    arrowParens: 'avoid',
    singleQuote: true,
    trailingComma: 'none',
    semi: false,
    overrides: [
        {
            // JSX is the one place the no-wrapping default fails. A Vue
            // template is its own block, so width never governs it; a JSX
            // element is an expression nested inside the code that returns it,
            // and at 9999 a single component with a handful of props and a
            // className list becomes one 500-character line. Structural
            // breaking (the @stylistic pair in the eslint config) cannot help
            // — it governs object literals, not elements. Give JSX a real
            // width and let prettier break it.
            files: ['*.jsx', '*.tsx'],
            options: { printWidth: 120 }
        }
    ]
}
