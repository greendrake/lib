import { afterAll, describe, expect, test } from 'bun:test'
import { mkdirSync, mkdtempSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { vueSpa, type VueSpaOptions } from '../src/main'
import type { Manifest } from '../src/sourceDeps'

interface Fixture {
    dependencies?: Record<string, string>
    devDependencies?: Record<string, string>
    // The manifests node_modules holds, keyed by package name — for the packages
    // whose contents a case turns on. Everything else any manifest names is
    // installed too, with an empty one.
    installed?: Record<string, Manifest>
    // Packages node_modules does not hold — a checkout nobody has installed yet.
    missing?: string[]
}

// An app root is a directory with a package.json and a node_modules: the preset
// reads the app's dependency list to decide what may be pre-bundled, and the
// manifests of the source packages among them to decide what has to be pinned
// into the pre-bundle.
const roots: string[] = []

afterAll(() => roots.forEach(root => rmSync(root, { recursive: true, force: true })))

const appRoot = ({ dependencies = {}, devDependencies = {}, installed = {}, missing = [] }: Fixture): string => {
    const root = mkdtempSync(path.join(os.tmpdir(), 'vite-preset-test-'))
    roots.push(root)
    writeFileSync(
        path.join(root, 'package.json'),
        JSON.stringify({
            name: 'app',
            dependencies,
            devDependencies
        })
    )
    const named = [...Object.keys(dependencies), ...Object.keys(devDependencies), ...Object.values(installed).flatMap(m => [...Object.keys(m.dependencies ?? {}), ...Object.keys(m.peerDependencies ?? {})])]
    const manifests = { ...Object.fromEntries(named.map(name => [name, {}])), ...installed }
    for (const [name, manifest] of Object.entries(manifests)) {
        if (missing.includes(name)) continue
        const dir = path.join(root, 'node_modules', name)
        mkdirSync(dir, { recursive: true })
        writeFileSync(path.join(dir, 'package.json'), JSON.stringify({ name, ...manifest }))
    }
    return root
}

// `build` rather than `serve`, so the VITE_PORT requirement stays out of it.
const optimizeDepsAt = (root: string, options: Partial<VueSpaOptions> = {}) => vueSpa({ dirname: root, ...options })({ command: 'build', mode: 'prod' }).optimizeDeps

const optimizeDeps = (fixture: Fixture, options: Partial<VueSpaOptions> = {}) => optimizeDepsAt(appRoot(fixture), options)

// An app whose own source packages are a scope of its own, so what is pinned
// into the pre-bundle can be read off the fixture rather than off the default.
const ACME: Partial<VueSpaOptions> = { sourcePackages: ['@acme/'] }

const DEPS = {
    '@greendrake/anything': '1.0.0',
    '@acme/widgets': '1.0.0',
    'my-design-system': '1.0.0',
    'some-library': '1.0.0',
    vue: '3.5.0',
    pinia: '3.0.0'
}

describe('which dependencies are kept out of the pre-bundle', () => {
    test('by default, the scope the preset ships beside — and nothing else', () => {
        expect(optimizeDeps({ dependencies: DEPS })?.exclude).toEqual(['@greendrake/anything'])
    })

    test('a trailing slash matches a whole scope, anything else an exact name', () => {
        expect(optimizeDeps({ dependencies: DEPS }, { sourcePackages: ['@acme/', 'my-design-system'] })?.exclude).toEqual(['@acme/widgets', 'my-design-system'])
    })

    test('naming any list replaces the default rather than adding to it', () => {
        expect(optimizeDeps({ dependencies: DEPS }, { sourcePackages: ['@acme/'] })?.exclude).not.toContain('@greendrake/anything')
    })

    test('a dependency the app does not have is not excluded on the strength of being listed', () => {
        expect(optimizeDeps({ dependencies: { 'some-library': '1.0.0' } }, { sourcePackages: ['@acme/', 'my-design-system'] })?.exclude).toEqual([])
    })
})

describe('which dependencies are pinned into the pre-bundle', () => {
    test('the framework libraries the app declares', () => {
        // vue-router is absent from DEPS: pinning a package the app does not
        // depend on is what Vite fails to resolve.
        expect(optimizeDeps({ dependencies: DEPS })?.include).toEqual(['vue', 'pinia'])
    })

    test("a source package's own dependency, named through the package that imports it", () => {
        const include = optimizeDeps({ dependencies: { '@acme/ui': '1.0.0' }, installed: { '@acme/ui': { dependencies: { 'tooltip-lib': '6.0.0' } } } }, ACME)?.include
        expect(include).toEqual(['@acme/ui > tooltip-lib'])
    })

    test('named plainly instead where the app declares it too, so it is not pre-bundled once per id', () => {
        const include = optimizeDeps({ dependencies: { '@acme/i18n': '1.0.0', 'yaml-parser': '4.1.0' }, installed: { '@acme/i18n': { dependencies: { 'yaml-parser': '4.1.0' } } } }, ACME)?.include
        expect(include).toEqual(['yaml-parser'])
    })

    test('a peer the app supplies at runtime, but not one it leaves to its tooling', () => {
        const include = optimizeDeps(
            {
                dependencies: { '@acme/shell': '1.0.0', 'native-bridge': '7.0.0' },
                devDependencies: { 'build-tool': '8.0.0' },
                installed: { '@acme/shell': { peerDependencies: { 'native-bridge': '7.0.0', 'build-tool': '>=8' } } }
            },
            ACME
        )?.include
        expect(include).toEqual(['native-bridge'])
    })

    test('named through the whole chain, so an intermediate the app never declares still resolves', () => {
        const include = optimizeDeps(
            {
                dependencies: { '@acme/dash': '1.0.0' },
                installed: {
                    '@acme/dash': { dependencies: { '@acme/ui': '1.0.0' } },
                    '@acme/ui': { dependencies: { 'tooltip-lib': '6.0.0' } }
                }
            },
            ACME
        )?.include
        expect(include).toEqual(['@acme/dash > @acme/ui > tooltip-lib'])
    })

    test('anchored at the declared package where there is one, whichever branch reaches it first', () => {
        const include = optimizeDeps(
            {
                dependencies: { '@acme/dash': '1.0.0', '@acme/ui': '1.0.0' },
                installed: {
                    '@acme/dash': { dependencies: { '@acme/ui': '1.0.0' } },
                    '@acme/ui': { dependencies: { 'tooltip-lib': '6.0.0' } }
                }
            },
            ACME
        )?.include
        expect(include).toEqual(['@acme/ui > tooltip-lib'])
    })

    test('the whole source-package graph, each package visited once', () => {
        const include = optimizeDeps(
            {
                dependencies: { '@acme/content': '1.0.0', '@acme/api': '1.0.0' },
                installed: {
                    '@acme/content': {
                        dependencies: {
                            '@acme/ui': '1.0.0',
                            '@acme/api': '1.0.0',
                            'markdown-lib': '15.0.0'
                        }
                    },
                    '@acme/api': { dependencies: { '@acme/ui': '1.0.0' } },
                    '@acme/ui': { dependencies: { 'tooltip-lib': '6.0.0' } }
                }
            },
            ACME
        )?.include
        expect(include).toEqual(['@acme/content > @acme/ui > tooltip-lib', '@acme/content > markdown-lib'])
    })

    test('nothing carried by a source package the app calls tooling, however it is reached', () => {
        // The build-tool integration a package ships beside its components: the
        // app has it among its dev dependencies, and what it carries could not
        // be bundled for a browser that never reaches it.
        const include = optimizeDeps(
            {
                dependencies: { '@acme/i18n': '1.0.0' },
                devDependencies: { '@acme/build-preset': '1.0.0' },
                installed: {
                    '@acme/i18n': { dependencies: { '@acme/build-preset': '1.0.0' } },
                    '@acme/build-preset': { dependencies: { 'bundler-plugin': '6.0.0' } }
                }
            },
            ACME
        )?.include
        expect(include).toEqual([])
    })

    test('nor a package published only under subpaths, which no entry can name', () => {
        const include = optimizeDeps(
            {
                dependencies: { '@acme/editor': '1.0.0' },
                installed: {
                    '@acme/editor': { dependencies: { 'subpaths-only': '3.0.0', rooted: '3.0.0' } },
                    'subpaths-only': { exports: { './state': './state.js', './view': './view.js' } },
                    rooted: { exports: { '.': './index.js' } }
                }
            },
            ACME
        )?.include
        expect(include).toEqual(['@acme/editor > rooted'])
    })

    test('found beside the real directory of a package an isolated install links in', () => {
        // bun's and pnpm's layout: the app's node_modules holds a link into a
        // store, and what the linked package depends on sits beside its real
        // directory there — nowhere above the link itself.
        const root = appRoot({ dependencies: { '@acme/ui': '1.0.0' }, missing: ['@acme/ui'] })
        const store = path.join(root, 'node_modules', '.store', 'node_modules')
        const install = (name: string, manifest: Manifest): void => {
            mkdirSync(path.join(store, name), { recursive: true })
            writeFileSync(path.join(store, name, 'package.json'), JSON.stringify({ name, ...manifest }))
        }
        install('@acme/ui', { dependencies: { 'tooltip-lib': '6.0.0' } })
        install('tooltip-lib', {})
        mkdirSync(path.join(root, 'node_modules', '@acme'))
        symlinkSync(path.join(store, '@acme', 'ui'), path.join(root, 'node_modules', '@acme', 'ui'))
        expect(optimizeDepsAt(root, ACME)?.include).toEqual(['@acme/ui > tooltip-lib'])
    })

    test('a source package declared but never installed is an error, not a gap in the list', () => {
        expect(() => optimizeDeps({ dependencies: { '@acme/ui': '1.0.0' }, missing: ['@acme/ui'] }, ACME)).toThrow(/@acme\/ui is declared but not installed/)
    })
})
