# greendrake.nz/go/i18n

What a Go service serving a localised product needs at run time — a web app's
backend, or the API behind a native one: rendering a message in a locale,
knowing how a locale counts, resolving the locale a request asks for against
the set the product ships, and rendering on the server from the message
catalogue the product's frontend is built from. One package each, usable
apart.

Its dependencies are `golang.org/x/text` and `gopkg.in/yaml.v3`, for reading a
locale declaration and a catalogue.

## `messageformat`

Renders a subset of ICU MessageFormat, and no more of it:

```
{name}                                   argument substitution
{name, plural, =N {…} one {…} other {…}} CLDR plural selection
```

- Inside a plural case, `#` renders the count, formatted for the locale; `=N`
  is an exact match that takes precedence over the keyword categories
  (`zero`, `one`, `two`, `few`, `many`, `other`); `other` is required. Outside
  a plural, `#` is a literal.
- There is no apostrophe quoting: `'` is always literal, and a literal `{` or
  `}` cannot be written.
- A currency amount (`golang.org/x/text/currency`'s `Amount`) renders in the
  locale's format.
- `Render(tag, msg, args)` errs on malformed input and on a name the message
  references but `args` does not supply, rather than emitting half a string.
  `Validate(msg)` is the syntax check alone, for a startup pass over a
  catalogue whose arguments only exist at render time.
- `EscapeArgs`/`EscapeText`/`Anchor`/`SafeHTML` prepare arguments for an HTML
  body: plain strings are escaped (newlines become `<br>`), a `SafeHTML` value
  passes through as trusted markup — a caller's own assembly of escaped parts.
  An `Alternative` carries a rendering for each format, its `Text` for a text
  render and its `HTML` for an HTML body: a message someone wrote, as a push
  says it and as an email shows it.

Plural selection uses the message's own locale, so grammar lives in the
locale-scoped message and call sites pass language-neutral values.

## `plurals`

Which plural categories a locale's messages carry, and which numbers select
each — derived from the same CLDR tables `messageformat` selects with, so a
translation holding any other set of categories would have forms the renderer
never reaches.

- `All()` — every CLDR category, in CLDR order.
- `Categories(locale)` — in CLDR order, always ending in `other`.
- `Examples(locale)` — per category, the numbers a translator is told select it:
  the first consecutive run from 1 (`1` for Russian's `one`, not `1, 21, 31`),
  fractions for a category no integer reaches, `0` for one only zero does.
- `Exact(locale)` — the categories a single number selects, and that number
  (Arabic's `one` is 1 and nothing else); a form for one of these may write its
  number as a word, or leave it out.

A browser's CLDR is newer than `x/text`'s and disagrees on a few locales, which
is why a web frontend is handed `Categories` rather than deriving its own.

## `negotiate`

Resolves a requested tag against a shipped set.

- `New(shipped, default)` — the tags must be canonical BCP-47 and include the
  default.
- `Match(requested)` — the matcher's answer when it is Exact, or High and about
  the same language; the default otherwise. The second condition keeps a Danish
  visitor out of a Norwegian UI: the matcher offers a neighbouring language at
  High confidence, and a language nobody asked for is not a better answer than
  the default.
- `Table()` — a web frontend's copy of the oracle: the verdict for every language,
  script and region a browser can send, keyed by language subtag. A browser
  cannot reproduce the matching itself (`Intl.Locale.maximize` turns `es-MX`
  into `es-Latn-MX`, which is not `es-419`), so it applies this table.
- `Language(tag)` — a tag's language, canonicalised (`tl` is `fil`).

## `locales`

A product's shipped set, the one object its locale decisions go through.

- `Load(path)` — the top-level `default` and `shipped` keys of a YAML file,
  whatever else it declares, so the set is read from the file that already
  declares it, a localisation manifest say; `New(shipped, default)` from
  values. Both validate through `negotiate.New`.
- `Set.Default`, `Set.Shipped`.
- `Set.Resolve(requested)` — `negotiate`'s `Match` over the set, a plain
  `func(string) string` for a request layer to call.

## `catalogue`

Rendering on the server from a product's message catalogue — the frontend's
strings, one YAML file per locale, nested, every leaf a message addressed by
its dotted path, as vue-i18n and its kin load them. The backend reads the
subtrees it renders from — an email's strings, a notification's, a page's
title and description — from the very files the frontend serves, so they are
authored and translated once, with the app's.

- `Load(Options)` — reads `<Dir>/<locale>.yaml` for every locale of
  `Options.Locales`, keeping only the keys under `Options.Prefixes` (a prefix
  `seo` is every `seo.*` key; the rest of the catalogue is the frontend's
  alone). Each locale is the default's messages overlaid with its own — the
  fallback the app applies — so a key a translation lacks reads in the
  default language here as it does in the app. It errs, rather than failing
  at the first render, on a message the renderer would not render as written:
  invalid for `messageformat`, a value that is not a string, or a vue-i18n
  plural (`|`-separated forms) or link (`@:key`), syntax the frontend's
  renderer reads and `messageformat` would emit as literal text. A prefix the
  default defines nothing under is an error too.
- `Options.Constants` — values every render is given beneath its own
  arguments (a call's argument of the same name wins): a brand name, stated
  once.
- `Catalogue.Message(locale, key, args)` — renders a key in a shipped locale. A
  locale not shipped, an unknown key and a message the arguments do not satisfy
  are errors, never another language's text, the key or the raw source.
- `Catalogue.Fingerprint()` — a digest of the shipped locales, their messages
  and the constants: it moves exactly when what a render reads does, so a
  service keeping rendered output can tell it is stale.
