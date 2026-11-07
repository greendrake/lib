// What a change puts in question, and nothing else: the units of this
// repository that changed since a base commit, and every unit built against
// one of them; and every unit of a side whose lint rules or pinned tooling
// moved (Pins). `verify.ts` and `e2e.ts` act on it, here and in CI;
// `bun affected.ts [--base=<ref>]` prints it. Without a base it can resolve —
// none given, the repository's first commit, a shallow clone — everything is
// in question.
//
// A package left as released while a sibling of it moves is built against
// what it was released with: pnpm-workspace.yaml overrides its edge to that
// sibling (`"<package>><sibling>"`) with the spec it was published with, and a
// Go module whose requirements moved past it stays out of go.work. Neither is
// built against the moved unit, so neither comes into question through it.
import { YAML } from 'bun'
import { existsSync, readFileSync } from 'node:fs'
import path from 'node:path'

/** The halves of the repository verified apart: the pnpm workspace's packages, and the Go workspace's modules. */
export type Side = 'js' | 'go'

export const SIDES: readonly Side[] = ['js', 'go']

/**
 * What every unit of a side is verified with: the lint rules and the tooling
 * the root pins. A change to these puts the side's every unit in question.
 * The scripts that do the verifying put nothing in question: they are tested
 * where they are written, and a unit meets a change to them when it next comes
 * into question itself.
 */
export interface Pins {
  js: {
    eslintConfig: string
    bun: string
    packageManager: string
    devDependencies: Record<string, string>
    permissions: unknown
  }
  go: { version: string }
}

/** A file's text as the tree at `root` holds it; undefined where it holds none. */
export const textAt =
  (root: string) =>
  (file: string): string | undefined =>
    existsSync(path.join(root, file)) ? readFileSync(path.join(root, file), 'utf-8') : undefined

/** Each side's pins, read from a root's files through `text`; null where that root pins the side otherwise or not at all. */
export const READ_PINS: { [S in Side]: (text: (file: string) => string | undefined) => Pins[S] | null } = {
  js: text => {
    const [eslintConfig, bun, root, workspace] = ['eslint.config.js', '.bun-version', 'package.json', 'pnpm-workspace.yaml'].map(text)
    if (eslintConfig === undefined || bun === undefined || root === undefined || workspace === undefined) return null
    const { packageManager, devDependencies } = JSON.parse(root) as { packageManager?: string; devDependencies: Record<string, string> }
    if (packageManager === undefined) return null
    return {
      eslintConfig,
      bun: bun.trim(),
      packageManager,
      devDependencies,
      permissions: (YAML.parse(workspace) as { permissions: unknown }).permissions
    }
  },
  go: text => {
    const version = /^go\s+(\S+)/m.exec(text('go.work') ?? '')?.[1]
    return version === undefined ? null : { version }
  }
}

export type UnitKind = 'npm' | 'go' | 'app' | 'example'

/** A part of the repository verified as one: an npm package, a Go module, an app (both its halves), an example pair. */
export interface Unit {
  kind: UnitKind
  /** Its directory, relative to the root. */
  rel: string
  /** What units depending on it name it by: npm package names, Go module paths. */
  provides: string[]
  /** What it is built and tested against, by those names. */
  uses: string[]
  /** The workspace packages it holds, relative to the root. */
  packages: string[]
  /** The Go modules it holds, relative to the root. */
  modules: string[]
}

export interface Affected {
  /** The sides whose every unit is in question: their pins moved, or there is no base to measure against. */
  wide: Side[]
  units: Unit[]
  /** The workspace packages in question: what is installed, type-checked, linted and tested. */
  packages: string[]
  /** The Go modules in question: what is formatted, vetted and tested. */
  modules: string[]
  /** The files that changed, as they are now: what is scanned before publishing. */
  files: string[]
}

// What of a unit each side verifies.
const HOLDS: Record<Side, (unit: Unit) => string[]> = { js: unit => unit.packages, go: unit => unit.modules }

/** The units a change to `changed` puts in question: those holding a changed file, and every unit whose uses reach one of them. */
export const affectedUnits = (units: Unit[], changed: string[]): Unit[] => {
  const byName = new Map(units.flatMap(unit => unit.provides.map(name => [name, unit] as const)))
  const dependants = new Map<Unit, Unit[]>()
  for (const unit of units) {
    for (const name of unit.uses) {
      const dependency = byName.get(name)
      if (dependency && dependency !== unit) dependants.set(dependency, [...(dependants.get(dependency) ?? []), unit])
    }
  }
  const found = new Set<Unit>()
  const reach = (unit: Unit): void => {
    if (found.has(unit)) return
    found.add(unit)
    for (const dependant of dependants.get(unit) ?? []) reach(dependant)
  }
  for (const unit of units) {
    if (changed.some(file => file.startsWith(`${unit.rel}/`))) reach(unit)
  }
  return units.filter(unit => found.has(unit))
}

interface Manifest {
  name: string
  dependencies?: Record<string, string>
  devDependencies?: Record<string, string>
  peerDependencies?: Record<string, string>
}

/** A go.mod's module path and the modules it requires. */
export const parseGoMod = (text: string): { path: string; requires: string[] } => {
  const module = /^module\s+(\S+)/m.exec(text)?.[1]
  if (!module) throw new Error('a go.mod without a module line')
  const requires: string[] = []
  let inBlock = false
  for (const raw of text.split('\n')) {
    const line = raw.replace(/\/\/.*$/, '').trim()
    if (inBlock) {
      if (line === ')') inBlock = false
      else if (line) requires.push(line.split(/\s+/)[0]!)
    } else if (line === 'require (') inBlock = true
    else if (line.startsWith('require ')) requires.push(line.split(/\s+/)[1]!)
  }
  return { path: module, requires }
}

/** The directories a go.work uses, relative to it. */
export const parseGoWork = (text: string): string[] => {
  const dirs: string[] = []
  let inBlock = false
  for (const raw of text.split('\n')) {
    const line = raw.replace(/\/\/.*$/, '').trim()
    if (inBlock) {
      if (line === ')') inBlock = false
      else if (line) dirs.push(line)
    } else if (line === 'use (') inBlock = true
    else if (line.startsWith('use ')) dirs.push(line.slice('use '.length).trim())
  }
  return dirs.map(dir => path.normalize(dir))
}

/** The example pair or app a directory belongs to: `examples/<pair>`, `apps/<app>` — its first two segments. */
export const pairOf = (rel: string): string => rel.split('/').slice(0, 2).join('/')

/** The repository's units, from its pnpm workspace and its Go workspace. */
export const readUnits = (root: string): Unit[] => {
  const units = new Map<string, Unit>()
  const unit = (kind: UnitKind, rel: string): Unit => {
    const found = units.get(rel) ?? {
      kind,
      rel,
      provides: [],
      uses: [],
      packages: [],
      modules: []
    }
    units.set(rel, found)
    return found
  }
  const workspace = YAML.parse(readFileSync(path.join(root, 'pnpm-workspace.yaml'), 'utf-8')) as { packages: string[]; overrides?: Record<string, string> }
  const overridden = new Set(Object.keys(workspace.overrides ?? {}))
  for (const rel of workspace.packages) {
    const manifest = JSON.parse(readFileSync(path.join(root, rel, 'package.json'), 'utf-8')) as Manifest
    const into = rel.startsWith('examples/') ? unit('example', pairOf(rel)) : rel.startsWith('apps/') ? unit('app', pairOf(rel)) : unit('npm', rel)
    into.provides.push(manifest.name)
    into.uses.push(...Object.keys({ ...manifest.dependencies, ...manifest.devDependencies, ...manifest.peerDependencies }).filter(dep => !overridden.has(`${manifest.name}>${dep}`)))
    into.packages.push(rel)
  }
  const work = path.join(root, 'go.work')
  for (const rel of existsSync(work) ? parseGoWork(readFileSync(work, 'utf-8')) : []) {
    const { path: modulePath, requires } = parseGoMod(readFileSync(path.join(root, rel, 'go.mod'), 'utf-8'))
    const into = rel.startsWith('apps/') ? unit('app', pairOf(rel)) : unit('go', rel)
    into.provides.push(modulePath)
    into.uses.push(...requires)
    into.modules.push(rel)
  }
  return [...units.values()]
}

const git = (root: string, args: string[]): { ok: boolean; out: string } => {
  const run = Bun.spawnSync(['git', ...args], {
    cwd: root,
    stdout: 'pipe',
    stderr: 'pipe'
  })
  return { ok: run.exitCode === 0, out: run.stdout.toString() }
}

const lines = (text: string): string[] => text.split('\n').filter(Boolean)

/** What changed in the tree at `root` since `base`, and what that puts in question. */
export const affected = (root: string, base: string | undefined): Affected => {
  const units = readUnits(root)
  const from = base !== undefined && git(root, ['rev-parse', '--verify', '--quiet', `${base}^{commit}`]).ok ? base : null
  const changed = from === null
    ? lines(git(root, ['ls-files', '--cached', '--others', '--exclude-standard']).out)
    : [...lines(git(root, ['diff', '--name-only', '--no-renames', from]).out), ...lines(git(root, ['ls-files', '--others', '--exclude-standard']).out)]
  const before = (file: string): string | undefined => {
    const shown = git(root, ['show', `${from}:${file}`])
    return shown.ok ? shown.out : undefined
  }
  const wide = from === null ? [...SIDES] : SIDES.filter(side => JSON.stringify(READ_PINS[side](before)) !== JSON.stringify(READ_PINS[side](textAt(root))))
  const touched = new Set(affectedUnits(units, changed))
  const of = (side: Side): string[] => units.filter(unit => touched.has(unit) || wide.includes(side)).flatMap(HOLDS[side])
  return {
    wide,
    units: units.filter(unit => touched.has(unit) || wide.some(side => HOLDS[side](unit).length)),
    packages: of('js'),
    modules: of('go'),
    files: [...new Set(changed)].filter(file => existsSync(path.join(root, file)))
  }
}

/** The `--base=<ref>` among `args`, if one is given. */
export const baseArg = (args: string[]): string | undefined => args.find(arg => arg.startsWith('--base='))?.slice('--base='.length)

if (import.meta.main) console.log(JSON.stringify(affected(import.meta.dir, baseArg(Bun.argv.slice(2))), null, 2))
