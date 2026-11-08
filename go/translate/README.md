# greendrake.nz/go/translate

Renders a product's master — its default locale's translatable files — into
every locale it ships, through a language model, and keeps the result current
as the master changes: a web product's message catalogues, controlled
vocabularies, prose and templates, and a native app's Android string
resources and iOS String Catalog. Everything about the product is declared in
one manifest, and nothing about any product is coded here; the *host*, below,
is the product whose manifest a run reads.

The recommended way to render is [`translate-gpu`](gpu/aws/README.md): it checks
first, and only when strings need a model rents a GPU in AWS for the length
of the run, serves an open model on it and renders against that. Run this
command itself to check — in every build — or to render against a provider
of your own.

```
translate -manifest translate.yaml [-project PROJECT] [--check | --adopt | --settle] [--locale TAG] [--retranslate PREFIXES] [--parallel N] [--verbose]
translate carry -project PROJECT -from MANIFEST -source ID -to MANIFEST -target ID [-prefix PREFIX]... [-key TO=FROM]...
translate prune -manifest LIBRARY -project PROJECT [-project PROJECT]...
```

- `-project PROJECT`: the project manifest a library manifest is run on
  behalf of (below); a library manifest is refused without one.

- No flags: render what is stale in every shipped locale, write it, write the
  derived outputs, remove a de-listed locale's files, then run the host's
  verification.
- `--check`: report what is stale — a string, a policy, a file not as a run
  would write it — and write nothing. Exits 0 when current, 3 when strings need
  rendering (a run needs a model) and 4 when only files are behind (a run
  settles them without one). Reaches no model, so it can gate every build on
  any machine.
- `--adopt`: record the current policy as what the translations on disk were
  rendered under, rendering nothing (below).
- `--settle`: write every file and record as a run would from what the locales
  hold — dropping what the master no longer defines, reordering with it,
  writing the derived outputs, removing a de-listed locale — rendering nothing.
  What is owed stays owed. A pass with no units (a vocabulary of no terms) owes
  nothing under any policy.
- `--locale TAG`: one locale; nothing that belongs to the shipped set as a whole
  is settled, and nothing is verified.
- `--retranslate PREFIXES`: render units whose id starts with one of these
  again, current or not.
- `--parallel N`: requests in flight across the whole run, every locale drawing
  on the same slots; the manifest's `llm.parallel` otherwise.

The model is reached through [`greendrake.nz/go/llm`](../llm/README.md) —
Anthropic's API, or any OpenAI-compatible endpoint, a local runtime included —
configured from the environment variables under the prefix the manifest's
`llm.env_prefix` names.

A host with a Go module of its own runs it as a Go tool:

```
// go.mod
tool greendrake.nz/go/translate/cmd/translate
```

```
go tool translate -manifest path/to/translate.yaml --check
```

Any other host installs it: `go install greendrake.nz/go/translate/cmd/translate@latest`.

## How it works

**Units.** Every translatable thing is a *unit* with a stable id —
`<pass>:<key>`, the pass being the vocabulary or one of the manifest's sources —
its source text, the context a translator needs, and the checks its
translation is held to. A unit's translation lives in the file its consumer
already reads, and a unit absent from that file is work to do whatever else is
recorded — which is what a record alone would miss when a key is deleted by
hand or lost in a merge. A plural its source keys by CLDR category — an
Android `<plurals>`, a String Catalog's plural variation — is one unit whose
text, and translation, are its forms written as one JSON object in CLDR's
order, so that any form, line breaks and all, comes back exactly.

**Files.** Most formats give each locale files of its own. A String Catalog is
one file every locale shares: its source holds one lock over it, and a
locale's write reads it again, replaces that locale's part alone and writes
the whole back — so locales rendered at once lose nothing of each other's, a
check reports only the locale that differs, and dropping a locale removes its
part rather than the file. Every generated file opens with a header saying
what generated it and what an edit to it does, in the file's own comment
syntax — `#` in YAML, one `<!-- -->` after an XML declaration — and a format
with no comments, JSON, carries none.

**Digests.** Each locale's state file records two things. Per unit, a digest of
its source text and the locale's glossary; per pass, a digest of the *policy*
the pass's units were rendered under: the engine's prompt version, the master's
locale, the brief, the manifest's prompt rules, and the pass's own declarations
that reach a prompt — its guidance, syntax, markup, shape, limits, context
templates, copied blocks. A unit is stale when either moves, when it has no record, or
when its translation is missing. The state digests the source side only, never
the translation, so a correction made by hand in a generated file survives
until what it was rendered from moves; and a unit whose digests still match is
never sent, so a run with nothing stale makes no request and writes no file,
and a one-string edit costs one request. What does not reach a prompt — paths,
batch sizes, tiers, the model — is not policy, and changing it costs nothing.
A digest sees what a unit says, not where the master puts it, so a check also
holds each locale's files to what a run would write from what they hold: keys
or paragraphs reordered in the master move no digest and still move every
file.

**The policy and the glossary.** The host authors its policy in three files: a
brief every prompt opens with; terms, the words the product uses about itself,
each with what it means in the product; and a register: per target locale, one
English sentence saying how the locale addresses its reader, du or Sie —
decided once by the host rather than asked of a model on every run, and
required for every shipped locale. The brief and the register are files of
their own because manifests whose strings share a screen — a host and the
libraries it renders — must address the reader alike, and libraries made for
the same apps are described alike: they name the same ones. The terms are a
manifest's own. The vocabulary pass renders the terms first and alone in
each locale, into a glossary file every later prompt carries. That is what
stops one noun becoming three across fifty batches — and why every other unit's
digest folds in the glossary: correcting one term's rendering, or changing the
register, makes the whole locale stale.

**Batches.** Units are asked for in batches per pass, grouped as the pass
declares (never spanning a catalogue section or a file where that is asked
for), bounded in size, and never splitting a *bundle* — the fields of one row,
which are one message. Each request's JSON schema pins the answer's ids to the
ones asked for, so a model that would mangle a long id is held to a legal one.
Every batch is written as it finishes, with what it rendered, files and record
alike: a run that stops
partway leaves a partial locale, which a check reports as stale and the next
run continues from. A pass whose policy moved is rendered whole; its policy is
recorded as it starts, so a run that stops partway through it resumes owing
only what it had not rendered.

**The wire.** A model translates language and keeps placeholders; what it does
not do reliably is restructure or copy long opaque tokens. So what a unit
carries that is syntax rather than language crosses the wire as something it
handles well, and the renderer's syntax is put back by the engine:

- A plural is never asked for whole. A vue-i18n plural (`|`-separated forms),
  and a plural keyed by category, are asked for one form per target category,
  each showing the source's form for it (or its `other`) with the numbers that
  select it; an ICU plural's sentence is asked with the plural as an opaque
  `{count}` placeholder, and its cases one per category, `#` crossing as that
  placeholder too. A category a single number selects is told it may write the
  number as a word, or leave it out. The engine assembles the `|` list, the
  cases under the source's argument, or the keyed forms, in the target
  locale's CLDR categories.
- A printf conversion — `%[n$][flags][width][.precision][length]conversion` —
  crosses as the engine's own placeholder `{p1}`, `{p2}`, …, numbered by the
  argument it formats: its stated position, or its place among the text's
  conversions. The model never sees the conversion; it comes back as the
  source wrote it while the arguments keep the source's order, and with every
  conversion positional — `%2$@ … %1$@`, which Android and iOS both read —
  where the translation reordered, repeated or left one out. A `%%` crosses as
  the `%` it renders as; an iOS `%#@name@` substitution as `{name}`.
- An `<xliff:g>` span — an argument with its example, or a literal like `km`
  or `Gmail` that the span exists to protect — crosses whole as one
  placeholder named for its id, `{count}`; nothing in it reaches the model. Its
  conversions count among the arguments at the span's place, and are made
  positional with the rest, the span otherwise as written.
- A vue-i18n reference, `@:brand.name`, crosses as `{brand_name}`: a model
  reads the reference as a placeholder by another name and writes its own in
  its place otherwise.
- A tag's attributes cross as one bare attribute naming the element —
  `<a href="…" target="_blank">` as `<a attr1>`: a line of CSS or a URL has
  nothing to translate and is what a model duplicates a clause of. The tag
  stays a plain HTML tag, which a model reproduces as markup; a placeholder in
  its place, `<a {attr1}>`, reads as running text, and a model carries the
  sentence on after it and drops the `>`.

These tokens — the reference placeholders, each tag as it crosses, each span
and substitution — are the engine's own encoding, and it checks them back by
identity, each exactly as often as the source has it, whatever the host's
syntax says a placeholder is. An attribute marker written anywhere but in its
tag is refused. A source carrying both something the wire hides and the
placeholder it would hide it behind fails its batch: one could not be told
from the other on the way back.

The answer comes back the same way it goes out: every character written as
itself. A text spelled in `\u` escapes is refused however well it decodes — a
model that drifts into ASCII-only JSON writes a script like Greek at several
times the tokens, loses its words doing so, and runs into the cap mid-string.
The prompt says so, and the retry is told.

**Checks.** Nothing reaches a file unchecked, and nothing degrades to the
master's language: a rejected item is asked for once more, at the strong tier,
with every complaint about it attached and what it was answered with quoted,
as three renderings — what it wrote with the complaint fixed, then the same
said another way, each shorter than the one before where it was over its
limit — and the first that passes is kept. A model cannot count characters:
shown its answer and told it is one too long, it writes the same phrase again,
where asked for ever shorter ones it writes one that fits, and the engine does
the counting. When no rendering passes, the item fails, with the rest of its
row where it is one of a row's fields.

An answer cut off at the length limit — a model looping inside one string,
as a rule — is not asked for again whole: every item it finished is kept and
checked like any other, the item it was cut off writing is asked for again at
the strong tier as a string that never ended, its beginning quoted, and the
items it never reached are asked for again as a batch of their own. One
looping string costs that string's retry, not its batch. An answer cut off
before any item in it is whole fails its batch.

What a unit is held to follows from its declarations:

| Declared | Checked |
|---|---|
| anything | the source's HTML tags (attributes included), `@:` references, `\|` and `\` characters as multisets — none added, none dropped: each is syntax to some renderer, and where the unit's does not read it the reader sees it; a backslash is also how a model barred from `\u` escapes spells one out as text. Every letter in the locale's own script, Latin, or a script the source carries — a letter of no script in particular (Japanese `ー`, the ʻokina) is anyone's: a word of another language slipped in is refused when its script gives it away |
| syntax `icu` | `{name}` placeholders as a set; the plural's cases are the locale's categories; the message parses as [`greendrake.nz/go/i18n`](../i18n/README.md)'s `messageformat` renders it |
| syntax `vue-i18n` | `{name}` placeholders as a set, form by form, with the single-number licence; one form per category |
| syntax `braces` | `{name}` placeholders as a set |
| syntax `printf` | conversions keyed by argument as a set — a conversion's letter is the source's by construction — with the single-number licence in a plural's form, and none left out while a later one is kept: a C-style formatter cannot skip an argument it is never told the type of; `%` characters as a multiset; substitutions and protected spans by identity. A source mixing stated positions with none, or a format string with a `%` that starts no conversion, is refused |
| syntax `none` | nothing more (literal text) |
| markup `html` | the tags properly nested |
| markup `markdown` | the same, plus link targets as a multiset, heading levels and list kinds in order, and a numbered list numbered in ASCII digits — the only ones markdown reads as a list |
| shape `line` | no line break |
| a field's `limit` | at most that many characters, counted on the text as it is written — the renderer's syntax restored, since that is what the field receives |

Order is not held: a sentence's word order is the translation's own, and its
placeholders and elements move with the words. Nor is a placeholder's count:
the renderer substitutes by name, and a language with no pronoun for "them"
names `{name}` twice. The system prompt's list of
checked rules is built from the checks the batch's units actually resolve to,
and the rules nothing checks — mood, what an id says, the host's own rules —
are stated apart from it, so a model told its answer failed "a checked rule"
can tell which.

**Failures.** An item rejected twice costs itself, with the rest of its row; a
request that outran its timeout, or that the endpoint refused, costs its own
items. They stay owed, and everything else — the rest of their batch, their
locale and the others — is rendered and written. A failed vocabulary holds its
locale back, since every other string is rendered against the glossary it
settles. The run then fails, listing each failure, and the next run owes
exactly those. What no request would escape — an endpoint that does not answer at all,
a file that cannot be written — stops the run at once.

**Requests.** Every locale renders at once, drawing on one set of `--parallel`
slots, so a locale's vocabulary and its last batches — each running alone —
leave no slot idle while other locales have work. A request's cap is its
answer's size — the text and ids in proportion to the source, plus the JSON
framing, which is most of a batch of one short label; what a model that
reasons first spends on that is the
deployment's `<prefix>REASONING_TOKENS`, added by `greendrake.nz/go/llm`. Kept to the
answer, the cap is what ends a model looping inside its answer — as truncated
JSON and a retry — before the request's timeout does.

**Completeness and verification.** A full run ends when every unit of every
locale has a translation, then runs the host's verify commands — its own
consumers' loaders over what was just written, which are what would otherwise
find a defect at boot or at build.

## `--adopt`

A policy change makes every unit of the passes it touches stale — a reworded
brief, a clarified guidance line, a prompt-version bump in an engine upgrade.
Whether that should cost a render is the host's judgement, since only the host
knows whether the text on disk still says what it should. `--adopt` is the
answer when it does: it records the current policy for every locale that has a
record, writes the derived outputs, calls no model and writes no translation.
A unit whose own source moved stays stale — adoption vouches for the policy,
never for a text. Adopt before rendering: a pass already partly rendered under
the new policy has dropped the records of what it had not reached.

## `carry`

```
translate carry -project host/translate.yaml -from host/translate.yaml -source ui -to lib/translate.yaml -target auth -prefix auth.
translate carry -project host/translate.yaml -from host/translate.yaml -source ui -to host/translate.yaml -target ui -key menu.settings=auth.menu.settings
```

Rearranging where strings are kept must not cost a render. `carry` hands each
locale's translations from one catalogue source's keys to another's: a library
taking over what its host used to say itself (`-prefix`: every target key under
it, from the same key), a key renamed, a new key saying exactly what an existing
one already says (`-key to=from`). The masters are authored and change by hand
first; `carry` moves what only the engine writes. Both sources must be
catalogues. A carry is for a project, `-project`: it carries the target's
locales, a library manifest among `-from` and `-to` being read on the
project's behalf. `-prefix ''` names every key: a pass carried whole, from one
manifest's state to another's over the same files.

A translation is carried only where it still holds: the source records it as
rendered from the very text the target's master now holds, under the source's
current policy and glossary. It arrives recorded as current in the target — so
the target's check does not count it — unless the target owes the whole pass a
render anyway (its policy moved, its vocabulary not yet rendered), when it
arrives as words kept until that render. A translation that does not hold — the
text changed on the way, the source's record stale — is not carried, and the
target owes it.

The source is left alone: a key it still defines keeps its own, and one it no
longer defines leaves its files and records with `--settle` or its next run.
Settle after every carry is done, not between: a source settled early has
nothing left to carry. A carry within one source (a rename) writes that source's
files as a run would, which settles them — carry anything else out of it first.

## Library manifests

A *library* — a component library with strings of its own, served by several
products — has a manifest of its own: its strings are rendered into the
locales of the projects that serve it, and no others. The manifest declares
its master locale (`default`, the locale its own catalogue is authored in) and
no locales — no `command`, `shipped`, `llm` or `derived` — and is run on
behalf of a project's (`-project`), which is refused unless its master is the
library's: in its shipped locales, on its model; `--locale` and `--retranslate` act within those
locales and `--check` reports those alone. The files it generates name the library's manifest rather than a
project's command, so each project serving the library writes the bytes the
other's check compares against, and a run leaves every locale outside its
project's set alone: those are other projects'.

```
translate prune -manifest lib/translate.yaml -project host/translate.yaml -project other/translate.yaml
```

What no serving project ships any more is `prune`'s to remove: given every
project serving the library — the library read on each one's behalf, so each
one's master is the library's — it removes the files of each locale the
library's record holds outside their locale sets.

## The manifest

YAML, validated on load before anything is read: an unknown key, format, check
or template placeholder, a non-canonical tag, a shipped locale with no register
or written in a script the engine has no letters for, all fail hard. Paths are
relative to the manifest.

```yaml
command: go tool translate -manifest translate.yaml   # how a person runs this; named in headers and reports
default: en-GB                            # the authored master
shipped: [en-GB, en-US, de, ru]           # every locale produced, the master included
state: translation/state                  # <dir>/<tag>.yaml

policy:
  path: translation/policy.yaml           # terms — authored
  brief: translation/brief.yaml           # brief — authored
  register: translation/register.yaml     # <tag>: how it addresses the reader — authored
  glossary: translation/glossary          # <dir>/<tag>.yaml — the rendered terms

vocabulary:                               # the pass that renders the terms
  id: gl
  batch: {size: 100}
  tier: strong
  guidance: Each id is a term to render into {language}. …

llm:
  env_prefix: TRANSLATE_LLM_              # greendrake.nz/go/llm's ConfigFromEnv prefix
  timeout: 10m                            # per request, unless the environment says
  parallel: 4                             # unless --parallel says

prompt:
  keep_verbatim: [App Store, Google Play] # names that stay as their owners write them
  rules: []                               # host rules, stated among the unchecked ones

sources: […]                              # below

derived:
  - kind: locale_tables                   # for a web frontend: i18n's plurals.Categories and negotiate.Table per shipped locale
    path: locale_tables.json
    comment: |
      What the file is, for whoever opens it.

verify:                                   # the host's own loaders, run over every full write
  - {run: [go, run, ./cmd/verify-l10n], cwd: .}
```

Every pass — the vocabulary and each source — declares `id` (the unit-id
prefix), `tier` (`fast` | `strong`), `batch` (`by`: `file` or, for a catalogue,
`first-segment`; `size`, zero for unbounded) and `guidance`, which reaches that
pass's prompts and no other's; `{language}` in it is the target language's name.

Every source declares its `format`, and the checks its units are held to:
`syntax` (`icu` | `vue-i18n` | `braces` | `printf` | `none`), `markup`
(`html` | `markdown` | `none`) and, optionally, `shape` (`line` | `text`, the
default). Each format takes its own keys and refuses the rest.

**`yaml-catalogue`** — one nested message catalogue per locale; a unit per leaf
string, keyed by its dotted path, its context every comment above it and its
sections. A locale's file clones the master's structure.

```yaml
- id: ui
  format: yaml-catalogue
  path: catalogue/{locale}.yaml           # the master is this at `default`
  syntax: vue-i18n
  syntax_overrides:                       # keys the backend renders, in its syntax
    - {prefix: seo., syntax: icu}
  markup: html
```

**`yaml-tree`** — controlled vocabularies, an attribute's values as a tree, one
file each: a key, a label per locale, and nodes nested under a children key,
each with a code and names per locale. A unit
per label and per node, a node's id composed from its tree's key and its code,
its context told its ancestry. A locale's names may sit inline in the master;
the engine writes them to a sidecar per locale, `{<key>: {label, nodes: {<code>:
name}}}`, which wins.

```yaml
- id: tax
  format: yaml-tree
  path: taxonomies/*.yaml                 # the master's files
  sidecar: taxonomies/names/{locale}.yaml
  keys: {key: key, label: label, nodes: nodes, code: code, names: names, children: children, skip_node_names: proper_nouns}
  node_id: "{key}:{code}"
  label_context: 'The name of the "{key}" attribute.'   # {key}
  context: 'A value of the "{key}" attribute ({label}).' # {key}, {label}; the ancestry is appended
  syntax: none
  markup: none
  shape: line
```

`skip_node_names` names a boolean a tree file sets when its nodes are names
rather than words — brands, models: its label is translated, its nodes are not.

**`text-blocks`** — prose, one file per page per locale, split into blocks at
blank lines. A block's id is its own digest under its page's id, so a block
that moves keeps its translation and an inserted one is the only work; the
block order a locale's file was written in is recorded, since that file is
addressed by order alone.

```yaml
- id: content
  format: text-blocks
  path: content/*/{locale}.txt            # `*` is a page's name; the pages are the master's
  page_id: "/{name}"
  copy_blocks: ["::TOC::"]                # reproduced rather than translated
  context: One block of the {page} page.  # {page}, {name}
  syntax: braces
  markup: markdown
```

**`yaml-rows`** — a list of rows, each a message written in several fields
under an id and a locale. The master holds the default locale's rows, and may
hold others'; each locale's translations go to a file of its own. A row's
fields are one bundle, and a row is written only whole.

```yaml
- id: email
  format: yaml-rows
  master: email_templates.yaml
  path: email_templates/{locale}.yaml
  row: {id: template_id, locale: locale}
  fields:                                 # in the order they are written
    subject: {shape: line, limit: 78, describe: The subject line}
    body: {markup: html, describe: The body}
  context: '{describe} of an email (template "{id}").'   # {describe}, {id}, {field}
  syntax: icu
  markup: none                            # a field's own markup and shape win
```

A field's `limit` is the most characters its translation may have, for text
bound for a field that caps it — a store's subtitle, a form's title. Each
item's context states it ("At most 78 characters."), and the prompt's checked
rules say what that note means.

**`android-strings`** — an Android app's string resources: the authored
`res/values/strings.xml`, and each locale's file in the directory named for its
resource qualifier — `de` for `de`, `en-rGB` for `en-GB`, BCP 47 form for
anything else (`b+zh+Hans`, `b+es+419`); a shipped tag Android names no
directory for is refused. A `<string>` is a unit keyed by its name, a
`<plurals>` a keyed plural (Android's quantities are CLDR's categories), and
each item of a `<string-array>` a unit keyed `name[i]`, an array's items one
bundle. The comments directly above a resource are its context. A resource
marked `translatable="false"` is left alone, one whose whole value is a
reference (`@string/…`) is copied as it is, and one marked `formatted="false"`
is literal text. A unit's text is what Android resolves the value to — its
quoting, whitespace and backslash escapes applied — with its markup kept (its
own `&` and `<` as entities); a value is escaped back as it is written. A
locale's file keeps the master's order, holds only translatable resources —
an array only whole — gives each plural the target locale's quantities, opens
with the XML declaration and the header, and is read back before it is
written.

```yaml
- id: app
  format: android-strings
  master: app/src/main/res/values/strings.xml
  path: app/src/main/res/values-{locale}/strings.xml   # {locale}: the qualifier
  syntax: printf                          # what an Android string is
  markup: html                            # <b>, <i>, <u>, <a>, <xliff:g>
```

**`xcstrings`** — an iOS or macOS app's String Catalog, one file every locale
shares. Its `sourceLanguage` must be the manifest's `default`, and its version
one this reader knows. Each entry is translated from its source language's
localization, or from its key where it has none, its `comment` the context;
an entry marked `shouldTranslate: false`, or `extractionState: stale`, is left
alone. A plural variation is a keyed plural; each device variation a unit of
its own, keyed `<key>#<device>`; each substitution's plural a keyed plural
keyed `<key>%<name>`, the string carrying `%#@name@` held to it as printf. An
entry's units are one bundle. A key is often the English text and may hold any
character: one that is not an identifier is addressed by its digest, `k` and
12 hex digits — the words reach the model as the unit's text, never its id. A
locale's localization of each entry is replaced, its values marked
`translated`, and the catalog is written as Xcode saves it — two-space indents,
`"key" : value`, every object's keys in code-point order, no newline at the
end — so a save from Xcode moves no byte.

```yaml
- id: ios
  format: xcstrings
  path: App/Localizable.xcstrings         # the one catalog
  syntax: printf
  markup: none
```

Unit ids are what a locale's record is keyed by: a declaration that composes
them differently — `node_id`, `page_id` — makes every unit it covers at once
removed and new, and everything it covers is rendered again. A host holds
these to what its own readers derive in its tests.
