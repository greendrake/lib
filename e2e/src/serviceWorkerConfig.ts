import { defineConfig, devices, type PlaywrightTestConfig } from '@playwright/test'
import { existsSync } from 'node:fs'

export interface ServiceWorkerPlaywrightOptions {
    // The app's own dist/, which `vite preview` will serve. Absolute.
    dist: string
    // Frontend port when VITE_PORT is unset, per the app's harness convention.
    defaultPort?: string
    // Extra environment for the preview server, for configs that resolve
    // something before they will start.
    previewEnv?: Record<string, string>
    // Run against the full Chromium build rather than the headless shell
    // Playwright otherwise picks for a headless run. Required by any suite that
    // asserts on notifications: the shell ships without the notification
    // platform, so it hard-denies the permission — grantPermissions() is
    // accepted and Notification.permission still reads "denied", making a
    // worker's push handling untestable. Off by default because the shell
    // starts faster and the two builds are not behaviourally identical — a
    // suite driving several windows at once may pass on one and not the other.
    notifications?: boolean
    overrides?: PlaywrightTestConfig
}

// The service-worker suite's config: tests/sw/, run against a built app served
// statically by `vite preview`, with no backend at all.
//
// Serving an existing dist rather than building one here is deliberate — a
// build can carry deploy-time artefacts fetched from production, which a test
// run must not depend on. Nothing in the suite needs an API either: the app
// boots, its calls park unanswered, and what is asserted is that it got far
// enough to say so.
export const serviceWorkerPlaywright = ({ dist, defaultPort, previewEnv, notifications, overrides }: ServiceWorkerPlaywrightOptions): PlaywrightTestConfig => {
    const port = process.env.VITE_PORT || defaultPort
    if (!port) {
        throw new Error('serviceWorkerPlaywright: VITE_PORT must be set (this app configures no defaultPort)')
    }
    if (!existsSync(dist)) {
        throw new Error(`serviceWorkerPlaywright: no dist/ at ${dist} — run \`bun run build\` first`)
    }
    return defineConfig({
        testDir: './tests/sw',
        // One worker: every test drives the same origin's worker registration
        // and cache storage, which are shared browser-wide state.
        fullyParallel: false,
        workers: 1,
        forbidOnly: !!process.env.FORBID_ONLY,
        reporter: [['html', { open: 'never' }]],
        timeout: 60_000,
        use: {
            ...devices['Desktop Chrome'],
            ...(notifications ? { channel: 'chromium' } : {}),
            baseURL: `http://localhost:${port}`
        },
        webServer: {
            command: 'bun --bun vite preview',
            url: `http://localhost:${port}`,
            reuseExistingServer: false,
            env: { VITE_PORT: port, ...previewEnv }
        },
        ...overrides
    })
}
