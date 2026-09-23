import { defineConfig, devices, type PlaywrightTestConfig } from '@playwright/test'

export interface SpaPlaywrightOptions {
    // Frontend port when VITE_PORT is unset (each app's .env.test convention).
    // Omit it for apps whose harness always supplies VITE_PORT: a missing port
    // then fails the run rather than silently testing whatever happens to be
    // listening on a default.
    defaultPort?: string
    overrides?: PlaywrightTestConfig
}

// The config an SPA would otherwise hand-copy: chromium-only, ./tests dir, an
// html reporter that never auto-opens, and a baseURL built from VITE_PORT.
export const spaPlaywright = (options: SpaPlaywrightOptions = {}): PlaywrightTestConfig => {
    const port = process.env.VITE_PORT || options.defaultPort
    if (!port) {
        throw new Error('spaPlaywright: VITE_PORT must be set (this app configures no defaultPort)')
    }
    return defineConfig({
        testDir: './tests',
        fullyParallel: true,
        forbidOnly: !!process.env.CI,
        retries: 0,
        workers: 5,
        // Crawl-style specs visit every page and click through every link;
        // Playwright's 30s default is sized for single-interaction tests.
        timeout: 120_000,
        reporter: [['html', { open: 'never' }]],
        use: {
            baseURL: `http://localhost:${port}`,
            trace: 'on-first-retry',
            video: 'retain-on-failure'
        },
        projects: [
            {
                name: 'chromium',
                use: { ...devices['Desktop Chrome'] }
            }
        ],
        ...options.overrides
    })
}
