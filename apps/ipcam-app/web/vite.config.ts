import { defineConfig } from 'vite'
import { dashApp } from '@greendrake/dash/vite'

// Where the backend lives is resolved in the bundle by src/api.ts via
// resolveApiTarget: in dev the page connects straight to the camera binary on
// __API_PORT__, the port the dev server is given as API_PORT; in production the
// binary serves the page itself, so it is the page's own origin.
export default defineConfig(
    dashApp({
        dirname: import.meta.dirname,
        // Dev-only input to resolveApiTarget; empty (and unused) in production builds.
        define: env => ({ __API_PORT__: env.API_PORT ?? '' })
    })
)
