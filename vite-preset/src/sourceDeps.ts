import { existsSync, readFileSync } from 'node:fs'
import path from 'node:path'

// The parts of a package.json this walk reads.
export interface Manifest {
    dependencies?: Record<string, string>
    peerDependencies?: Record<string, string>
    exports?: string | Record<string, unknown>
}

// What an app declares, split the way the app itself splits it: what it ships
// to the browser, and what only its tooling reaches.
export interface DeclaredDependencies {
    runtime: string[]
    dev: string[]
}

// Where the installer put a package: the nearest node_modules at or above
// `from` holding it.
const packageDir = (from: string, name: string): string => {
    for (let dir = from; ; dir = path.dirname(dir)) {
        const candidate = path.join(dir, 'node_modules', name)
        if (existsSync(path.join(candidate, 'package.json'))) return candidate
        if (path.dirname(dir) === dir) throw new Error(`${name} is declared but not installed anywhere above ${from}`)
    }
}

const manifest = (dir: string): Manifest => JSON.parse(readFileSync(path.join(dir, 'package.json'), 'utf-8')) as Manifest

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
// The walk starts at the app's runtime dependencies and follows source packages
// only, since the scanner reads everything else itself.
export const nestedDependencies = (dirname: string, declared: DeclaredDependencies, isSourcePackage: (name: string) => boolean): string[] => {
    const entries = new Set<string>()
    const visited = new Set<string>()
    const pin = (chain: string[], from: string, name: string): void => {
        if (!hasRootExport(manifest(packageDir(from, name)))) return
        // A package the app declares itself resolves from the app root, and its
        // plain name is the id the scanner would produce for it; naming it
        // nested as well would pre-bundle the same module twice, once per id.
        // Anything else is named in Vite's `a > b > c` form, and names the whole
        // chain from the dependency the app declares: Vite resolves each segment
        // from the one before it, starting at the app root, and an intermediate
        // package the app never declares need not be reachable from there.
        entries.add(declared.runtime.includes(name) ? name : [...chain, name].join(' > '))
    }
    const walk = (name: string, chain: string[], from: string): void => {
        // A source package the app keeps among its dev dependencies is tooling
        // by the app's own reckoning — a vite integration a package re-exports
        // beside its components, a native-build helper — and what such a package
        // carries is unbundleable for a browser that never reaches it anyway.
        if (visited.has(name) || declared.dev.includes(name)) return
        visited.add(name)
        const dir = packageDir(from, name)
        const pkg = manifest(dir)
        // A package the app declares resolves from the app root, so it anchors
        // its own chain however it was reached. Without that, the chain a
        // package gets would depend on which branch of the walk arrived first.
        const here = declared.runtime.includes(name) ? [name] : [...chain, name]
        for (const dep of Object.keys(pkg.dependencies ?? {})) {
            if (isSourcePackage(dep)) walk(dep, here, dir)
            else pin(here, dir, dep)
        }
        // A peer is the app's to supply, so it counts only where the app does,
        // at runtime — the same reckoning, applied to what a package leaves to
        // its consumer.
        for (const peer of Object.keys(pkg.peerDependencies ?? {})) {
            if (declared.runtime.includes(peer)) pin(here, dirname, peer)
        }
    }
    for (const name of declared.runtime) {
        if (isSourcePackage(name)) walk(name, [], dirname)
    }
    return [...entries]
}
