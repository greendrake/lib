#!/usr/bin/env node
// Load a URL in every engine, at phone and desktop widths, and report any box
// whose content is clipped away unreachably — see src/findClippedContent.ts for
// what that means and why engines disagree about it.
//
//   ./check-layout.mjs http://localhost:5173/news
//   ./check-layout.mjs https://app.example.com/news --css '.News .Card { overflow: hidden }'
//
// --css injects a stylesheet before measuring, which is how a fix is tried
// against a deployed page without deploying it, and how this check is confirmed
// to still catch the thing it exists to catch.
//
// Exits non-zero when anything is clipped, so it can gate a deploy.

import { createRequire } from 'node:module'

const VIEWPORTS = [
    {
        name: 'phone',
        width: 390,
        height: 844
    },
    {
        name: 'desktop',
        width: 1280,
        height: 800
    }
]

const [url, ...rest] = process.argv.slice(2)
if (!url) {
    console.error('usage: check-layout.mjs <url> [--css <stylesheet>]')
    process.exit(2)
}
const cssIndex = rest.indexOf('--css')
const extraCSS = cssIndex === -1 ? null : rest[cssIndex + 1]

// Playwright is a peer dependency: resolve it from wherever the caller has it
// rather than pinning a path that only exists on one machine.
const require = createRequire(import.meta.url)
let playwright
for (const candidate of ['playwright', 'playwright-core', `${process.env.HOME}/.bun/install/global/node_modules/playwright`]) {
    try {
        // Playwright is CommonJS, so an ESM import puts its exports behind
        // `default` — except where the interop hoists them. Accept either.
        const loaded = await import(require.resolve(candidate))
        const exports = loaded.chromium ? loaded : loaded.default
        if (exports?.chromium) {
            playwright = exports
            break
        }
    } catch {
        continue
    }
}
if (!playwright) {
    console.error('No playwright available. Install it in the workspace, or globally: bun add -g playwright')
    process.exit(2)
}

// Inlined rather than imported from src/: this runs as a plain script against
// any URL, with no TypeScript pipeline in the way.
const findClipped = tolerance => {
    const describe = el => {
        const name = e => e.tagName.toLowerCase() + (e.classList.length ? `.${[...e.classList].join('.')}` : '')
        return el.parentElement ? `${name(el.parentElement)} > ${name(el)}` : name(el)
    }
    const found = []
    for (const el of document.querySelectorAll('*')) {
        const style = getComputedStyle(el)
        if (style.overflowY !== 'hidden' && style.overflowY !== 'clip') continue
        if (el.scrollHeight <= el.clientHeight + tolerance) continue
        if (el.childElementCount === 0) continue
        found.push({
            selector: describe(el),
            height: Math.round(el.getBoundingClientRect().height),
            contentHeight: el.scrollHeight,
            overflowY: style.overflowY,
            parentDisplay: el.parentElement ? getComputedStyle(el.parentElement).display : '',
            text: (el.textContent ?? '').trim().slice(0, 60)
        })
    }
    return found
}

let failures = 0
for (const engineName of ['chromium', 'webkit']) {
    const engine = playwright[engineName]
    if (!engine) continue
    const browser = await engine.launch()
    for (const viewport of VIEWPORTS) {
        const context = await browser.newContext({ viewport: { width: viewport.width, height: viewport.height } })
        const page = await context.newPage()
        try {
            await page.goto(url, { waitUntil: 'networkidle', timeout: 30000 })
            if (extraCSS) await page.addStyleTag({ content: extraCSS })
            const clipped = await page.evaluate(findClipped, 4)
            const label = `${engineName}/${viewport.name}`
            if (clipped.length === 0) {
                console.log(`ok    ${label}`)
            } else {
                failures += clipped.length
                console.log(`CLIP  ${label}`)
                // One component collapses once per instance; the selector is
                // the finding and the count is the blast radius.
                const bySelector = new Map()
                for (const c of clipped) {
                    const seen = bySelector.get(c.selector)
                    if (seen) {
                        seen.count++
                        seen.height = Math.min(seen.height, c.height)
                        seen.contentHeight = Math.max(seen.contentHeight, c.contentHeight)
                    } else {
                        bySelector.set(c.selector, { ...c, count: 1 })
                    }
                }
                for (const c of bySelector.values()) {
                    console.log(`      ${c.selector} ×${c.count}: ${c.height}px tall, up to ${c.contentHeight}px of content ` + `(overflow-y: ${c.overflowY}, parent display: ${c.parentDisplay})`)
                    if (c.text) console.log(`        hidden: ${JSON.stringify(c.text)}`)
                }
            }
        } catch (error) {
            failures++
            console.log(`ERROR ${engineName}/${viewport.name}: ${error.message.split('\n')[0]}`)
        }
        await context.close()
    }
    await browser.close()
}

process.exit(failures > 0 ? 1 : 0)
