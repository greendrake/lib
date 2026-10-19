import type { PlaywrightTestConfig } from '@playwright/test'

// The frontend port: VITE_PORT, the variable the app's own Vite config pins its
// server to, falling back to the app's default. No fallback configured and no
// VITE_PORT is a failed run rather than a silent hit on whatever is listening.
export const frontendPort = (factory: string, defaultPort: string | undefined): string => {
    const port = process.env.VITE_PORT || defaultPort
    if (!port) {
        throw new Error(`${factory}: VITE_PORT must be set (this app configures no defaultPort)`)
    }
    return port
}

// What every factory's config shares: a stray `.only` fails a CI run instead of
// quietly narrowing it, and the html report never auto-opens.
export const commonConfig = (): Pick<PlaywrightTestConfig, 'forbidOnly' | 'reporter'> => ({
    forbidOnly: !!process.env.CI,
    reporter: [['html', { open: 'never' }]]
})
