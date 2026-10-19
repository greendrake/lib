import { defineConfig, devices, type PlaywrightTestConfig } from '@playwright/test'
import { commonConfig, frontendPort } from './common'

export interface SpaPlaywrightOptions {
    // Frontend port when VITE_PORT is unset (each app's .env.test convention).
    // Omit it for apps whose harness always supplies VITE_PORT: a missing port
    // then fails the run rather than silently testing whatever happens to be
    // listening on a default.
    defaultPort?: string
    // The app's origin, for an app its own backend serves rather than a dev
    // server: VITE_PORT is then not read at all.
    baseURL?: string
    overrides?: PlaywrightTestConfig
}

// The config an SPA would otherwise hand-copy: chromium-only, ./tests dir, an
// html reporter that never auto-opens, and a baseURL built from VITE_PORT.
export const spaPlaywright = (options: SpaPlaywrightOptions = {}): PlaywrightTestConfig =>
    defineConfig({
        ...commonConfig(),
        testDir: './tests',
        fullyParallel: true,
        retries: 0,
        workers: 5,
        // Crawl-style specs visit every page and click through every link;
        // Playwright's 30s default is sized for single-interaction tests.
        timeout: 120_000,
        use: {
            baseURL: options.baseURL ?? `http://localhost:${frontendPort('spaPlaywright', options.defaultPort)}`,
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
