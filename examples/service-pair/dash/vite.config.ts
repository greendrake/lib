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
        // The dev server reads files from the workspace root it finds above
        // the app. This example sits in the repository that holds the
        // packages, but not necessarily under the workspace that links them,
        // so the repository root is named for their source and their font
        // files. An app that installs them from npm has them under its own
        // node_modules and needs nothing here.
        overrides: () => ({ server: { fs: { allow: [path.resolve(import.meta.dirname, '../../..')] } } })
    })
)
