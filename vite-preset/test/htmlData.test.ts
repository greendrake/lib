import { describe, expect, test } from 'bun:test'
import { mkdtempSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { build } from 'vite'
import { htmlData } from '../src/html'

describe('htmlData', () => {
    // The built index carries each element in exactly the form greendrake.nz/go/spa
    // fills: its JSON with < escaped, or nothing for the serving binary.
    test('writes a data element per entry into the built index', async () => {
        const root = mkdtempSync(path.join(tmpdir(), 'html-data-'))
        writeFileSync(path.join(root, 'index.html'), '<!doctype html><html><head><title>app</title></head><body></body></html>')
        const result = await build({
            root,
            configFile: false,
            logLevel: 'silent',
            plugins: [htmlData({ env: { key: '</script>' }, served: null })],
            build: { write: false }
        })
        if (Array.isArray(result) || !('output' in result)) {
            throw new Error('expected a single build output')
        }
        const index = result.output.find(file => file.fileName === 'index.html')
        if (index?.type !== 'asset') {
            throw new Error('the build has no index.html')
        }
        expect(index.source).toContain('<script type="application/json" id="env">{"key":"\\u003c/script>"}</script>')
        expect(index.source).toContain('<script type="application/json" id="served"></script>')
    })
})
