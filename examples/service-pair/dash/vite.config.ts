import path from 'node:path'
import { defineConfig } from 'vite'
import { dashApp, requireEnv } from '@greendrake/dash/vite'

export default defineConfig(
    dashApp({
        dirname: import.meta.dirname,
        define: env => ({
            __WS_URL__: requireEnv(env, 'WS_URL'),
            __ADMIN_API_KEY__: requireEnv(env, 'ADMIN_API_KEY')
        }),
        // This example sits two levels inside the repository that holds the
        // packages, so the dev server's default reach — one level up from the
        // app — stops short of their source and their font files. An app that
        // installs them from npm has them under its own node_modules and needs
        // nothing here.
        overrides: () => ({ server: { fs: { allow: [path.resolve(import.meta.dirname, '../../..')] } } })
    })
)
