// The browser suites of what changed since a base commit and what is built
// against it (affected.ts): an example's, and an app's after its server is
// built. Each config starts what it tests itself.
//
//   bun e2e.ts [--base=<ref>]
//
// Without a base, every suite.
import { existsSync } from 'node:fs'
import path from 'node:path'
import { affected, baseArg, type Unit } from './affected'

export interface BrowserSuite {
  /** Where its playwright.config.js is, relative to the root. */
  dir: string
  /** What has to be built before it runs. */
  build?: { dir: string; command: string }
}

/**
 * The browser suites of `units`, under `root`. An app's needs its server built
 * from the Go workspace: one whose server is left out of it as released has no
 * suite to run here.
 */
export const browserSuites = (root: string, units: Unit[]): BrowserSuite[] =>
  units.flatMap((unit): BrowserSuite[] => {
    if (unit.kind === 'example') return unit.packages.filter(rel => existsSync(path.join(root, rel, 'playwright.config.js'))).map(dir => ({ dir }))
    if (unit.kind === 'app' && unit.modules.length) return [{ dir: `${unit.rel}/web`, build: { dir: `${unit.rel}/server`, command: './build' } }]
    return []
  })

if (import.meta.main) {
  const root = import.meta.dir
  const run = (command: string[], cwd: string): void => {
    console.error(`$ (cd ${path.relative(root, cwd) || '.'} && ${command.join(' ')})`)
    const { exitCode } = Bun.spawnSync(command, {
      cwd,
      stdin: 'inherit',
      stdout: 'inherit',
      stderr: 'inherit'
    })
    if (exitCode !== 0) process.exit(exitCode)
  }
  const suites = browserSuites(root, affected(root, baseArg(Bun.argv.slice(2))).units)
  if (suites.length) run(['pnpm', 'install'], root)
  for (const suite of suites) {
    // From inside the suite, so it uses the Playwright its own package.json
    // pins rather than whatever the root happens to resolve.
    run(['pnpm', 'exec', 'playwright', 'install', '--with-deps', 'chromium'], path.join(root, suite.dir))
    if (suite.build) run([suite.build.command], path.join(root, suite.build.dir))
    run(['bun', '--bun', 'playwright', 'test'], path.join(root, suite.dir))
  }
}
