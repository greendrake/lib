// Verifies what changed since a base commit and what is built against it
// (affected.ts), and nothing else:
//
//   bun verify.ts [--base=<ref>] [--only=js|go]
//
// js: the workspace installed over the lockfile it has — every package not in
// question resolves what it was verified with — then the affected packages
// type-checked, linted and tested. go: the affected Go modules formatted,
// vetted and tested (check-go). Without a base, everything.
import { existsSync } from 'node:fs'
import path from 'node:path'
import { affected, baseArg } from './affected'

const ROOT = import.meta.dir

const run = (command: string[]): void => {
  console.error(`$ ${command.join(' ')}`)
  const { exitCode } = Bun.spawnSync(command, {
    cwd: ROOT,
    stdin: 'inherit',
    stdout: 'inherit',
    stderr: 'inherit'
  })
  if (exitCode !== 0) process.exit(exitCode)
}

const args = Bun.argv.slice(2)
const only = args.find(arg => arg.startsWith('--only='))?.slice('--only='.length)
if (only !== undefined && only !== 'js' && only !== 'go') {
  console.error(`--only is js or go, not ${only}`)
  process.exit(2)
}
const set = affected(ROOT, baseArg(args))
const wide = set.wide.length ? ` — every ${set.wide.join(' and ')} unit, what it is verified with having moved` : ''
console.error(`verifying ${set.units.map(unit => unit.rel).join(', ') || 'nothing'}${wide}`)

if (only !== 'go' && set.packages.length) {
  run(['pnpm', 'install'])
  run(['pnpm', '-r', '--no-bail', ...set.packages.flatMap(rel => ['--filter', `./${rel}`]), 'run', '--if-present', 'check'])
  // A package holding nothing the lint config takes (stylesheets, fonts) is
  // one with nothing to lint, not a mistaken argument.
  run(['pnpm', 'exec', 'eslint', '--no-error-on-unmatched-pattern', ...set.packages])
  // Each as `./` + its path: bun reads any other argument as a substring of the
  // file path, and `dash/test` would match `examples/service-pair/dash/tests/`,
  // the Playwright specs, too.
  const tests = set.packages.map(rel => `${rel}/test`).filter(rel => existsSync(path.join(ROOT, rel)))
  if (tests.length) run(['bun', 'test', ...tests.map(rel => `./${rel}`)])
}

if (only !== 'js' && set.modules.length) run(['./check-go', ...set.modules])
