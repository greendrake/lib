// Build-time (Node) environment helpers shared by app vite configs.

import { execSync } from 'node:child_process'
import { loadEnv } from 'vite'
import type { ApiTargetEnv } from './apiTarget'

// The app's own .env / .env.<mode> files, every key (not just VITE_-prefixed
// ones) — these files carry plain build inputs, and dotenv strips the `export`
// prefix they are written with so the same file can also be shell-sourced.
// process.env outranks the file, which makes a deliberate shell export an
// override but also means an app must not name files `.env.development` /
// `.env.production`: bun auto-loads those at startup, and whichever one it
// picked would then outrank the mode's file.
// Config factories take vite's whole build-env surface through this package so
// that a downstream factory never resolves its own vite copy.
export const loadAppEnv = (mode: string, dirname: string): Record<string, string> => loadEnv(mode, dirname, '')

// The build-time inputs of resolveApiTarget, read from the conventional env
// vars (API_PORT, PUBLIC_FE_HOST, PUBLIC_API_HOST). Apps bake the result into
// a single define (`__API_TARGET_ENV__`) and feed the same object to anything
// else that resolves the host, so all of it resolves from one source.
export const apiTargetEnv = (dev: boolean, paths?: Pick<ApiTargetEnv, 'apiPath' | 'wsPath'>): ApiTargetEnv => ({
    dev,
    apiPort: process.env.API_PORT,
    publicFeHost: process.env.PUBLIC_FE_HOST,
    publicApiHost: process.env.PUBLIC_API_HOST,
    ...paths
})

// `Boolean(process.env.X)` would treat the literal string "0" as true (it's a
// non-empty string). Parse as a Go-flavoured boolean instead so "0" / "false"
// / unset all register as off (Go strconv.ParseBool semantics).
export const envBool = (v: string | undefined): boolean => /^(1|t|true)$/i.test(v ?? '')

// imageBaseUrl is prepended to S3 object keys to form absolute image URLs. An
// explicitly defined S3_PREFIX wins outright — including when set to empty,
// which forces the base empty regardless of the AWS_S3_* vars. The check is
// for definedness (not truthiness) precisely so an empty S3_PREFIX overrides.
// When S3_PREFIX is unset, derive the base from the S3 endpoint + bucket.
export const deriveImageBaseUrl = (env: NodeJS.ProcessEnv = process.env): string =>
    env.S3_PREFIX !== undefined ? env.S3_PREFIX : env.AWS_S3_ENDPOINT && env.AWS_S3_BUCKET ? `${env.AWS_S3_ENDPOINT}/${env.AWS_S3_BUCKET}` : ''

// FE build identifier for a `__BUILD_HASH__` define. If git fails, the
// build fails (no defensive fallbacks for environmental issues — fix the
// build environment).
export const buildHash = (): string => execSync('git rev-parse --short HEAD', { encoding: 'utf-8' }).trim()

// The single port convention: VITE_PORT, required, strict.
export const requirePort = (): number => {
    if (!process.env.VITE_PORT) {
        throw new Error('VITE_PORT environment variable must be set')
    }
    return parseInt(process.env.VITE_PORT, 10)
}
