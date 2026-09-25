import type { Plugin } from 'vite'

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
