import { installed, packageAt, RUNTIME, walk, type Manifest, type Reached } from './deps'

export type { Manifest } from './deps'

// What an app declares, split the way the app itself splits it: what it ships
// to the browser, and what only its tooling reaches.
export interface DeclaredDependencies {
    runtime: string[]
    dev: string[]
}

// An `optimizeDeps` entry names a package, so a package published only under
// subpaths (it maps every entry it has, and none of them is `.`) cannot be one.
// Nor does it need to be: Vite meets such a package through the subpath ids that
// import it, and whatever pre-bundles those carries it along.
const hasRootExport = (pkg: Manifest): boolean => {
    if (pkg.exports === undefined || typeof pkg.exports === 'string') return true
    const keys = Object.keys(pkg.exports)
    return keys.includes('.') || !keys.some(key => key.startsWith('.'))
}

// The third-party packages a source package imports, as `optimizeDeps.include`
// entries.
//
// Vite's dependency scanner externalises every id in `optimizeDeps.exclude`, so
// it never reads a source package and never sees what that package imports. The
// first browser request for one of those imports is then a discovery: Vite
// re-runs the optimiser mid-session and full-reloads every open page. Under a
// parallel E2E run that reload lands in the middle of whatever tests are in
// flight, and they fail on a page that vanished under them. Naming the imports
// up front makes the first optimisation complete instead.
//
// The walk starts at the app's runtime dependencies and goes on into source
// packages only, since the scanner reads everything else itself.
export const nestedDependencies = (dirname: string, declared: DeclaredDependencies, isSourcePackage: (name: string) => boolean): string[] => {
    const entries = new Set<string>()
    // A package the app declares itself resolves from the app root, and its
    // plain name is the id the scanner would produce for it; naming it nested
    // as well would pre-bundle the same module twice, once per id. Anything
    // else is named in Vite's `a > b > c` form, and names the whole chain from
    // the dependency the app declares: Vite resolves each segment from the one
    // before it, starting at the app root, and an intermediate package the app
    // never declares need not be reachable from there. A package the app
    // declares anchors the chain of whatever is reached through it, however it
    // was reached itself: without that, the chain a package gets would depend
    // on which branch of the walk arrived first.
    const pin = ({ name, chain, package: pkg }: Reached): void => {
        if (!hasRootExport(pkg.manifest)) return
        const anchor = chain.findLastIndex(link => declared.runtime.includes(link))
        entries.add(declared.runtime.includes(name) ? name : [...chain.slice(anchor), name].join(' > '))
    }
    walk([packageAt(dirname)], [...RUNTIME, 'peerDependencies'], installed(), reached => {
        const { name, chain, edge } = reached
        // A source package the app keeps among its dev dependencies is tooling
        // by the app's own reckoning — a vite integration a package re-exports
        // beside its components, a native-build helper — and what such a
        // package carries is unbundleable for a browser that never reaches it
        // anyway.
        if (edge === 'dependencies' && isSourcePackage(name)) return !declared.dev.includes(name)
        // The app's own third-party dependencies, and its own peers, are the
        // scanner's to see. A peer is the app's to supply, so a source
        // package's counts only where the app does, at runtime — the same
        // reckoning, applied to what a package leaves to its consumer. A source
        // package among them is served raw like any other, and walked as the
        // app's own declaration of it is.
        if (chain.length > 0 && (edge === 'dependencies' || (declared.runtime.includes(name) && !isSourcePackage(name)))) pin(reached)
        return false
    })
    return [...entries]
}
