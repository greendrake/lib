# greendrake.nz/go/i18n

Localisation for a Go service at run time: the backend of a web app, or the
API behind a native one. It renders a message in a locale, lists a locale's
plural categories, and resolves the locale a request asks for against the
locales the product ships. It also renders on the server from the message
catalogue that the product's frontend is built from.

```sh
go get greendrake.nz/go/i18n
```

## Why

The frontend of a localised product is not the only part that writes to its
users. The backend often sends emails and push notifications, renders a page's
title and description, and decides which locale a request gets. The obvious
ways to do that go wrong. A string formatted in code is right for English
only: CLDR gives Russian four plural categories and Arabic six. Strings kept
apart for the backend are translated apart, and drift from the frontend's.
Locale tags compared as text do not find that `zh-TW` should get `zh-Hant`.

This module renders messages with CLDR plural selection, from the very files
the frontend serves. It matches locale tags with `golang.org/x/text`'s
matcher, and gives a web frontend the server's answers so that the two agree.

## Trade-offs

- **A subset of MessageFormat.** `messageformat` reads `{name}` and
  `{name, plural, …}`. A placeholder of any other kind is refused: there is no
  `select`, no `selectordinal`, no number or date format, and no `offset:` in
  a plural. There is no apostrophe quoting either, so a message cannot contain
  a literal `{` or `}`. If your messages need more, use a full MessageFormat
  implementation.
- **The CLDR data is `golang.org/x/text`'s.** Plural categories and tag
  matching follow that release, which is older than a browser's. Where the two
  differ, the server's answer is the one both sides use, so the frontend has
  to take `Categories` and `Table` from the server.
- **Negotiation takes one tag.** `Match` does not parse an `Accept-Language`
  header or weigh a list of preferences: the caller chooses the tag to ask
  about.

## Packages

Each job is a package of its own, usable apart:

- `messageformat` renders messages with CLDR plural selection. The plural
  forms are written in each locale's message, and the calling code passes a
  count or a name.
- `catalogue` reads the messages the backend renders from the very files the
  frontend serves. They are written and translated once.
- `negotiate` and `locales` resolve a tag with `golang.org/x/text`'s matcher.
  `negotiate` also writes its answers out as a table, so that a browser gives
  the answers the server gives.
- `plurals` lists each locale's plural categories, from the same tables the
  renderer selects with. A web frontend takes that list in place of the
  browser's own.

A render succeeds or returns an error. A missing argument, an unknown key and
a locale that is not shipped are errors: there is no half-rendered string and
no raw key.

The module depends on `golang.org/x/text`, and on `gopkg.in/yaml.v3` for
reading a locale declaration and a catalogue.

## `messageformat`

Renders a subset of ICU MessageFormat, and no more of it:

```
{name}                                   argument substitution
{name, plural, =N {…} one {…} other {…}} CLDR plural selection
```

- Inside a plural case, `#` renders the count, formatted for the locale. `=N`
  is an exact match, and it takes precedence over the keyword categories
  (`zero`, `one`, `two`, `few`, `many`, `other`). `other` is the fallback.
  When a count matches no case and there is no `other`, the render fails.
  Outside a plural, `#` is a literal.
- A plural's count must be a Go `int`: no fractional count selects a form.
- There is no apostrophe quoting: `'` is always literal, and a literal `{` or
  `}` cannot be written.
- Few values are formatted for the locale. An integer is, and so is a currency
  amount (`golang.org/x/text/currency`'s `Amount`). Any other value, a float
  or a date among them, is written as Go's `%v` prints it, so format it before
  passing it in.
- `Render(tag, msg, args)` returns an error for malformed input. It also
  returns one for a name that the message references and `args` does not
  supply. It never returns half a string. `Validate(msg)` is the syntax check
  alone. It is for a startup pass over a catalogue, whose arguments exist only
  at render time.
- `EscapeArgs`, `EscapeText`, `Anchor` and `SafeHTML` prepare arguments for an
  HTML body. Plain strings are escaped, and their newlines become `<br>`. A
  `SafeHTML` value passes through as trusted markup: the caller has assembled
  it from parts it escaped itself. An `Alternative` holds a rendering for each
  format: its `Text` for a text render, and its `HTML` for an HTML body. It is
  for a value such as a message that a person wrote, which a push notification
  shows as written and an email shows as HTML.

Plural selection uses the message's own locale. So the wording of each plural
form is in the message, which is written for one locale, and call sites pass
language-neutral values.

## `plurals`

Says which plural categories a locale's messages have, and which numbers
select each. The answers are derived from the same CLDR tables that
`messageformat` selects with. A translation holding any other set of
categories would have forms the renderer never reaches.

- `All()` — every CLDR category, in CLDR order.
- `Categories(locale)` — the locale's categories, in CLDR order, always ending
  in `other`.
- `Examples(locale)` — for each category, numbers that select it, to show a
  translator. They are the first consecutive run counting from 1: `1` for
  Russian's `one`, not `1, 21, 31`. A category that no positive integer
  reaches gets the fractions that reach it, or `0` where only zero does.
- `Exact(locale)` — the categories that a single number selects, each with
  that number. Arabic's `one` is 1 and nothing else. A form for such a
  category may write its number as a word, or leave it out.

A browser's CLDR is newer than `x/text`'s, and the two disagree on a few
locales. That is why a web frontend is given `Categories` rather than deriving
its own.

## `negotiate`

Resolves a requested tag against the set of shipped locales. A request for
`zh-TW` should get `zh-Hant`, one for `es-MX` should get `es-419`, and `no`
should get `nb`. No comparison of prefixes finds those.

- `New(shipped, default)` — the tags must be canonical BCP-47 and include the
  default.
- `Match(requested)` — asks `golang.org/x/text/language`'s matcher. Its answer
  is taken when its confidence is Exact, or when it is High and the answer is
  in the language that was asked for. Otherwise `Match` answers the default.
  The second condition keeps a Danish visitor out of a Norwegian UI. The
  matcher offers a neighbouring language at High confidence, and a language
  nobody asked for is not a better answer than the default.
- `Table()` — the same answers as a table, for a web frontend. It holds the
  answer for every language, script and region a browser can send, keyed by
  language subtag. A browser cannot reproduce the matching itself:
  `Intl.Locale.maximize` turns `es-MX` into `es-Latn-MX`, which is not
  `es-419`. So the frontend applies this table.
- `Language(tag)` — a tag's language, canonicalised (`tl` is `fil`).

## `locales`

The set of locales a product ships: every locale, the default among them, and
which of them a requested tag gets.

- `Load(path)` — reads the set from the top-level `default` and `shipped` keys
  of a YAML file, whatever else the file declares. So the set is read from the
  file that already declares it, such as a localisation manifest.
  `New(shipped, default)` builds the set from values. Both validate through
  `negotiate.New`.
- `Set.Default`, `Set.Shipped`.
- `Set.Resolve(requested)` — `negotiate`'s `Match` over the set. It is a plain
  `func(string) string`, for a request layer to call.

## `catalogue`

Renders on the server from a product's message catalogue. The catalogue is the
frontend's strings: one YAML file per locale, nested, with every leaf a
message addressed by its dotted path, as vue-i18n and similar libraries load
them. The backend renders from some subtrees of it: an email's strings, a
notification's, a page's title and description. It reads them from the very
files the frontend serves, so they are written and translated once, with the
app's.

- `Load(Options)` — reads `<Dir>/<locale>.yaml` for every locale of
  `Options.Locales`, and keeps only the keys under `Options.Prefixes`. A
  prefix `seo` is every `seo.*` key, and the rest of the catalogue is the
  frontend's alone. Each locale is the default's messages overlaid with its
  own, which is the fallback the app applies. So a key that a translation
  lacks is rendered in the default language here, as it is in the app. `Load`
  returns an error for a message the renderer would not render as written,
  rather than leaving it to fail at the first render. That is a message
  invalid for `messageformat`, a value that is not a string, or a vue-i18n
  plural (`|`-separated forms) or link (`@:key`). The frontend's renderer
  reads that syntax, and `messageformat` would write it out as literal text. A
  prefix under which the default defines nothing is an error too.
- `Options.Constants` — values that every render gets along with its own
  arguments. A call's argument of the same name wins. They are for a value
  such as a brand name, stated once.
- `Catalogue.Message(locale, key, args)` — renders a key in a shipped locale.
  A locale that is not shipped, an unknown key, and a message the arguments do
  not satisfy are each an error. The answer is never another language's text,
  the key or the raw source.
- `Catalogue.Fingerprint()` — a digest of the shipped locales, their messages
  and the constants. It changes exactly when something a render reads changes,
  so a service that keeps rendered output can tell when it is stale.
