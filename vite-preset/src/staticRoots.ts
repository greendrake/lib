import { cpSync, existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import type { Plugin } from 'vite'

// Serving files that live outside the app: an app's `public/` is its own, but
// some assets belong to the product rather than to the frontend — a locale
// catalogue the backend renders from too, the prose a content page shows. Those
// live in shared config, and this plugin puts them at their public URLs in both
// modes: middleware in dev, a copy into `dist/` at build. One declaration, so
// the two cannot drift into serving different bytes.

export interface StaticRoot {
    // Absolute directory to serve.
    dir: string
    // URL prefix it appears under, e.g. '/catalogue'.
    urlPath: string
    // Rewrites a file's contents on the way out, in both modes. Used for the
    // locale catalogues, which are served merged over the default locale.
    transform?: (relativePath: string, contents: string) => string
}

const contentTypeFor = (file: string): string => {
    if (file.endsWith('.yaml') || file.endsWith('.yml')) return 'text/yaml; charset=utf-8'
    if (file.endsWith('.json')) return 'application/json; charset=utf-8'
    return 'text/plain; charset=utf-8'
}

// Every file under dir, as paths relative to it.
const walk = (dir: string): string[] =>
    readdirSync(dir, { recursive: true, withFileTypes: true })
        .filter(entry => entry.isFile())
        .map(entry => path.relative(dir, path.join(entry.parentPath, entry.name)))

// staticRootFiles lists what a root publishes, as scope-relative URL paths —
// the service worker's precache list needs to name these without duplicating
// the walk.
export const staticRootFiles = (root: StaticRoot): string[] =>
    walk(root.dir).map(rel => `${root.urlPath}/${rel.split(path.sep).join('/')}`)

export const staticRoots = (roots: StaticRoot[]): Plugin => ({
    name: 'greendrake-static-roots',

    configureServer(server) {
        server.middlewares.use((req, res, next) => {
            const url = (req.url ?? '').split('?')[0]
            const root = roots.find(r => url.startsWith(`${r.urlPath}/`))
            if (!root) {
                next()
                return
            }
            const rel = url.slice(root.urlPath.length + 1)
            // Nothing climbs out of the root: the URL is attacker-controlled in
            // dev too, and the roots sit outside the workspace fs.allow list.
            const file = path.resolve(root.dir, rel)
            if (!file.startsWith(path.resolve(root.dir) + path.sep) || !existsSync(file)) {
                next()
                return
            }
            const body = readFileSync(file, 'utf-8')
            res.setHeader('Content-Type', contentTypeFor(file))
            res.end(root.transform ? root.transform(rel, body) : body)
        })
    },

    // After the bundle, so nothing in the pipeline treats these as app assets.
    closeBundle: {
        sequential: true,
        handler() {
            for (const root of roots) {
                const dest = path.resolve(this.environment.config.build.outDir, root.urlPath.replace(/^\//, ''))
                mkdirSync(dest, { recursive: true })
                if (!root.transform) {
                    cpSync(root.dir, dest, { recursive: true })
                    continue
                }
                for (const rel of walk(root.dir)) {
                    const target = path.join(dest, rel)
                    mkdirSync(path.dirname(target), { recursive: true })
                    writeFileSync(target, root.transform(rel, readFileSync(path.join(root.dir, rel), 'utf-8')))
                }
            }
        }
    }
})
