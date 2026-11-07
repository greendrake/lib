import { existsSync, readFileSync, realpathSync } from 'node:fs'
import path from 'node:path'

// A package graph, walked once for whoever asks about it: what a package
// declares, how the packages it names are found, and which of its edges a
// question follows. Node builtins only, so the walk runs in a vite config and
// in tooling with nothing installed alike.

// The parts of a package.json the walk reads.
export interface Manifest {
    dependencies?: Record<string, string>
    devDependencies?: Record<string, string>
    peerDependencies?: Record<string, string>
    exports?: string | Record<string, unknown>
}

// The kinds of edge a package.json declares.
export type Edge = 'dependencies' | 'devDependencies' | 'peerDependencies'

// What ships: the dependencies, each package's own in turn.
export const RUNTIME: readonly Edge[] = ['dependencies']
// What builds and tests it besides: every package's dev dependencies too.
export const BUILD: readonly Edge[] = ['dependencies', 'devDependencies']

// A package as found: its directory and its package.json.
export interface Package<M extends Manifest = Manifest> {
    dir: string
    manifest: M
}

// The package in dir.
export const packageAt = <M extends Manifest>(dir: string): Package<M> => ({ dir, manifest: JSON.parse(readFileSync(path.join(dir, 'package.json'), 'utf-8')) as M })

// How the packages a package names are found.
export interface Resolver<M extends Manifest = Manifest> {
    // Whether an edge to the package `name` is followed at all: a resolver over
    // a workspace's own members follows no third-party package.
    follows: (name: string) => boolean
    // The package `name`, as a dependency of the package in `from`.
    find: (from: string, name: string) => Package<M>
}

// The resolver of an installed tree: the nearest node_modules at or above
// `from` holding the package, as the real path behind whatever link reached
// it. That is where its own dependencies are found from — an isolated install
// (bun's, pnpm's) puts them beside the package's real directory, not beside the
// link an app's node_modules holds — and it is what Node and Vite resolve from
// too. A package declared and not installed is an error, never a gap.
export const installed = <M extends Manifest>(): Resolver<M> => ({
    follows: () => true,
    find: (from, name) => {
        for (let dir = from; ; dir = path.dirname(dir)) {
            const candidate = path.join(dir, 'node_modules', name)
            if (existsSync(path.join(candidate, 'package.json'))) return packageAt<M>(realpathSync(candidate))
            if (path.dirname(dir) === dir) throw new Error(`${name} is declared but not installed anywhere above ${from}`)
        }
    }
})

// One edge the walk reached: the package it names, by the chain of packages it
// was reached through — from a root's own dependency down to its dependent, the
// roots themselves left out — and the kind of edge it is.
export interface Reached<M extends Manifest = Manifest> {
    name: string
    chain: readonly string[]
    edge: Edge
    // The package, found on first use: what a visitor never asks about is
    // never looked up — an optional peer nobody installed among it.
    readonly package: Package<M>
}

// Says, for every edge reached, whether the walk goes on into the package's
// own edges. A package is walked into once, however many edges reach it; each
// edge is still visited.
export type Visitor<M extends Manifest = Manifest> = (reached: Reached<M>) => boolean

// walk visits every edge of the kinds in `edges` that the resolver follows,
// from the packages in `roots` and from each package a visitor goes on into,
// in declaration order.
export const walk = <M extends Manifest>(roots: Package<M>[], edges: readonly Edge[], resolver: Resolver<M>, visit: Visitor<M>): void => {
    const walked = new Set<string>()
    const into = (pkg: Package<M>, chain: readonly string[]): void => {
        walked.add(pkg.dir)
        for (const edge of edges) {
            for (const name of Object.keys(pkg.manifest[edge] ?? {}).filter(resolver.follows)) {
                let found: Package<M> | undefined
                const reached: Reached<M> = {
                    name,
                    chain,
                    edge,
                    get package() {
                        found ??= resolver.find(pkg.dir, name)
                        return found
                    }
                }
                if (visit(reached) && !walked.has(reached.package.dir)) into(reached.package, [...chain, name])
            }
        }
    }
    for (const root of roots) into(root, [])
}

// closure is the packages in `roots` and every package reachable from them over
// `edges` that the resolver follows, each once, in the order the walk first
// reaches it.
export const closure = <M extends Manifest>(roots: string[], edges: readonly Edge[], resolver: Resolver<M>): Package<M>[] => {
    const packages = new Map(roots.map(dir => [dir, packageAt<M>(dir)]))
    walk([...packages.values()], edges, resolver, ({ package: pkg }) => {
        if (!packages.has(pkg.dir)) packages.set(pkg.dir, pkg)
        return true
    })
    return [...packages.values()]
}
