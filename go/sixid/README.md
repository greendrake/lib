# greendrake.nz/go/sixid

Short random ids for the things a program stores: six random bytes, written
as ten characters such as `7ZK4M0Q2X5`. No dependencies.

```sh
go get greendrake.nz/go/sixid
```

## Why

Ids leave the program. They sit in URLs and API payloads, and people read
them out over the phone, paste them into support tickets and retype them from
screenshots. The common kinds of id are poor at that. A UUID is 36
characters, and nobody reads one aloud. A database's counter is short, but
it tells every reader how many rows there are, and lets them reach other
rows by counting up or down.

A sixid is made for ids that people handle. It is ten characters long and
random, and parsing accepts the common ways people mistype an id.

## Trade-offs

- **48 bits is small, so two ids can come out equal.** Among *n* ids of one
  kind, the chance that some two are equal is about *n*²/2⁴⁹: roughly 1 in
  560 for a million ids, and 1 in 6 for ten million. So put a unique index
  on the id, and when an insert is refused as a duplicate, draw another id
  and insert again. With that in place the size of the table stops
  mattering: at a billion rows, one insert in about 280,000 needs the second
  draw. Where an insert cannot be retried — ids drawn offline and merged
  later, say — use a longer id, such as a UUID.
- **No order.** Unlike a ULID or a UUIDv7, a sixid carries no time. Sorting
  by id sorts at random, so keep a timestamp for order.
- **No check digit.** A mistyped id that is still ten valid characters is
  another id. It will most likely name nothing, but the id cannot tell.

## Overview

- **Short.** Ten characters as text, six bytes in the database.
- **Hard to get wrong.** It is written in Crockford's Base32, an alphabet
  without `I`, `L`, `O` and `U`. Parsing accepts what people do to an id:
  lowercase, hyphens and spaces pass, `O` reads as `0`, and `I` and `L` read
  as `1`.
- **One canonical form.** However an id was typed, it is written back one
  way, in uppercase. That form is safe as a cache key or a log search.
- **Random.** An id says nothing about how many others exist or when it was
  made. It can be drawn anywhere, before the row is written, without asking
  the database.

The `prefix` package adds what an id alone lacks: a letter saying what kind
of thing it names.

## `sixid`

- `SixID` is a `[6]byte`. `NewSixID` draws one from `crypto/rand`.
- `String` writes the canonical form: ten uppercase characters.
- `ParseSixID` reads an id leniently, as described above. It refuses the
  empty string, a wrong length and a character outside the alphabet. It also
  refuses a non-canonical spelling: ten characters hold 50 bits, an id has
  48, and the two spare bits must be zero. Without that check, four
  different last characters would read as the same id.
- In JSON an id is its string. `MarshalBinary`, `UnmarshalBinary` and
  `Bytes` work with the six bytes, which is the form to store.
- `IsZero` reports the all-zero id, the type's zero value.

`NewSixID` is random, so a test cannot predict the ids a store assigns. When
a test needs to decide them, do not have the store call `NewSixID` itself.
Pass the store its id generator, a `func() sixid.SixID`, as a dependency:
`sixid.NewSixID` in production, a function returning known ids in the test.

## `prefix`

An id alone does not say what it identifies. A letter in front says it:
`U7ZK4M0Q2X5` for a user, `O…` for an order. A person reading a log can then
tell what an id names, and so can the program. An order's id passed where a
user's belongs is refused as it is parsed. Without the letter it would be
looked up in the wrong table and answered "not found".

The program that exposes the ids is the host, whatever libraries it is built
from. The host decides which kinds of thing exist and which letter each
gets, or that ids carry no letter at all. This package is the mechanism. A
`Codec` encodes and decodes the ids of an `Entity`, a name such as
`"order"`:

- `Plain` writes the id's ten characters alone, whatever the entity. It is
  for a host that wants no prefixes.
- `New(Table)` writes each entity's one-letter prefix before the id's ten
  characters: 11 characters in all. `Decode` refuses an id of another
  entity. `DecodeAny` answers which entity an id belongs to, for a place
  that accepts any kind — a search box, say. `Letter` and `Entities` read
  the table back.

```go
codec := prefix.New(prefix.Table{"order": 'O', "user": 'U'})
s := codec.Encode("order", id) // "O" + id.String()
id, err := codec.Decode(s, "order")
```

Two entities sharing a letter make `New` panic. So does encoding an entity
that is not in the table. Both are mistakes in the program. A letter is part
of an id's public form, so changing an entity's letter invalidates every id
of it already handed out.

A library that exposes ids takes the host's `Codec` rather than choosing a
form itself. It documents the entity names it uses, which the host's `Table`
must include.
