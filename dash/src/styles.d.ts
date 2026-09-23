// A stylesheet imported for effect resolves to nothing at the type level, and
// TypeScript 6 checks that it resolves at all (noUncheckedSideEffectImports).
// Declared here rather than left to the consumer's vite/client: this package
// ships source, so whatever compiles it — an app, another package, an outside
// project — must be told what the import means by the package that makes it.
declare module '*.scss'
