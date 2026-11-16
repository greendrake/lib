# greendrake.nz/go/translate

Keeps a product's translations up to date with its source text. The source is
the product's *master*: the translatable files of its default locale. This
command translates the master into every locale the product ships, through a
language model. Afterwards it translates only what a change to the master has
made out of date. It reads a web product's message catalogues, controlled
vocabularies, prose and templates, and a native app's Android string resources
and iOS String Catalog. One manifest declares everything about the product,
and nothing about any product is coded here. The product whose manifest a run
reads is the *host* below.

```sh
go install greendrake.nz/go/translate/cmd/translate@latest
```

## Why

A product that ships in several languages has to translate its text again with
every release: each string added, reworded or renamed has to reach every
locale. People can keep up with that, at the price of a round trip to a
translator for every change. A language model can do the translating, but
handed the files it goes wrong. It rewords strings that did not change, breaks
placeholders and plurals, and translates one of the product's terms three
ways. And nothing records which strings are out of date.

This command sends a model only the strings whose source changed. It checks
every answer's syntax before writing it, and translates the product's terms
first, into a glossary for every later prompt. A build can ask it whether the
translations are current.

## Trade-offs

- **A model translates, and only form is checked.** The command checks a
  translation's placeholders, tags, plural forms, length limit and script.
  Nothing checks that a sentence means what its source means, or that it reads
  well. That depends on the model, and on what the host tells it about the
  product.
- **A correction made by hand lasts until its source changes.** An edit to a
  generated file is kept while what the string was translated from stays as it
  was: its source text, the locale's glossary, and the *policy*, which is the
  instructions its prompts include. When one of them changes, the string is
  translated again and the edit is gone. Nothing pins a translation, and there
  is no hand-off format for translators. Where people translate or review
  every string, use a tool built around that.
- **A build tool over files in a repository.** The translations, each locale's
  state file and its glossary are files the host commits. Nothing is
  translated at run time, so text that exists only at run time, such as what
  users write, is outside it.
- **Six file formats.** The command reads the formats listed under
  [The manifest](#the-manifest). A source in any other format is not read.

## Overview

- **Only what changed is translated.** Every translatable thing is a *unit*,
  with a recorded digest of what it was translated from. A unit is *stale*
  when that has changed. A run with nothing stale makes no request, and a
  one-string edit costs one request.
- **Syntax is the command's job.** Plurals, printf conversions, references and
  tag attributes are sent to the model in a form it handles well, and the
  command puts the original syntax back. Every answer is checked before it
  reaches a file. A string that fails is asked for once more. If it fails
  again, it is left untranslated: it is never written unchecked, and never
  filled in with the master's text.
- **The terms are translated first.** The product's own words are translated
  once for each locale, into a glossary that every later prompt includes.
- **A build can refuse stale translations.** `--check` reaches no model and
  writes nothing. It reports what is out of date, so a build on any machine
  can refuse to ship translations that are out of date.

Translations are kept in the files the product already reads, so there is
nothing to export or import. A run that stops partway has written every batch
it finished, and the next run continues from there.

## The command

The recommended way to translate is [`translate-gpu`](gpu/aws/README.md). It
checks first. Only when strings need a model does it rent a GPU in AWS for the
length of the run, serve an open model on it and translate against that. Run
this command itself to check, in every build, or to translate against a
provider of your own.

```
translate -manifest translate.yaml [-project PROJECT] [--check | --adopt | --settle] [--locale TAG] [--retranslate PREFIXES] [--parallel N] [--verbose]
translate carry -project PROJECT -from MANIFEST -source ID -to MANIFEST -target ID [-prefix PREFIX]... [-key TO=FROM]...
translate prune -manifest LIBRARY -project PROJECT [-project PROJECT]...
```

- `-project PROJECT`: the project manifest that a library manifest is run on
  behalf of (below). A library manifest is refused without one.

- No flags: translate what is stale in every shipped locale, write it, write
  the derived outputs (the manifest's `derived`), remove the files of a locale
  taken out of `shipped`, then run the host's `verify` commands.
- `--check`: report what is stale — a string, a policy, a file not as a run
  would write it — and write nothing. Exits 0 when everything is current, 3
  when strings need translating (a run needs a model) and 4 when only files
  are out of date (a run rewrites them without a model). Reaches no model, so
  every build on any machine can run it.
- `--adopt`: record the current policy as the one that the translations on
  disk were made under, and translate nothing (below).
- `--settle`: translate nothing, and write every file and state file as a run
  would from the translations the locales have. That drops what the master no
  longer defines, puts the files in the master's order, writes the derived
  outputs and removes a locale taken out of `shipped`. What needs translating
  still does. A source with no units needs nothing under any policy, and nor
  does a vocabulary (the product's terms) of no terms.
- `--locale TAG`: one locale. The derived outputs and the removal of locales
  concern the shipped set as a whole, so they are left alone, and nothing is
  verified.
- `--retranslate PREFIXES`: translate again the units whose id starts with one
  of these, stale or not.
- `--parallel N`: the number of requests in flight across the whole run. Every
  locale shares that one limit. Without it, the manifest's `llm.parallel`.

The model is reached through [`greendrake.nz/go/llm`](../llm/README.md):
Anthropic's API, or any OpenAI-compatible endpoint, a local runtime included.
It is configured from the environment variables under the prefix that the
manifest's `llm.env_prefix` names. That configuration names a model for each
of two tiers, fast and strong.

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

**Units.** Every translatable thing is a *unit*. A unit has a stable id, its
source text, the context a translator needs, and the checks its translation
has to pass. The id is `<pass>:<key>`. A *pass* is one of the manifest's
sources, or the vocabulary: the pass that translates the product's terms. A
unit's translation is kept in the file that the product already reads. A unit
absent from that file is work to do, whatever else is recorded. A record alone
would miss that when a key is deleted by hand or lost in a merge. Some sources
key a plural by CLDR category: an Android `<plurals>`, a String Catalog's
plural variation. Such a plural is one unit. Its text, and its translation,
are its forms written as one JSON object in CLDR's order, so that any form
comes back exactly, line breaks and all.

**Files.** Most formats give each locale files of its own. A String Catalog is
one file that every locale shares, and one lock guards it. A locale's write
reads the file again, replaces that locale's part alone and writes the whole
back. So locales translated at the same time lose nothing of each other's, a
check reports only the locale that differs, and dropping a locale removes its
part rather than the file. Every generated file opens with a header, which
says what generated the file and what an edit to it does. The header is in the
file's own comment syntax: `#` in YAML, one `<!-- -->` after an XML
declaration. A format with no comments, JSON, has none.

**Digests.** Each locale's state file records two things. For each unit, it
records a digest of the unit's source text and the locale's glossary. For each
pass, it records a digest of the *policy* that the pass's units were
translated under. The policy is the command's prompt version, the master's
locale, the brief, the manifest's prompt rules, and the pass's own
declarations that reach a prompt: its guidance, syntax, markup, shape, limits,
context templates, copied blocks. A unit is stale when either digest has
changed, when it has no record, or when its translation is missing. The state
file digests the source side only, never the translation. So a correction made
by hand in a generated file survives until what it was translated from
changes. A unit whose digests still match is never sent. So a run with nothing
stale makes no request and writes no file, and a one-string edit costs one
request. What does not reach a prompt is not policy, and changing it costs
nothing: paths, batch sizes, tiers, the model. A digest covers what a unit
says, not where the master puts it. Keys or paragraphs reordered in the master
change no digest, and still change every file. So a check also compares each
locale's files with what a run would write from the translations they have.

**The policy and the glossary.** The host writes its policy in three files.
The first is a brief, which every prompt opens with. The second is the terms:
the words the product uses about itself, each with what it means in the
product. The third is a register: for each target locale, one English sentence
saying how the locale addresses its reader, du or Sie. The host decides that
once, where a model would otherwise be asked on every run, and every shipped
locale must have its sentence. The brief and the register are files of their
own so that several manifests can name the same ones. Manifests whose strings
share a screen, a host's and those of the libraries it uses, must address the
reader alike. And libraries made for the same apps are described alike. The
terms are a manifest's own. The vocabulary pass translates the terms first and
alone in each locale, into a glossary file that every later prompt includes.
That is what stops one noun becoming three across fifty batches. It is also
why every other unit's digest includes the glossary: correcting one term's
translation, or changing the register, makes the whole locale stale.

**Batches.** Units are asked for in batches, pass by pass. A batch is grouped
as its pass declares: where that is asked for, it never spans a catalogue
section or a file. It is bounded in size. And it never splits a *bundle*: the
fields of one row, which are one message. Each request's JSON schema restricts
the answer's ids to the ones asked for, so a model that would garble a long id
has to write a valid one. Every batch is written as it finishes, with what it
translated, files and state file alike. So a run that stops partway leaves a
partial locale, which a check reports as stale and the next run continues
from. A pass whose policy changed is translated whole. Its policy is recorded
as it starts, so a run that stops partway through it resumes with only what it
had not translated left to do.

**What the model is sent.** A model translates language and keeps
placeholders. What it does not do reliably is restructure, or copy long opaque
tokens. So whatever in a unit is syntax rather than language is sent in a form
a model handles well, and the command puts the original syntax back:

- A plural is never asked for whole. A vue-i18n plural (`|`-separated forms)
  and a plural keyed by category are asked for as one form per target
  category. Each form is shown the source's form for its category, or the
  source's `other`, with the numbers that select it. An ICU plural is asked
  for in two parts: its sentence, with the plural as an opaque `{count}`
  placeholder, and its cases, one per category. `#` is sent as that
  placeholder too. Where a single number selects a category, the model is told
  it may write the number as a word, or leave it out. The command assembles
  the `|` list, the cases under the source's argument, or the keyed forms, in
  the target locale's CLDR categories.
- A printf conversion — `%[n$][flags][width][.precision][length]conversion` —
  is sent as a placeholder of the command's own, `{p1}`, `{p2}`, …, numbered
  by the argument it formats: its stated position, or its place among the
  text's conversions. The model never sees the conversion. Where the
  translation keeps the arguments in the source's order, each conversion comes
  back as the source wrote it. Where the translation reordered, repeated or
  left one out, every conversion comes back positional: `%2$@ … %1$@`, which
  Android and iOS both read. A `%%` is sent as the `%` it stands for, and an
  iOS `%#@name@` substitution is sent as `{name}`.
- An `<xliff:g>` span holds an argument with its example, or a literal that
  the span exists to protect, like `km` or `Gmail`. It is sent whole as one
  placeholder named for its id, `{count}`, and nothing in it reaches the
  model. Its conversions count among the arguments at the span's place. They
  are made positional with the rest, and the span is otherwise kept as
  written.
- A vue-i18n reference, `@:brand.name`, is sent as `{brand_name}`. Left as
  written, a model reads the reference as a placeholder by another name, and
  writes a placeholder of its own in its place.
- A tag's attributes are sent as one bare attribute naming the element:
  `<a href="…" target="_blank">` is sent as `<a attr1>`. A line of CSS or a
  URL has nothing in it to translate, and a model that has to copy one
  duplicates a clause of it now and then. The tag stays a plain HTML tag,
  which a model reproduces as markup. A placeholder in the attribute's place,
  `<a {attr1}>`, reads as running text: a model continues the sentence after
  it and drops the `>`.

These tokens are the command's own encoding: the reference placeholders, each
tag as it is sent, each span and substitution. The command checks that each
comes back exactly as often as the source has it, whatever the host's syntax
says a placeholder is. An attribute marker written anywhere but in its tag is
refused. A source may contain both something the command replaces and the very
placeholder it would be replaced with. Such a source fails its batch: one
could not be told from the other on the way back.

The answer comes back the same way it goes out: every character written as
itself. A text spelled in `\u` escapes is refused, however well it decodes. A
model that switches to ASCII-only JSON writes a script like Greek at several
times the tokens, gets words wrong doing so, and runs into the token cap
mid-string. The prompt states the rule, and a retry repeats it.

**Checks.** Nothing reaches a file unchecked, and nothing falls back to the
master's language. A rejected item is asked for once more, at the strong tier.
The retry attaches every complaint about the item, and quotes what the model
answered. It asks for three versions: first what the model wrote with the
complaint fixed, then the same said another way, and, where the item was over
its limit, each shorter than the one before. The first version that passes is
kept. Three are asked for because a model cannot count characters. Shown its
answer and told it is one too long, it writes the same phrase again. Asked for
ever shorter ones, it writes one that fits, and the command does the counting.
When no version passes, the item fails. Where it is one of a row's fields, the
rest of its row fails with it.

An answer cut off at its token cap is, as a rule, a model repeating itself
inside one string without end. It is not asked for again whole. Every item it
finished is kept, and checked like any other. The item it was cut off writing
is asked for again at the strong tier, as a string that never ended, with its
beginning quoted. The items it never reached are asked for again as a batch of
their own. So one such string costs that string's retry, not its batch. An
answer cut off before any item in it is whole fails its batch.

What a unit is checked for follows from its declarations:

| Declared | Checked |
|---|---|
| anything | the source's HTML tags (attributes included), `@:` references, `\|` and `\` characters as multisets — none added, none dropped. Each is syntax in some message format, and where the unit's format does not read it, the reader sees it. A backslash is also how a model that may not write `\u` escapes spells one out as text. Every letter is in the locale's own script, in Latin, or in a script the source uses. A letter of no script in particular (Japanese `ー`, the ʻokina) passes. So a word of another language that slipped in is refused when its script shows it |
| syntax `icu` | `{name}` placeholders as a set; the plural's cases are the locale's categories; the message is valid for [`greendrake.nz/go/i18n`](../i18n/README.md)'s `messageformat` |
| syntax `vue-i18n` | `{name}` placeholders as a set, form by form; a form that a single number selects may leave the count's placeholder out; one form per category |
| syntax `braces` | `{name}` placeholders as a set |
| syntax `printf` | conversions as a set, keyed by argument. The model never sees a conversion, so its letter is always the source's. In a plural's form that a single number selects, the count's conversion may be left out. No conversion is left out while a later one is kept: a C-style formatter cannot skip an argument whose type it is never told. `%` characters as a multiset. Substitutions and protected spans exactly as often as the source has them. A source mixing stated positions with none, or a format string with a `%` that starts no conversion, is refused |
| syntax `none` | nothing more (literal text) |
| markup `html` | the tags properly nested |
| markup `markdown` | the same, plus link targets as a multiset, heading levels and list kinds in order, and a numbered list numbered in ASCII digits — the only ones markdown reads as a list |
| shape `line` | no line break |
| a field's `limit` | at most that many characters, counted on the text as it is written to the file, with the original syntax put back: that is what the field receives |

Order is not checked: a sentence's word order is the translation's own, and
its placeholders and elements move with the words. Nor is how often a
placeholder appears: a placeholder is substituted by name, and a language with
no pronoun for "them" names `{name}` twice. The system prompt's list of
checked rules is built from the checks that apply to the batch's units. The
rules that nothing checks are stated apart from it: mood, what an id says, the
host's own rules. So a model told that its answer failed "a checked rule" can
tell which.

**Failures.** An item rejected twice fails alone, with the rest of its row. A
request that timed out, or that the endpoint refused, fails with its own
items. Those stay untranslated, and everything else is translated and written:
the rest of their batch, their locale and the others. When a locale's
vocabulary fails, the rest of that locale is not translated in that run, since
every other string is translated against the glossary that the vocabulary
produces. The run then fails, listing each failure, and the next run has
exactly those left to translate. Some failures no other request would escape:
an endpoint that does not answer at all, a file that cannot be written. Those
stop the run at once.

**Requests.** Every locale is translated at the same time, and all of them
share one limit of `--parallel` requests in flight. A locale's vocabulary runs
alone, and so do its last batches. With the limit shared, neither leaves
capacity idle while other locales have work. A request's token cap is the size
of its answer: the text and ids in proportion to the source, plus the JSON
framing, which is most of a batch of one short label. A model that reasons
before it answers spends tokens on the reasoning too. The allowance for those
is `<prefix>REASONING_TOKENS` in the model's configuration, which
`greendrake.nz/go/llm` adds to the cap. Kept to the answer, the cap is what
ends a model repeating itself inside its answer, as truncated JSON and a
retry, before the request's timeout does.

**Completeness and verification.** A full run succeeds only when every unit of
every locale has a translation. A run that wrote anything then runs the host's
`verify` commands. They are the host's own code that loads the translated
files, run over what was just written: the code that would otherwise find a
defect at boot or at build.

## `--adopt`

A policy change makes every unit of the passes it touches stale: a reworded
brief, a clarified guidance line, a new prompt version in an upgrade of this
command. Whether that should cost a translation is the host's judgement, since
only the host knows whether the text on disk still says what it should.
`--adopt` is the answer when it does. It records the current policy for every
locale that has a state file, writes the derived outputs, calls no model and
writes no translation. A unit whose own source changed stays stale: `--adopt`
accepts a change of policy, never a change of text. Adopt before translating:
a pass already partly translated under the new policy has dropped the records
of what it had not reached.

## `carry`

```
translate carry -project host/translate.yaml -from host/translate.yaml -source ui -to lib/translate.yaml -target auth -prefix auth.
translate carry -project host/translate.yaml -from host/translate.yaml -source ui -to host/translate.yaml -target ui -key menu.settings=auth.menu.settings
```

Rearranging where strings are kept must not cost a translation. `carry` copies
each locale's translations from the keys of one catalogue source to the keys
of another. It serves three cases. A library takes over what its host used to
say itself: `-prefix` copies to every target key under the prefix, from the
same key. A key is renamed, or a new key says exactly what an existing one
already says: `-key to=from` copies each. The masters are written by hand, and
are changed by hand first. `carry` copies what only the command writes. Both
sources must be catalogues. A carry is for a project, named by `-project`. It
covers the target's locales, and a library manifest among `-from` and `-to` is
read on the project's behalf. `-prefix ''` names every key. That copies a pass
whole, from one manifest's state to another's over the same files.

A translation is copied only where it is still valid. It is valid when the
source records it as translated from the very text the target's master has,
under the source's current policy and glossary. It arrives recorded as current
in the target, so the target's check does not count it. The exception is a
target whose whole pass is due for translation anyway, because its policy
changed or its vocabulary is not yet translated. There it is written without
being recorded as current, and stays in the file until that translation. A
translation that is not valid is not copied, and the target still has it to
translate: the text changed on the way, say, or the source's record is stale.

The source is left alone. A key it still defines keeps its own translations. A
key it no longer defines leaves its files and records with `--settle`, or with
its next run. Run `--settle` after every carry is done, not between carries:
after an early `--settle`, a source has nothing left to copy. A carry within
one source (a rename) writes that source's files as a run would, which drops
what its master no longer defines. So carry anything else out of that source
first.

## Library manifests

A *library* is a component library with strings of its own, used by several
products. It has a manifest of its own, and its strings are translated into
the locales of the projects that use it, and no others. A *project* is a
product whose manifest declares the locales it ships. The library's manifest
declares its master locale: `default`, the locale its own catalogue is written
in. It declares no locales, and no `command`, `shipped`, `llm` or `derived`.
It is run on behalf of a project's manifest, named by `-project`, in that
project's shipped locales and on its model. A project whose master locale is
not the library's is refused. `--locale` and `--retranslate` act within those
locales, and `--check` reports those alone. The files a library manifest
generates name the library's manifest rather than a project's command. So each
project that uses the library writes the bytes that the other's check compares
against. A run leaves every locale outside its project's set alone: those are
other projects'.

```
translate prune -manifest lib/translate.yaml -project host/translate.yaml -project other/translate.yaml
```

A locale that none of the library's projects ships any more is removed by
`prune`. `prune` is given every project that uses the library. It reads the
library on each one's behalf, so each one's master locale must be the
library's. It then removes the files of each locale that the library has a
state file for and that is outside all of their locale sets.

## The manifest

The manifest is YAML. It is validated on load, before anything else is read,
and each of these is an error: an unknown key, format, check or template
placeholder; a non-canonical tag; a shipped locale with no register; a shipped
locale written in a script the command has no table of letters for. Paths are
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
  glossary: translation/glossary          # <dir>/<tag>.yaml — the translated terms

vocabulary:                               # the pass that translates the terms
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
prefix), `tier` (`fast` | `strong`), `batch` (`by`: `file` or, for a
catalogue, `first-segment`; `size`, zero for unbounded) and `guidance`. The
guidance reaches that pass's prompts and no other's, and `{language}` in it is
the target language's name.

Every source declares its `format`, and the checks its units have to pass:
`syntax` (`icu` | `vue-i18n` | `braces` | `printf` | `none`), `markup`
(`html` | `markdown` | `none`) and, optionally, `shape` (`line` | `text`, the
default). Each format takes its own keys and refuses the rest.

**`yaml-catalogue`** — one nested message catalogue per locale. Each leaf
string is a unit, keyed by its dotted path. Its context is every comment above
it and above its sections. A locale's file copies the master's structure.

```yaml
- id: ui
  format: yaml-catalogue
  path: catalogue/{locale}.yaml           # the master is this at `default`
  syntax: vue-i18n
  syntax_overrides:                       # keys the backend renders, in its syntax
    - {prefix: seo., syntax: icu}
  markup: html
```

**`yaml-tree`** — controlled vocabularies: an attribute's values as a tree, one
file each. A file holds a key, a label per locale, and nodes nested under a
children key, each with a code and names per locale. Each label is a unit, and
so is each node. A node's id is composed from its tree's key and its code, and
its context states its ancestry. A locale's names may sit inline in the
master. The command writes them to a separate file per locale, the sidecar:
`{<key>: {label, nodes: {<code>: name}}}`. The sidecar wins.

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

`skip_node_names` names a boolean key. A tree file sets it when its nodes are
names rather than words: brands, models. Its label is then translated, and its
nodes are not.

**`text-blocks`** — prose, one file per page per locale, split into blocks at
blank lines. A block's id is its own digest under its page's id. So a block
that moves keeps its translation, and an inserted block is the only work. A
locale's file is addressed by order alone, so the order its blocks were
written in is recorded.

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

**`android-strings`** — an Android app's string resources. The master is the
hand-written `res/values/strings.xml`. Each locale's file is in the directory
named for its resource qualifier: `de` for `de`, `en-rGB` for `en-GB`, and BCP
47 form for anything else (`b+zh+Hans`, `b+es+419`). A shipped tag that
Android names no directory for is refused. A `<string>` is a unit keyed by its
name. A `<plurals>` is a plural keyed by category: Android's quantities are
CLDR's categories. Each item of a `<string-array>` is a unit keyed `name[i]`,
and an array's items are one bundle. The comments directly above a resource
are its context. A resource marked `translatable="false"` is left alone, one
whose whole value is a reference (`@string/…`) is copied as it is, and one
marked `formatted="false"` is literal text. A unit's text is what Android
resolves the value to, with its quoting, whitespace and backslash escapes
applied. Its markup is kept, with its own `&` and `<` as entities. A value is
escaped back as it is written. A locale's file keeps the master's order, and
holds only translatable resources, an array only whole. It gives each plural
the target locale's quantities, opens with the XML declaration and the header,
and is read back before it is written.

```yaml
- id: app
  format: android-strings
  master: app/src/main/res/values/strings.xml
  path: app/src/main/res/values-{locale}/strings.xml   # {locale}: the qualifier
  syntax: printf                          # what an Android string is
  markup: html                            # <b>, <i>, <u>, <a>, <xliff:g>
```

**`xcstrings`** — an iOS or macOS app's String Catalog, one file that every
locale shares. Its `sourceLanguage` must be the manifest's `default`, and its
version must be one this reader knows. Each entry is translated from its
source language's localization, or from its key where it has none. Its
`comment` is the context. An entry marked `shouldTranslate: false`, or
`extractionState: stale`, is left alone. A plural variation is a plural keyed
by category. Each device variation is a unit of its own, keyed
`<key>#<device>`. Each substitution's plural is a plural keyed by category,
keyed `<key>%<name>`, and the string that contains `%#@name@` is checked as
printf. An entry's units are one bundle. A key is often the English text and
may hold any character. A key that is not an identifier is addressed by its
digest, `k` and 12 hex digits: the words reach the model as the unit's text,
never as its id. A locale's localization of each entry is replaced, with its
values marked `translated`. The catalog is written as Xcode saves it:
two-space indents, `"key" : value`, every object's keys in code-point order,
no newline at the end. So a save from Xcode changes no byte.

```yaml
- id: ios
  format: xcstrings
  path: App/Localizable.xcstrings         # the one catalog
  syntax: printf
  markup: none
```

A locale's state file is keyed by unit ids. When a declaration that composes
them changes, `node_id` or `page_id`, every unit it covers is at once removed
and new, and everything it covers is translated again. In its tests, a host
checks these declarations against what its own code derives.
