import js from '@eslint/js'
import tseslint from 'typescript-eslint'
import vue from 'eslint-plugin-vue'
import prettier from 'eslint-config-prettier'
import stylistic from '@stylistic/eslint-plugin'
import globals from 'globals'

// The flat config for a TypeScript + Vue project. Re-export it directly, or
// spread it and append overrides:
//   import greendrake from '@greendrake/eslint-config'
//   export default [...greendrake, { rules: { ... } }]
export default [
    js.configs.recommended,
    ...tseslint.configs.recommended,
    ...vue.configs['flat/recommended'],
    prettier,
    {
        languageOptions: {
            // Browser AND node: the same workspace mixes app code with its
            // tooling (vite/playwright configs, pdf printers, workers, tests).
            // TS files get real undefined-name checking from tsc regardless.
            globals: { ...globals.browser, ...globals.node },
            ecmaVersion: 'latest',
            sourceType: 'module',
            parserOptions: {
                parser: tseslint.parser,
                extraFileExtensions: ['.vue']
            }
        },
        plugins: { '@stylistic': stylistic },
        rules: {
            // Structural line-breaking. Prettier (printWidth 9999) never wraps
            // for width, so readability of big literals is governed here, by
            // SHAPE: an object literal with 3+ properties (or any multiline
            // content) breaks open, one property per line. Array breaking then
            // falls out of prettier's own printer (an array holding a
            // multiline element goes one-element-per-line), so no array rules
            // are needed — and must not be added: their forbid direction
            // collapses deliberate hand-broken lists, which prettier
            // preserves. The pair below is require-only (`consistent: true`
            // tolerates objects expanded by hand below the property
            // threshold), so eslint --fix and prettier -w reach a stable
            // fixpoint. These live AFTER the eslint-config-prettier entry,
            // which switches @stylistic rules off wholesale — only
            // width-independent, require-only rules may be re-enabled here.
            '@stylistic/object-curly-newline': [
                'error',
                {
                    ObjectExpression: {
                        minProperties: 3,
                        multiline: true,
                        consistent: true
                    }
                }
            ],
            '@stylistic/object-property-newline': ['error', { allowAllPropertiesOnSameLine: true }],
            // `catch {}` is the deliberate ignore-errors idiom (e.g. probing
            // JSON bodies); an accidental empty block elsewhere is still a bug.
            'no-empty': ['error', { allowEmptyCatch: true }],
            '@typescript-eslint/no-explicit-any': 'error',
            '@typescript-eslint/consistent-type-imports': 'error',
            // Destructure-to-omit (`const { drop: _drop, ...rest } = obj`) is the
            // idiomatic way to strip keys; don't report the omitted bindings.
            // A leading underscore more generally marks a binding kept only to
            // satisfy a shape — a positional parameter holding its slot, a
            // destructured name documenting what the source provides. tsc already
            // exempts _-prefixed parameters under noUnusedParameters, so matching
            // it here stops the two checkers disagreeing about the same name.
            '@typescript-eslint/no-unused-vars': [
                'error',
                {
                    ignoreRestSiblings: true,
                    argsIgnorePattern: '^_',
                    varsIgnorePattern: '^_',
                    caughtErrorsIgnorePattern: '^_'
                }
            ],
            // Deep mutation of a reactive object prop is a deliberate
            // shared-state contract in places (optimistic updates on a
            // parent-owned reactive); only reassigning the prop itself is a bug.
            'vue/no-mutating-props': ['error', { shallowOnly: true }],
            // Keep `:ariaLabel` camelCase on components. The rule already ignores
            // the kebab `aria-*` form, but hyphenating the camelCase one to
            // `:aria-label` breaks vue-tsc: it matches a component's typed
            // `ariaLabel` prop by exact key, and `aria-label` isn't that key.
            'vue/attribute-hyphenation': ['error', 'always', { ignore: ['ariaLabel'] }],
            // Deliberate, endorsed patterns — warnings here would be permanent
            // noise: v-html renders trusted (i18n/content-pipeline) HTML only;
            // optional TS props legitimately model `T | undefined` without a
            // default; small render-helper components live at module scope
            // beside their SFC.
            'vue/no-v-html': 'off',
            'vue/require-default-prop': 'off',
            'vue/one-component-per-file': 'off',
            'vue/multi-word-component-names': 'off',
            'vue/html-indent': 'off',
            'vue/max-attributes-per-line': 'off',
            'vue/singleline-html-element-content-newline': 'off',
            'vue/html-self-closing': 'off'
        }
    },
    {
        // dist* rather than dist: an app emits sibling build outputs beside
        // it — a service-worker bundle, a deploy staging tree — and every one
        // of them is generated code no rule here has an opinion about. A
        // tracked source directory whose name starts with "dist" is not a
        // thing, so the wildcard costs nothing.
        ignores: ['**/dist*/**', '**/node_modules/**', '**/playwright-report/**', '**/test-results/**', '**/.playwright-mcp/**']
    }
]
