# @greendrake/domains

Email address and domain name validation: structural checks plus a real-TLD check against the bundled IANA root-zone list. Ships as TypeScript source (`src/main.ts`) with no build step, no `.d.ts` and no runtime dependencies; `sideEffects: false`.

## Install

```sh
bun add @greendrake/domains
```

`npm install @greendrake/domains` works equally. The source imports `src/tlds.json` as a module, so the consuming project's TypeScript config needs `resolveJsonModule` (set by `@greendrake/dev-config`'s base tsconfig).

## Usage

```ts
import { isDomainValid, isEmailValid } from '@greendrake/domains'

isDomainValid('sub.example.co.nz') // true
isDomainValid('  EXAMPLE.COM  ') // true: trimmed, case-insensitive
isDomainValid('example') // false: single label
isDomainValid('example.invalidtldxyz') // false: TLD not in the IANA list
isDomainValid('-bad.com') // false: label starts with a hyphen

isEmailValid('user+tag@example.com') // true
isEmailValid("o'brien@example.co.nz") // true
isEmailValid('user@example') // false
```

### `isDomainValid(d: string | null | undefined): boolean`

Trims and lower-cases the input, then requires:

- two or more dot-separated labels, each 1–63 characters of `[a-zA-Z0-9-]`, neither starting nor ending with a hyphen;
- the last label to be in the bundled TLD set.

`null`, `undefined` and `''` are invalid. Labels are ASCII only: internationalised names must be in punycode (`xn--…`) form, which is also how IDN TLDs appear in the list.

### `isEmailValid(v: string): boolean`

Requires a local part of one or more characters from letters, digits, `'` and `` . ! # $ % & * + / = ? ^ _ ` { | } ~ - ``, exactly one `@`, and a domain part that passes `isDomainValid`. The input as a whole is not trimmed.

## TLD list

`src/tlds.json` is a JSON array of every TLD in the IANA root zone, lower-cased, IDN TLDs in punycode form. Refresh it with

```sh
bun run update-tlds
```

`scripts/update-tlds.ts` runs under Bun: it fetches `https://data.iana.org/TLD/tlds-alpha-by-domain.txt`, fails on a non-2xx response, trims and lower-cases each line, drops blank lines and `#` comments, refuses to overwrite when fewer than 1000 entries result (a truncated download), then writes the array to `src/tlds.json` pretty-printed and reports the count.
