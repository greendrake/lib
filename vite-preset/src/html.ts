import type { Plugin } from 'vite'

// Writes a data element per entry into index.html's head —
// <script type="application/json" id="<id>">JSON</script> — that the app
// reads at runtime instead of a compile-time define. A null value leaves the
// element empty, for the Go binary that serves the build to fill
// (greendrake/spa's Data, which matches the element in exactly this form).
// `<` is escaped so no value can end the script element.
export const htmlData = (map: Record<string, unknown>): Plugin => ({
    name: 'greendrake-html-data',
    transformIndexHtml: () =>
        Object.entries(map).map(([id, value]) => ({
            tag: 'script',
            attrs: { type: 'application/json', id },
            children: value === null ? '' : JSON.stringify(value).replace(/</g, '\\u003c'),
            injectTo: 'head-prepend' as const
        }))
})

// Substitutes build-time values into index.html before Vite processes it —
// e.g. a generated bootstrap snippet, a build hash, PWA metadata. Keys are
// matched literally (convention: __NAME__).
export const htmlPlaceholders = (map: Record<string, string>): Plugin => {
    const keys = Object.keys(map)
    if (!keys.length) {
        throw new Error('htmlPlaceholders: empty placeholder map')
    }
    const pattern = new RegExp(keys.map(key => key.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|'), 'g')
    return {
        name: 'greendrake-html-placeholders',
        transformIndexHtml: {
            order: 'pre',
            // The pattern is built from the map's own keys, so every match is one.
            handler: html => html.replace(pattern, match => map[match]!)
        }
    }
}
