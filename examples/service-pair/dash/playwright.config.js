// @ts-check
import { spaPlaywright } from '@greendrake/e2e'

// The ports and the key the pair agrees on for a test run, overridable so two
// runs on one machine do not collide. They reach the dashboard build as vite
// defines, which is why WS_URL is composed here rather than read from .env.dev
// — a moved API port has to move with it.
const apiPort = process.env.API_PORT ?? '8100'
const vitePort = process.env.VITE_PORT ?? '5180'
const adminKey = 'example-admin-key'

// Both halves, started by the test run: the spec is the only thing that proves
// they still fit together.
export default spaPlaywright({
    defaultPort: vitePort,
    overrides: {
        // One service, one state: parallel workers would each turn it on and
        // off under the others.
        fullyParallel: false,
        workers: 1,
        reporter: 'list',
        webServer: [
            {
                command: 'bun run start',
                cwd: '../api',
                url: `http://localhost:${apiPort}/readyz`,
                reuseExistingServer: false,
                env: { API_PORT: apiPort, ADMIN_API_KEY: adminKey }
            },
            {
                command: 'bun run dev',
                url: `http://localhost:${vitePort}`,
                reuseExistingServer: false,
                env: {
                    VITE_PORT: vitePort,
                    WS_URL: `ws://localhost:${apiPort}/v1/ws`,
                    ADMIN_API_KEY: adminKey
                }
            }
        ]
    }
})
