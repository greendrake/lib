import { expect, type Page } from '@playwright/test'

// Crawl every same-origin page link reachable from '/': verify each page
// renders content, exercise SPA navigation by clicking through links, and
// fail on any uncaught page error. Returns the visited paths for reporting.

const hasFileExtension = (url: string): boolean => {
    const pathname = url.replace(/[?#].*/, '')
    const lastSegment = pathname.split('/').pop()
    return !!lastSegment && lastSegment.includes('.') && !lastSegment.startsWith('.')
}

const isPageLink = (href: string | null): href is string => {
    if (!href) return false
    if (href.startsWith('#')) return false
    if (href.startsWith('mailto:')) return false
    if (href.startsWith('tel:')) return false
    if (hasFileExtension(href)) return false
    return true
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
        // are normal when the test navigates away mid-load (e.g. via goBack()).
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

    while (urlsToVisit.length > 0) {
        const currentPath = urlsToVisit.shift()!
        if (visitedUrls.has(currentPath)) {
            continue
        }

        console.log(`Visiting: ${currentPath}`)
        visitedUrls.add(currentPath)

        await page.goto(`${origin}${currentPath}`, { waitUntil: 'domcontentloaded' })

        // Wait for real content to render (networkidle is unreliable with
        // keepalive/analytics requests).
        try {
            await page.waitForFunction(() => {
                const body = document.body
                return !!body && !!body.textContent && body.textContent.trim().length > 100
            }, { timeout: 10000 })
        } catch (error) {
            console.error(`Pending requests: ${Array.from(pendingRequests).join(', ')}`)
            throw error
        }

        // Routes may deliberately leave the site (external-redirect stubs like
        // /store → a marketplace). Once off-origin, nothing on the page belongs
        // to the app: harvesting there would queue the FOREIGN site's paths
        // onto the origin under test. Reaching the redirect without a page
        // error is the
        // assertion. Checked after the content wait — the guard's replace can
        // land any time during boot.
        if (new URL(page.url()).origin !== new URL(origin).origin) {
            console.log(`  → left origin (${page.url()}), skipping harvest`)
            expect(pageErrors).toEqual([])
            continue
        }

        const bodyText = await page.textContent('body')
        expect(bodyText).toBeTruthy()
        expect(pageErrors).toEqual([])

        // Collect same-origin page links for the crawl queue.
        const links = await page.evaluate(() => {
            const fileExt = (url: string): boolean => {
                const pathname = url.replace(/[?#].*/, '')
                const lastSegment = pathname.split('/').pop()
                return !!lastSegment && lastSegment.includes('.') && !lastSegment.startsWith('.')
            }
            return Array.from(document.querySelectorAll('a[href]'))
                .map(a => {
                    const href = a.getAttribute('href')
                    if (!href) return null
                    if (href.startsWith('#') || href.startsWith('mailto:') || href.startsWith('tel:')) return null
                    if (fileExt(href)) return null
                    if (href.startsWith('http://') || href.startsWith('https://')) {
                        try {
                            const url = new URL(href)
                            if (url.origin !== window.location.origin) return null
                            if (fileExt(url.pathname)) return null
                            return url.pathname
                        } catch {
                            return null
                        }
                    }
                    return href.startsWith('/') ? href : null
                })
                .filter((href): href is string => href !== null)
        })
        for (const link of links) {
            if (!visitedUrls.has(link) && !urlsToVisit.includes(link)) {
                urlsToVisit.push(link)
            }
        }

        // Click through the links on this page to exercise SPA navigation.
        // Locators are re-resolved by index each iteration: goBack() rebuilds
        // the DOM, so a locator captured before the click can go stale.
        const linkCount = await page.locator('a[href]').count()
        for (let i = 0; i < linkCount; i++) {
            // A previous click may have landed on an external-redirect route;
            // goBack() cannot always recover (location.replace rewrites the
            // entry). Re-anchor on the page under test before continuing.
            if (new URL(page.url()).origin !== new URL(origin).origin) {
                await page.goto(`${origin}${currentPath}`, { waitUntil: 'domcontentloaded' })
                await page.waitForTimeout(500)
            }
            const link = page.locator('a[href]').nth(i)
            try {
                const href = await link.getAttribute('href', { timeout: 2000 })
                if (!isPageLink(href) || clickedHrefs.has(href)) {
                    continue
                }
                if (href.startsWith('http://') || href.startsWith('https://')) {
                    if (new URL(href).origin !== new URL(origin).origin) continue
                }
                if ((await link.isVisible()) && (await link.isEnabled())) {
                    clickedHrefs.add(href)
                    await link.click()
                    // Brief settle for the router transition; content asserted below.
                    await page.waitForTimeout(500)
                    const postClickBody = await page.textContent('body')
                    expect(postClickBody).toBeTruthy()
                    await page.goBack()
                    await page.waitForTimeout(500)
                }
            } catch (error) {
                console.warn(`Warning: link #${i} click-through failed: ${(error as Error).message}`)
            }
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
