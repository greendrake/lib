import { describe, expect, test } from 'bun:test'
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { staticRootFiles } from '../src/staticRoots'

describe('staticRootFiles', () => {
    test('lists every file but the dotfiles Finder and editors leave behind', () => {
        const dir = mkdtempSync(path.join(tmpdir(), 'static-root-'))
        mkdirSync(path.join(dir, 'about'))
        mkdirSync(path.join(dir, '.cache'))
        for (const file of ['en-GB.yaml', 'about/ru.txt', '.DS_Store', 'about/.DS_Store', '.cache/index']) {
            writeFileSync(path.join(dir, file), '')
        }

        expect(staticRootFiles({ dir, urlPath: '/catalogue' }).sort()).toEqual(['/catalogue/about/ru.txt', '/catalogue/en-GB.yaml'])
    })
})
