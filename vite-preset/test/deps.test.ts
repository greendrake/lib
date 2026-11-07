import { afterAll, describe, expect, test } from 'bun:test'
import { mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { BUILD, RUNTIME, closure, installed, packageAt, walk, type Edge, type Manifest, type Resolver } from '../src/deps'

const roots: string[] = []

afterAll(() => roots.forEach(root => rmSync(root, { recursive: true, force: true })))

// An app and the packages node_modules holds for it, by name: what the walk is
// asked about in every case.
//
//   app ── ui ── api ── util
//    │      │     └──── ui        (a cycle)
//    │      ├── chart             (third-party)
//    │      └╌╌ vue, @playwright/test (peers: the app supplies vue; nobody installed the other)
//    ├── util                     (reached again, by another chain)
//    └┄┄ kit ── builder           (dev only)
const app = (): string => {
    const root = realpathSync(mkdtempSync(path.join(os.tmpdir(), 'deps-test-')))
    roots.push(root)
    const write = (dir: string, manifest: Manifest & { name: string }): void => {
        mkdirSync(dir, { recursive: true })
        writeFileSync(path.join(dir, 'package.json'), JSON.stringify(manifest))
    }
    write(root, {
        name: 'app',
        dependencies: {
            ui: '1',
            util: '1',
            vue: '3'
        },
        devDependencies: { kit: '1' }
    })
    const installed: Record<string, Manifest> = {
        ui: { dependencies: { api: '1', chart: '1' }, peerDependencies: { vue: '3', '@playwright/test': '1' } },
        api: { dependencies: { util: '1', ui: '1' } },
        util: {},
        chart: {},
        vue: {},
        kit: { dependencies: { builder: '1' } },
        builder: {}
    }
    for (const [name, manifest] of Object.entries(installed)) write(path.join(root, 'node_modules', name), { name, ...manifest })
    return root
}

const names = (root: string, edges: readonly Edge[], resolver: Resolver = installed()): string[] => closure([root], edges, resolver).map(pkg => path.relative(root, pkg.dir) || '.')

describe('closure', () => {
    test('the runtime view: what ships, transitively, every package once', () => {
        expect(names(app(), RUNTIME)).toEqual(['.', 'node_modules/ui', 'node_modules/api', 'node_modules/util', 'node_modules/chart', 'node_modules/vue'])
    })

    test('the build view: the dev dependencies too, and theirs', () => {
        expect(names(app(), BUILD)).toEqual([...names(app(), RUNTIME), 'node_modules/kit', 'node_modules/builder'])
    })

    test('an edge the resolver does not follow is never reached', () => {
        const all = installed()
        expect(names(app(), RUNTIME, { follows: name => name !== 'chart', find: all.find })).not.toContain('node_modules/chart')
    })

    test('a package declared but never installed is an error, not a gap', () => {
        const root = app()
        rmSync(path.join(root, 'node_modules/util'), { recursive: true })
        expect(() => closure([root], RUNTIME, installed())).toThrow(/util is declared but not installed/)
    })
})

describe('walk', () => {
    // Every edge the walk reached, as `chain > name (edge)`, going on into
    // everything but what `stop` names.
    const edgesOf = (root: string, edges: readonly Edge[], stop: (name: string) => boolean = () => false): string[] => {
        const reached: string[] = []
        walk([packageAt(root)], edges, installed(), ({ name, chain, edge }) => {
            reached.push(`${[...chain, name].join(' > ')} (${edge})`)
            return !stop(name)
        })
        return reached
    }

    test('hands the visitor each edge with the chain it was reached by, a package reached twice walked once', () => {
        expect(edgesOf(app(), RUNTIME)).toEqual([
            'ui (dependencies)',
            'ui > api (dependencies)',
            'ui > api > util (dependencies)',
            // The cycle back: visited, not walked again.
            'ui > api > ui (dependencies)',
            'ui > chart (dependencies)',
            // Another chain to a package already walked.
            'util (dependencies)',
            'vue (dependencies)'
        ])
    })

    test('goes on only into what the visitor says to', () => {
        expect(edgesOf(app(), RUNTIME, name => name === 'ui')).toEqual(['ui (dependencies)', 'util (dependencies)', 'vue (dependencies)'])
    })

    test('looks a package up only once the visitor asks for it: a peer nobody installed is passed over', () => {
        const peers: string[] = []
        walk([packageAt(app())], [...RUNTIME, 'peerDependencies'], installed(), reached => {
            if (reached.edge !== 'peerDependencies') return true
            // The app supplies vue, so it is found; the other peer is asked
            // nothing of, and its absence is never noticed.
            if (reached.name === 'vue') peers.push(path.basename(reached.package.dir))
            else peers.push(`${reached.name} unasked`)
            return false
        })
        expect(peers).toEqual(['vue', '@playwright/test unasked'])
    })
})
