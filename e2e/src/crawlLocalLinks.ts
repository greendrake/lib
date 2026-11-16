import { expect, type Page } from '@playwright/test'

// Crawl every same-origin page link reachable from '/': verify each page
// renders content, exercise SPA navigation by clicking each distinct link
// once, and fail on any uncaught page error or a click that does not reach a
// rendered page. Returns the visited paths for reporting.

// A path whose last segment carries a file extension names a file, not a page.
const namesFile = (pathname: string): boolean => {
    const lastSegment = pathname.split('/').pop()
    return !!lastSegment && lastSegment.includes('.') && !lastSegment.startsWith('.')
}

// A URL's path as the crawl keys pages: with its query string, without its
// fragment.
const pathOf = (url: URL): string => url.pathname + url.search

// The links of exactly this href that navigate in place, as a CSS selector.
const linksTo = (href: string): string => {
    const link = `a[href="${href.replace(/["\\]/g, '\\$&')}"]`
    return `${link}:not([target]), ${link}[target="_self"]`
}

export const crawlLocalLinks = async (page: Page, origin: string): Promise<string[]> => {
    const visitedUrls = new Set<string>()
    const urlsToVisit = ['/']
    // Each distinct href is click-tested once across the whole crawl — nav
    // menus repeat on every page and re-clicking them is pure wait time.
    const clickedHrefs = new Set<string>()

    // Track pending network requests for debugging
    const pendingRequests = new Set<string>()
    page.on('request', request => {
        pendingRequests.add(request.url())
    })
    page.on('requestfinished', request => {
        pendingRequests.delete(request.url())
    })
    page.on('requestfailed', request => {
        pendingRequests.delete(request.url())
        const url = request.url()
        const reason = request.failure()?.errorText ?? 'unknown'
        // Only warn about real failures of HTTP(S) requests. Aborted requests
        // are normal when a navigation cuts a load short.
        if ((url.startsWith('http://') || url.startsWith('https://')) && reason !== 'net::ERR_ABORTED') {
            console.warn(`Request failed: ${url} (${reason})`)
        }
    })

    const consoleErrors: string[] = []
    page.on('console', msg => {
        if (msg.type() === 'error') {
            consoleErrors.push(msg.text())
        }
    })

    const pageErrors: string[] = []
    page.on('pageerror', error => {
        pageErrors.push(error.message)
    })

    // Waits for the page to render real content — more than 100 characters of
    // text, other than `unlike`, the text of the page a click left — and
    // answers its text. networkidle is unreliable with keepalive/analytics
    // requests. The wait outlives a navigation landing meanwhile, such as a
    // route's redirect to another site.
    const rendered = async (unlike = ''): Promise<string> => {
        try {
            await page.waitForFunction(
                previous => {
                    const text = document.body?.innerText.trim() ?? ''
                    return text.length > 100 && text !== previous
                },
                unlike,
                { timeout: 10000 }
            )
        } catch (error) {
            console.error(`Pending requests: ${Array.from(pendingRequests).join(', ')}`)
            throw error
        }
        return (await page.locator('body').innerText()).trim()
    }

    // Loads the path and answers its text once rendered.
    const load = async (path: string): Promise<string> => {
        await page.goto(`${origin}${path}`, { waitUntil: 'domcontentloaded' })
        return rendered()
    }

    while (urlsToVisit.length > 0) {
        const currentPath = urlsToVisit.shift()!
        if (visitedUrls.has(currentPath)) {
            continue
        }

        console.log(`Visiting: ${currentPath}`)
        visitedUrls.add(currentPath)
        let text = await load(currentPath)

        // Routes may deliberately leave the site (external-redirect stubs like
        // /store → a marketplace). Once off-origin, nothing on the page belongs
        // to the app: harvesting there would queue the FOREIGN site's paths
        // onto the origin under test. Reaching the redirect without a page
        // error is the assertion. Checked after the content wait — the guard's
        // replace can land any time during boot.
        const here = new URL(page.url())
        if (here.origin !== new URL(origin).origin) {
            console.log(`  → left origin (${page.url()}), skipping harvest`)
            expect(pageErrors).toEqual([])
            continue
        }
        expect(pageErrors).toEqual([])

        // The page's links to the site's pages, by href: each one's path joins
        // the queue.
        const hrefs = await page.locator('a[href]').evaluateAll(links => links.map(a => a.getAttribute('href')!))
        const pages = new Map<string, URL>()
        for (const href of hrefs) {
            const url = new URL(href, here)
            if (url.origin === here.origin && !namesFile(url.pathname)) {
                pages.set(href, url)
            }
        }
        for (const url of pages.values()) {
            const path = pathOf(url)
            if (!visitedUrls.has(path) && !urlsToVisit.includes(path)) {
                urlsToVisit.push(path)
            }
        }

        // Click through the links to other pages, to exercise SPA navigation.
        // Each click starts from the page freshly loaded, and must change the
        // URL and render text other than this page's. A link this page shows
        // no visible copy of navigating in place (one in a collapsed menu, or
        // opening a new tab) is left for a page that shows one.
        let away = false
        for (const [href, url] of pages) {
            if (clickedHrefs.has(href) || pathOf(url) === pathOf(here)) {
                continue
            }
            if (away) {
                text = await load(currentPath)
                away = false
            }
            const link = page.locator(linksTo(href)).filter({ visible: true }).first()
            if ((await link.count()) === 0) {
                continue
            }
            clickedHrefs.add(href)
            await link.click()
            away = true
            await page.waitForURL(at => at.href !== here.href)
            await rendered(text)
            expect(pageErrors, `after clicking ${href} on ${currentPath}`).toEqual([])
        }
    }

    console.log(`\nTest Summary:`)
    console.log(`Total unique pages visited: ${visitedUrls.size}`)
    console.log(`Pages: ${Array.from(visitedUrls).join(', ')}`)

    if (consoleErrors.length > 0) {
        console.warn('Console errors detected:', consoleErrors)
    }
    expect(pageErrors).toEqual([])
    return Array.from(visitedUrls)
}
